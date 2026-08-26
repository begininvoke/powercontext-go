package endpoint

import (
	"context"

	v1 "github.com/ob-labs/powercontext-go/api/v1"
	"github.com/ob-labs/powercontext-go/artifact"
	"github.com/ob-labs/powercontext-go/review"
	"github.com/ob-labs/powercontext-go/runtime"
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
