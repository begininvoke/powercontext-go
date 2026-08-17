package endpoint

import (
	"context"
	"fmt"

	v1 "github.com/thunguo/powercontext-go/api/v1"
	"github.com/thunguo/powercontext-go/artifact"
	"github.com/thunguo/powercontext-go/artifact/experience"
	"github.com/thunguo/powercontext-go/artifact/skill"
	"github.com/thunguo/powercontext-go/review"
	"github.com/thunguo/powercontext-go/runtime"
	"github.com/thunguo/powercontext-go/source"
)

type ReviewOperations interface {
	ProposeExperience(context.Context, string, experience.Content, []source.Ref, []artifact.Ref, *artifact.Ref, *string) (review.Snapshot, error)
	ProposeSkill(context.Context, string, skill.Content, []source.Ref, []artifact.Ref, *artifact.Ref, *string) (review.Snapshot, error)
	GetCandidate(context.Context, string, string) (review.Snapshot, error)
	ListCandidates(context.Context, string, review.Status, *string, *string, int) (review.Page, error)
	Approve(context.Context, string, string, int64) (review.Snapshot, error)
	Reject(context.Context, string, string, int64, string) (review.Snapshot, error)
	Revise(context.Context, string, string, int64, any, []source.Ref, []artifact.Ref, *artifact.Ref, *string) (review.Snapshot, error)
	GetExperience(context.Context, string, artifact.Ref) (experience.Experience, error)
	GetSkill(context.Context, string, artifact.Ref) (skill.Skill, error)
}

func (h *Handler) ProposeExperience(
	ctx context.Context,
	req *v1.ProposeExperienceRequest,
) (v1.ProposeExperienceRes, error) {
	if h.review == nil {
		return nil, &RuntimeNotReadyError{}
	}
	proposal, err := experienceProposal(req.Proposal)
	if err != nil {
		return nil, err
	}
	sources, artifacts, target, err := reviewLineage(req.SourceRefs, req.ArtifactRefs, req.Target)
	if err != nil {
		return nil, err
	}
	value, err := h.review.ProposeExperience(
		ctx, req.ScopeID, proposal, sources, artifacts, target, optionalString(req.Reason),
	)
	if err != nil {
		return nil, err
	}
	return candidateHeaders(ctx, value)
}

func (h *Handler) ProposeSkill(
	ctx context.Context,
	req *v1.ProposeSkillRequest,
) (v1.ProposeSkillRes, error) {
	if h.review == nil {
		return nil, &RuntimeNotReadyError{}
	}
	proposal, err := skillProposal(req.Proposal)
	if err != nil {
		return nil, err
	}
	sources, artifacts, target, err := reviewLineage(req.SourceRefs, req.ArtifactRefs, req.Target)
	if err != nil {
		return nil, err
	}
	value, err := h.review.ProposeSkill(
		ctx, req.ScopeID, proposal, sources, artifacts, target, optionalString(req.Reason),
	)
	if err != nil {
		return nil, err
	}
	return candidateHeaders(ctx, value)
}

func (h *Handler) GetArtifactCandidate(
	ctx context.Context,
	req *v1.GetArtifactCandidateRequest,
) (v1.GetArtifactCandidateRes, error) {
	if h.review == nil {
		return nil, &RuntimeNotReadyError{}
	}
	value, err := h.review.GetCandidate(ctx, req.ScopeID, req.CandidateID)
	if err != nil {
		return nil, err
	}
	return candidateHeaders(ctx, value)
}

func (h *Handler) ListArtifactCandidates(
	ctx context.Context,
	req *v1.ListArtifactCandidatesRequest,
) (v1.ListArtifactCandidatesRes, error) {
	if h.review == nil {
		return nil, &RuntimeNotReadyError{}
	}
	var family *string
	if value, ok := req.Family.Get(); ok {
		converted := string(value)
		family = &converted
	}
	page, err := h.review.ListCandidates(
		ctx, req.ScopeID, review.Status(req.Status.Or(v1.CandidateStatusPending)),
		family, optionalString(req.Cursor), req.Limit.Or(review.DefaultPageSize),
	)
	if err != nil {
		return nil, err
	}
	candidates := make([]v1.ArtifactCandidate, len(page.Candidates))
	for index, value := range page.Candidates {
		candidates[index], err = candidate(value)
		if err != nil {
			return nil, err
		}
	}
	return &v1.ArtifactCandidatePageHeaders{
		XPowerContextRequestID: requestID(ctx),
		Response: v1.ArtifactCandidatePage{
			Candidates: candidates,
			NextCursor: nullableString(page.NextCursor),
		},
	}, nil
}

