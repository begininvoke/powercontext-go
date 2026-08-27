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

package endpoint

import (
	"context"

	v1 "github.com/ob-labs/powercontext-go/api/v1"
	"github.com/ob-labs/powercontext-go/artifact"
	"github.com/ob-labs/powercontext-go/internal/review"
	"github.com/ob-labs/powercontext-go/internal/runtime"
	"github.com/ob-labs/powercontext-go/source"
)

type GenerationOperations interface {
	GenerateExperience(context.Context, string, []source.Ref, []artifact.Ref, *artifact.Ref, *string) (review.GeneratedCandidateResult, error)
	GenerateSkill(context.Context, string, review.SkillGenerationOrigin, []source.Ref, []artifact.Ref, *artifact.Ref, *string) (review.GeneratedCandidateResult, error)
}

func (h *Handler) GenerateExperience(
	ctx context.Context,
	req *v1.GenerateExperienceRequest,
) (v1.GenerateExperienceRes, error) {
	if h.generation == nil {
		return nil, &GenerationUnavailableError{Family: "experience"}
	}
	sources, artifacts, target, err := reviewLineage(req.SourceRefs, req.ArtifactRefs, req.Target)
	if err != nil {
		return nil, err
	}
	result, err := h.generation.GenerateExperience(
		ctx, req.ScopeID, sources, artifacts, target, optionalString(req.Reason),
	)
	if err != nil {
		return nil, err
	}
	return generatedCandidateHeaders(ctx, result)
}

func (h *Handler) GenerateSkill(
	ctx context.Context,
	req *v1.GenerateSkillRequest,
) (v1.GenerateSkillRes, error) {
	if h.generation == nil {
		return nil, &GenerationUnavailableError{Family: "skill"}
	}
	sources, artifacts, target, err := reviewLineage(req.SourceRefs, req.ArtifactRefs, req.Target)
	if err != nil {
		return nil, err
	}
	result, err := h.generation.GenerateSkill(
		ctx, req.ScopeID, review.SkillGenerationOrigin(req.Origin),
		sources, artifacts, target, optionalString(req.Reason),
	)
	if err != nil {
		return nil, err
	}
	return generatedCandidateHeaders(ctx, result)
}

func generatedCandidateHeaders(
	ctx context.Context,
	result review.GeneratedCandidateResult,
) (*v1.GeneratedCandidateResponseHeaders, error) {
	response := v1.GeneratedCandidateResponse{Status: v1.GeneratedCandidateStatusNoOp}
	if result.Generated() {
		value, err := candidate(result.Candidate)
		if err != nil {
			return nil, err
		}
		response.Status = v1.GeneratedCandidateStatusPending
		response.Candidate = v1.NewNilArtifactCandidate(value)
	} else {
		response.Candidate.SetToNull()
	}
	return &v1.GeneratedCandidateResponseHeaders{
		XPowerContextRequestID: requestID(ctx), Response: response,
	}, nil
}

var _ GenerationOperations = (*runtime.GenerationApplication)(nil)
