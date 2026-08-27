// Copyright (c) 2026 OceanBase.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package review

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/ob-labs/powercontext-go/artifact"
	"github.com/ob-labs/powercontext-go/artifact/experience"
	"github.com/ob-labs/powercontext-go/artifact/skill"
	"github.com/ob-labs/powercontext-go/source"
)

type SkillGenerationOrigin string

const (
	SkillOriginExperience SkillGenerationOrigin = "experience"
	SkillOriginSource     SkillGenerationOrigin = "source"
	SkillOriginUsage      SkillGenerationOrigin = "usage"
)

type EvidenceReader interface {
	Read(context.Context, []source.Ref, []artifact.Ref) ([]artifact.GenerationEvidence, error)
}

type GeneratedCandidateResult struct{ Candidate Snapshot }

func (r GeneratedCandidateResult) Generated() bool { return r.Candidate != nil }

// GenerationService resolves immutable evidence in one short transaction,
// invokes the model with no transaction open, and only then creates a pending
// Candidate in a separate transaction.
type GenerationService struct {
	evidence            EvidenceReader
	review              *Service
	experienceGenerator experience.Generator
	skillGenerator      skill.Generator
}

func NewGenerationService(
	evidence EvidenceReader,
	reviewService *Service,
	experienceGenerator experience.Generator,
	skillGenerator skill.Generator,
) (*GenerationService, error) {
	if evidence == nil || reviewService == nil {
		return nil, errors.New("review: generation evidence and Review service must not be nil")
	}
	return &GenerationService{
		evidence: evidence, review: reviewService,
		experienceGenerator: experienceGenerator, skillGenerator: skillGenerator,
	}, nil
}

func (s *GenerationService) CanGenerateExperience() bool { return s.experienceGenerator != nil }
func (s *GenerationService) CanGenerateSkill() bool      { return s.skillGenerator != nil }

func (s *GenerationService) Experience(
	ctx context.Context,
	sources []source.Ref,
	artifacts []artifact.Ref,
	target *artifact.Ref,
	reason *string,
) (GeneratedCandidateResult, error) {
	if s.experienceGenerator == nil {
		return GeneratedCandidateResult{}, &GenerationCapabilityUnavailableError{Family: experience.Family}
	}
	if target != nil && target.Family() != experience.Family {
		return GeneratedCandidateResult{}, &InvalidCandidateError{Field: "target", Detail: "must identify an Experience"}
	}
	evidence, err := s.evidence.Read(ctx, slices.Clone(sources), slices.Clone(artifacts))
	if err != nil {
		return GeneratedCandidateResult{}, err
	}
	input, err := generationInput(evidence, target)
	if err != nil {
		return GeneratedCandidateResult{}, err
	}
	proposal, err := s.experienceGenerator.Generate(ctx, input)
	if err != nil || proposal == nil {
		return GeneratedCandidateResult{}, err
	}
	candidate, err := s.review.ProposeExperience(ctx, *proposal, sources, artifacts, target, reason)
	if err != nil {
		return GeneratedCandidateResult{}, err
	}
	return GeneratedCandidateResult{Candidate: candidate}, nil
}

func (s *GenerationService) Skill(
	ctx context.Context,
	origin SkillGenerationOrigin,
	sources []source.Ref,
	artifacts []artifact.Ref,
	target *artifact.Ref,
	reason *string,
) (GeneratedCandidateResult, error) {
	if s.skillGenerator == nil {
		return GeneratedCandidateResult{}, &GenerationCapabilityUnavailableError{Family: skill.Family}
	}
	if err := validateSkillLineage(origin, sources, artifacts, target); err != nil {
		return GeneratedCandidateResult{}, err
	}
	evidence, err := s.evidence.Read(ctx, slices.Clone(sources), slices.Clone(artifacts))
	if err != nil {
		return GeneratedCandidateResult{}, err
	}
	input, err := generationInput(evidence, target)
	if err != nil {
		return GeneratedCandidateResult{}, err
	}
	proposal, err := s.skillGenerator.Generate(ctx, input)
	if err != nil || proposal == nil {
		return GeneratedCandidateResult{}, err
	}
	candidate, err := s.review.ProposeSkill(ctx, *proposal, sources, artifacts, target, reason)
	if err != nil {
		return GeneratedCandidateResult{}, err
	}
	return GeneratedCandidateResult{Candidate: candidate}, nil
}

func generationInput(evidence []artifact.GenerationEvidence, target *artifact.Ref) (artifact.GenerationInput, error) {
	var targetID *string
	if target != nil {
		value := artifactEvidenceID(*target)
		found := false
		for _, item := range evidence {
			if item.EvidenceID == value {
				found = true
				break
			}
		}
		if !found {
			return artifact.GenerationInput{}, &InvalidCandidateError{
				Field: "artifacts", Detail: "must include the exact target Artifact",
			}
		}
		targetID = &value
	}
	value, err := artifact.NewGenerationInput(evidence, targetID)
	if err != nil {
		return artifact.GenerationInput{}, &InvalidCandidateError{Field: "evidence", Detail: err.Error()}
	}
	return value, nil
}

func validateSkillLineage(
	origin SkillGenerationOrigin,
	sources []source.Ref,
	artifacts []artifact.Ref,
	target *artifact.Ref,
) error {
	switch origin {
	case SkillOriginExperience:
		if target != nil || len(artifacts) == 0 {
			return invalidSkillOrigin("Experience generation requires approved Experience references and no target")
		}
		for _, ref := range artifacts {
			if ref.Family() != experience.Family {
				return invalidSkillOrigin("Experience generation requires approved Experience references and no target")
			}
		}
		return nil
	case SkillOriginSource:
		if target != nil || len(sources) == 0 || len(artifacts) != 0 {
			return invalidSkillOrigin("Source generation requires only exact Source references")
		}
		return nil
	case SkillOriginUsage:
		if target == nil || target.Family() != skill.Family || len(sources) == 0 || !containsArtifact(artifacts, *target) {
			return invalidSkillOrigin("usage evolution requires the exact target Skill and bounded Source evidence")
		}
		return nil
	default:
		return invalidSkillOrigin(fmt.Sprintf("unsupported Skill generation origin %q", origin))
	}
}

func invalidSkillOrigin(detail string) error {
	return &InvalidCandidateError{Field: "origin", Detail: detail}
}

func containsArtifact(values []artifact.Ref, target artifact.Ref) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func artifactEvidenceID(ref artifact.Ref) string {
	return fmt.Sprintf("artifact:%s/%s@%d", ref.Family(), ref.ID(), ref.Revision())
}