func (h *Handler) ApproveArtifactCandidate(
	ctx context.Context,
	req *v1.ApproveArtifactCandidateRequest,
) (v1.ApproveArtifactCandidateRes, error) {
	if h.review == nil {
		return nil, &RuntimeNotReadyError{}
	}
	value, err := h.review.Approve(ctx, req.ScopeID, req.CandidateID, int64(req.ExpectedVersion))
	if err != nil {
		return nil, err
	}
	return candidateHeaders(ctx, value)
}

func (h *Handler) RejectArtifactCandidate(
	ctx context.Context,
	req *v1.RejectArtifactCandidateRequest,
) (v1.RejectArtifactCandidateRes, error) {
	if h.review == nil {
		return nil, &RuntimeNotReadyError{}
	}
	value, err := h.review.Reject(
		ctx, req.ScopeID, req.CandidateID, int64(req.ExpectedVersion), req.Reason,
	)
	if err != nil {
		return nil, err
	}
	return candidateHeaders(ctx, value)
}

func (h *Handler) ReviseArtifactCandidate(
	ctx context.Context,
	req *v1.ReviseArtifactCandidateRequest,
) (v1.ReviseArtifactCandidateRes, error) {
	if h.review == nil {
		return nil, &RuntimeNotReadyError{}
	}
	proposal, err := reviewedProposal(req.Proposal)
	if err != nil {
		return nil, err
	}
	sources, artifacts, target, err := reviewLineage(req.SourceRefs, req.ArtifactRefs, req.Target)
	if err != nil {
		return nil, err
	}
	value, err := h.review.Revise(
		ctx, req.ScopeID, req.CandidateID, int64(req.ExpectedVersion), proposal,
		sources, artifacts, target, optionalString(req.Reason),
	)
	if err != nil {
		return nil, err
	}
	return candidateHeaders(ctx, value)
}

func (h *Handler) GetExperience(
	ctx context.Context,
	req *v1.GetExperienceRequest,
) (v1.GetExperienceRes, error) {
	if h.review == nil {
		return nil, &RuntimeNotReadyError{}
	}
	ref, err := runtimeArtifactReference(req.Artifact)
	if err != nil {
		return nil, err
	}
	value, err := h.review.GetExperience(ctx, req.ScopeID, ref)
	if err != nil {
		return nil, err
	}
	lineage := value.Lineage()
	return &v1.ExperienceArtifactHeaders{
		XPowerContextRequestID: requestID(ctx),
		Response: v1.ExperienceArtifact{
			Artifact: artifactReference(value.Ref()), Content: wireExperienceProposal(value.Content()),
			SourceRefs: wireSourceReferences(lineage.Sources()), ArtifactRefs: wireArtifactReferences(lineage.Artifacts()),
		},
	}, nil
}

func (h *Handler) GetSkill(
	ctx context.Context,
	req *v1.GetSkillRequest,
) (v1.GetSkillRes, error) {
	if h.review == nil {
		return nil, &RuntimeNotReadyError{}
	}
	ref, err := runtimeArtifactReference(req.Artifact)
	if err != nil {
		return nil, err
	}
	value, err := h.review.GetSkill(ctx, req.ScopeID, ref)
	if err != nil {
		return nil, err
	}
	lineage := value.Lineage()
	return &v1.SkillArtifactHeaders{
		XPowerContextRequestID: requestID(ctx),
		Response: v1.SkillArtifact{
			Artifact: artifactReference(value.Ref()), Content: wireSkillProposal(value.Content()),
			SourceRefs: wireSourceReferences(lineage.Sources()), ArtifactRefs: wireArtifactReferences(lineage.Artifacts()),
		},
	}, nil
}

func candidateHeaders(ctx context.Context, value review.Snapshot) (*v1.ArtifactCandidateHeaders, error) {
	response, err := candidate(value)
	if err != nil {
		return nil, err
	}
	return &v1.ArtifactCandidateHeaders{XPowerContextRequestID: requestID(ctx), Response: response}, nil
}

