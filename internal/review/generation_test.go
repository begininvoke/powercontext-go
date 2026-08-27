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

package review_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/ob-labs/powercontext-go/artifact"
	"github.com/ob-labs/powercontext-go/artifact/experience"
	"github.com/ob-labs/powercontext-go/artifact/skill"
	"github.com/ob-labs/powercontext-go/internal/review"
	"github.com/ob-labs/powercontext-go/source"
)

func TestGenerationWithoutModelFailsBeforeEvidenceOrCandidatePersistence(t *testing.T) {
	t.Parallel()

	evidence := &recordingEvidenceReader{}
	backend := &recordingReviewBackend{}
	service := newGenerationService(t, evidence, backend, nil, nil)
	sourceRef := mustSourceRef(t, "content", "task-1")

	_, err := service.Experience(t.Context(), []source.Ref{sourceRef}, nil, nil, nil)
	var unavailable *review.GenerationCapabilityUnavailableError
	if !errors.As(err, &unavailable) || unavailable.Family != experience.Family {
		t.Fatalf("generation error = %v", err)
	}
	if evidence.calls != 0 || backend.proposeCalls != 0 {
		t.Fatalf("unavailable generation read evidence or persisted a Candidate: evidence=%d propose=%d",
			evidence.calls, backend.proposeCalls)
	}
}