func candidate(value review.Snapshot) (v1.ArtifactCandidate, error) {
	if value == nil {
		return v1.ArtifactCandidate{}, fmt.Errorf("endpoint: nil Candidate snapshot")
	}
	var proposal v1.ArtifactCandidateProposal
	switch content := value.ProposalValue().(type) {
	case experience.Content:
		proposal = v1.NewExperienceProposalArtifactCandidateProposal(wireExperienceProposal(content))
	case skill.Content:
		proposal = v1.NewSkillProposalArtifactCandidateProposal(wireSkillProposal(content))
	default:
		return v1.ArtifactCandidate{}, fmt.Errorf("endpoint: unsupported Candidate proposal %T", content)
	}
	result := v1.ArtifactCandidate{
		CandidateID: value.ID(), Version: int(value.Version()), Family: v1.CandidateFamily(value.Family()),
		Status: v1.CandidateStatus(value.Status()), Proposal: proposal,
		SourceRefs: wireSourceReferences(value.Sources()), ArtifactRefs: wireArtifactReferences(value.Artifacts()),
		Reason: nullableString(value.Reason()), DecisionReason: nullableString(value.DecisionReason()),
	}
	if target := value.Target(); target != nil {
		result.Target = v1.NewNilArtifactReference(artifactReference(*target))
	} else {
		result.Target.SetToNull()
	}
	if artifactResult := value.ResultArtifact(); artifactResult != nil {
		result.ResultArtifact = v1.NewNilArtifactReference(artifactReference(*artifactResult))
	} else {
		result.ResultArtifact.SetToNull()
	}
	return result, nil
}

func experienceProposal(value v1.ExperienceProposal) (experience.Content, error) {
	result, err := experience.NewContent(value.Situation, value.Action, value.Outcome, value.Lesson)
	if err != nil {
		return experience.Content{}, &InvalidRequestError{Field: "proposal"}
	}
	return result, nil
}

func skillProposal(value v1.SkillProposal) (skill.Content, error) {
	validation := make([]string, len(value.Validation))
	for index, item := range value.Validation {
		validation[index] = string(item)
	}
	result, err := skill.NewContent(value.Name, value.Description, value.Instructions, validation)
	if err != nil {
		return skill.Content{}, &InvalidRequestError{Field: "proposal"}
	}
	return result, nil
}

func reviewedProposal(value v1.ReviseArtifactCandidateRequestProposal) (any, error) {
	if proposal, ok := value.GetExperienceProposal(); ok {
		return experienceProposal(proposal)
	}
	if proposal, ok := value.GetSkillProposal(); ok {
		return skillProposal(proposal)
	}
	return nil, &InvalidRequestError{Field: "proposal"}
}

func reviewLineage(
	sourceValues []v1.SourceReference,
	artifactValues []v1.ArtifactReference,
	targetValue v1.OptNilArtifactReference,
) ([]source.Ref, []artifact.Ref, *artifact.Ref, error) {
	sources := make([]source.Ref, len(sourceValues))
	for index, value := range sourceValues {
		ref, err := source.NewRef(value.Name, value.SourceID)
		if err != nil {
			return nil, nil, nil, err
		}
		sources[index] = ref
	}
	artifacts := make([]artifact.Ref, len(artifactValues))
	for index, value := range artifactValues {
		ref, err := runtimeArtifactReference(value)
		if err != nil {
			return nil, nil, nil, err
		}
		artifacts[index] = ref
	}
	var target *artifact.Ref
	if value, ok := targetValue.Get(); ok {
		ref, err := runtimeArtifactReference(value)
		if err != nil {
			return nil, nil, nil, err
		}
		target = &ref
	}
	return sources, artifacts, target, nil
}

func wireExperienceProposal(value experience.Content) v1.ExperienceProposal {
	return v1.ExperienceProposal{
		Situation: value.Situation(), Action: value.Action(), Outcome: value.Outcome(), Lesson: value.Lesson(),
	}
}

func wireSkillProposal(value skill.Content) v1.SkillProposal {
	validation := value.Validation()
	items := make([]v1.SkillValidationItem, len(validation))
	for index, item := range validation {
		items[index] = v1.SkillValidationItem(item)
	}
	return v1.SkillProposal{
		Name: value.Name(), Description: value.Description(), Instructions: value.Instructions(), Validation: items,
	}
}

func wireSourceReferences(values []source.Ref) []v1.SourceReference {
	result := make([]v1.SourceReference, len(values))
	for index, value := range values {
		result[index] = sourceReference(value)
	}
	return result
}

func wireArtifactReferences(values []artifact.Ref) []v1.ArtifactReference {
	result := make([]v1.ArtifactReference, len(values))
	for index, value := range values {
		result[index] = artifactReference(value)
	}
	return result
}

var _ ReviewOperations = (*runtime.ReviewApplication)(nil)