func TestExperienceGenerationUsesExactEvidenceTargetAndSupportsNoOp(t *testing.T) {
	t.Parallel()

	sourceRef := mustSourceRef(t, "content", "task-1")
	target := mustArtifactRef(t, experience.Family, "experience-1", 1)
	sourceEvidence, err := artifact.NewGenerationEvidence(
		"source:content/task-1", artifact.SourceEvidence, "contract validation passed", false,
	)
	if err != nil {
		t.Fatal(err)
	}
	artifactEvidence, err := artifact.NewGenerationEvidence(
		"artifact:experience/experience-1@1", artifact.ArtifactEvidence, "reviewed experience", false,
	)
	if err != nil {
		t.Fatal(err)
	}
	proposal := experienceProposal(t, "regenerate before contract tests")
	generator := &recordingExperienceGenerator{proposal: &proposal}
	evidence := &recordingEvidenceReader{values: []artifact.GenerationEvidence{sourceEvidence, artifactEvidence}}
	backend := &recordingReviewBackend{}
	service := newGenerationService(t, evidence, backend, generator, nil)

	result, err := service.Experience(
		t.Context(), []source.Ref{sourceRef, sourceRef}, []artifact.Ref{target}, &target, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Generated() || backend.proposeCalls != 1 {
		t.Fatalf("generation result = %#v, propose calls = %d", result, backend.proposeCalls)
	}
	if got := result.Candidate.Sources(); len(got) != 1 || got[0] != sourceRef {
		t.Fatalf("canonical Candidate sources = %#v", got)
	}
	if len(generator.inputs) != 1 || generator.inputs[0].TargetEvidenceID() == nil ||
		*generator.inputs[0].TargetEvidenceID() != "artifact:experience/experience-1@1" {
		t.Fatalf("generator inputs = %#v", generator.inputs)
	}

	evidence.values = []artifact.GenerationEvidence{sourceEvidence}
	_, err = service.Experience(t.Context(), []source.Ref{sourceRef}, []artifact.Ref{target}, &target, nil)
	assertInvalidCandidateField(t, err, "artifacts")
	if len(generator.inputs) != 1 || backend.proposeCalls != 1 {
		t.Fatalf("invalid target reached model or persistence: inputs=%d propose=%d",
			len(generator.inputs), backend.proposeCalls)
	}

	noOp := &recordingExperienceGenerator{}
	noOpBackend := &recordingReviewBackend{}
	noOpService := newGenerationService(
		t, &recordingEvidenceReader{values: []artifact.GenerationEvidence{sourceEvidence}}, noOpBackend, noOp, nil,
	)
	noOpResult, err := noOpService.Experience(t.Context(), []source.Ref{sourceRef}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if noOpResult.Generated() || noOpBackend.proposeCalls != 0 {
		t.Fatalf("no-op generation persisted a Candidate: result=%#v propose=%d", noOpResult, noOpBackend.proposeCalls)
	}
}

func TestManagedSkillGenerationEnforcesOriginSpecificLineage(t *testing.T) {
	t.Parallel()

	sourceRef := mustSourceRef(t, "content", "task-1")
	experienceRef := mustArtifactRef(t, experience.Family, "experience-1", 1)
	skillRef := mustArtifactRef(t, skill.Family, "skill-1", 1)
	tests := []struct {
		name      string
		origin    review.SkillGenerationOrigin
		sources   []source.Ref
		artifacts []artifact.Ref
		target    *artifact.Ref
	}{
		{name: "experience requires artifact", origin: review.SkillOriginExperience, sources: []source.Ref{sourceRef}},
		{name: "experience rejects non-experience", origin: review.SkillOriginExperience, artifacts: []artifact.Ref{skillRef}},
		{name: "source accepts only sources", origin: review.SkillOriginSource, sources: []source.Ref{sourceRef}, artifacts: []artifact.Ref{experienceRef}},
		{name: "usage requires source", origin: review.SkillOriginUsage, artifacts: []artifact.Ref{skillRef}, target: &skillRef},
		{name: "usage requires exact target evidence", origin: review.SkillOriginUsage, sources: []source.Ref{sourceRef}, target: &skillRef},
		{name: "unknown origin", origin: review.SkillGenerationOrigin("unknown"), sources: []source.Ref{sourceRef}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			evidence := &recordingEvidenceReader{}
			backend := &recordingReviewBackend{}
			service := newGenerationService(t, evidence, backend, nil, &recordingSkillGenerator{})
			_, err := service.Skill(t.Context(), test.origin, test.sources, test.artifacts, test.target, nil)
			assertInvalidCandidateField(t, err, "origin")
			if evidence.calls != 0 || backend.proposeCalls != 0 {
				t.Fatalf("invalid origin reached evidence or persistence: evidence=%d propose=%d",
					evidence.calls, backend.proposeCalls)
			}
		})
	}

	item, err := artifact.NewGenerationEvidence(
		"artifact:experience/experience-1@1", artifact.ArtifactEvidence, "reviewed experience", false,
	)
	if err != nil {
		t.Fatal(err)
	}
	proposal := skillProposal(t, "run the checked validations")
	generator := &recordingSkillGenerator{proposal: &proposal}
	backend := &recordingReviewBackend{}
	service := newGenerationService(
		t, &recordingEvidenceReader{values: []artifact.GenerationEvidence{item}}, backend, nil, generator,
	)
	result, err := service.Skill(
		t.Context(), review.SkillOriginExperience, []source.Ref{sourceRef}, []artifact.Ref{experienceRef}, nil, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Generated() || backend.proposeCalls != 1 || len(generator.inputs) != 1 {
		t.Fatalf("valid Experience-origin result=%#v propose=%d inputs=%d",
			result, backend.proposeCalls, len(generator.inputs))
	}
}

func newGenerationService(
	t *testing.T,
	evidence review.EvidenceReader,
	backend review.Backend,
	experienceGenerator experience.Generator,
	skillGenerator skill.Generator,
) *review.GenerationService {
	t.Helper()
	reviewService, err := review.NewService(backend, func(kind string) (string, error) {
		return kind + "-1", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	service, err := review.NewGenerationService(evidence, reviewService, experienceGenerator, skillGenerator)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

type recordingEvidenceReader struct {
	values []artifact.GenerationEvidence
	err    error
	calls  int
}

func (r *recordingEvidenceReader) Read(
	context.Context,
	[]source.Ref,
	[]artifact.Ref,
) ([]artifact.GenerationEvidence, error) {
	r.calls++
	return append([]artifact.GenerationEvidence(nil), r.values...), r.err
}

type recordingExperienceGenerator struct {
	proposal *experience.Content
	err      error
	inputs   []artifact.GenerationInput
}

func (g *recordingExperienceGenerator) Generate(
	_ context.Context,
	input artifact.GenerationInput,
) (*experience.Content, error) {
	g.inputs = append(g.inputs, input)
	return g.proposal, g.err
}

type recordingSkillGenerator struct {
	proposal *skill.Content
	err      error
	inputs   []artifact.GenerationInput
}

func (g *recordingSkillGenerator) Generate(
	_ context.Context,
	input artifact.GenerationInput,
) (*skill.Content, error) {
	g.inputs = append(g.inputs, input)
	return g.proposal, g.err
}

type recordingReviewBackend struct{ proposeCalls int }

func (b *recordingReviewBackend) Propose(
	_ context.Context,
	candidateID, family string,
	proposal any,
	sources []source.Ref,
	artifacts []artifact.Ref,
	target *artifact.Ref,
	reason *string,
) (review.Snapshot, error) {
	b.proposeCalls++
	switch value := proposal.(type) {
	case experience.Content:
		return review.NewCandidate(
			candidateID, 1, family, review.Pending, value,
			sources, artifacts, target, reason, nil, nil,
		)
	case skill.Content:
		return review.NewCandidate(
			candidateID, 1, family, review.Pending, value,
			sources, artifacts, target, reason, nil, nil,
		)
	default:
		return nil, fmt.Errorf("unexpected proposal type %T", proposal)
	}
}

func (*recordingReviewBackend) Get(context.Context, string) (review.Snapshot, error) {
	return nil, errors.New("unexpected Get")
}

func (*recordingReviewBackend) List(
	context.Context,
	review.Status,
	*string,
	*string,
	int,
) (review.Page, error) {
	return review.Page{}, errors.New("unexpected List")
}

func (*recordingReviewBackend) Revise(
	context.Context,
	string,
	int64,
	any,
	[]source.Ref,
	[]artifact.Ref,
	*artifact.Ref,
	*string,
) (review.Snapshot, error) {
	return nil, errors.New("unexpected Revise")
}

func (*recordingReviewBackend) Reject(context.Context, string, int64, string) (review.Snapshot, error) {
	return nil, errors.New("unexpected Reject")
}

func (*recordingReviewBackend) Approve(
	context.Context,
	string,
	int64,
	review.IDFactory,
) (review.Snapshot, error) {
	return nil, errors.New("unexpected Approve")
}

func (*recordingReviewBackend) GetArtifact(context.Context, artifact.Ref) (artifact.Snapshot, error) {
	return nil, errors.New("unexpected GetArtifact")
}

func experienceProposal(t *testing.T, lesson string) experience.Content {
	t.Helper()
	value, err := experience.NewContent("situation", "action", "outcome", lesson)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func skillProposal(t *testing.T, instructions string) skill.Content {
	t.Helper()
	value, err := skill.NewContent(
		"review-contract", "Use for reviewed contract changes.", instructions, []string{"tests pass"},
	)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
