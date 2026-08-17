package review

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/thunguo/powercontext-go/artifact"
	"github.com/thunguo/powercontext-go/source"
)

const (
	MaxEvidence     = 32
	MaxPageSize     = 100
	DefaultPageSize = 50
	MaxReasonLength = 2_000
	MaxCandidateID  = 128
)

type Status string

const (
	Pending  Status = "pending"
	Approved Status = "approved"
	Rejected Status = "rejected"
)

// Candidate is one current head with its immutable proposal version.
type Candidate[T any] struct {
	candidateID    string
	version        int64
	family         string
	status         Status
	proposal       T
	sources        []source.Ref
	artifacts      []artifact.Ref
	target         *artifact.Ref
	reason         *string
	resultArtifact *artifact.Ref
	decisionReason *string
}

func NewCandidate[T any](
	candidateID string,
	version int64,
	family string,
	status Status,
	proposal T,
	sources []source.Ref,
	artifacts []artifact.Ref,
	target *artifact.Ref,
	reason *string,
	result *artifact.Ref,
	decisionReason *string,
) (Candidate[T], error) {
	if err := candidateText("candidate_id", candidateID, MaxCandidateID); err != nil {
		return Candidate[T]{}, err
	}
	if err := candidateText("family", family, 0); err != nil {
		return Candidate[T]{}, err
	}
	if version < 1 {
		return Candidate[T]{}, &InvalidCandidateError{Field: "version", Detail: "must be positive"}
	}
	if status != Pending && status != Approved && status != Rejected {
		return Candidate[T]{}, &InvalidCandidateError{Field: "status", Detail: string(status)}
	}
	if len(sources)+len(artifacts) < 1 {
		return Candidate[T]{}, &InvalidCandidateError{Field: "evidence", Detail: "at least one exact reference is required"}
	}
	if len(sources)+len(artifacts) > MaxEvidence || len(sources) > MaxEvidence || len(artifacts) > MaxEvidence {
		return Candidate[T]{}, &InvalidCandidateError{Field: "evidence", Detail: fmt.Sprintf("must not exceed %d exact references", MaxEvidence)}
	}
	for _, ref := range sources {
		if _, err := source.NewRef(ref.Type(), ref.ID()); err != nil {
			return Candidate[T]{}, err
		}
	}
	for _, ref := range artifacts {
		if err := ref.Validate(); err != nil {
			return Candidate[T]{}, err
		}
	}
	if target != nil {
		if err := target.Validate(); err != nil {
			return Candidate[T]{}, err
		}
		if target.Family() != family {
			return Candidate[T]{}, &InvalidCandidateError{Field: "target", Detail: "must belong to the proposed family"}
		}
	}
	for field, value := range map[string]*string{"reason": reason, "decision_reason": decisionReason} {
		if value != nil {
			if err := candidateText(field, *value, MaxReasonLength); err != nil {
				return Candidate[T]{}, err
			}
		}
	}
	if status == Approved {
		if result == nil {
			return Candidate[T]{}, &InvalidCandidateError{Field: "result_artifact", Detail: "approved Candidate must identify its result Artifact"}
		}
	} else if result != nil {
		return Candidate[T]{}, &InvalidCandidateError{Field: "result_artifact", Detail: "only an approved Candidate may identify a result Artifact"}
	}
	if result != nil {
		if err := result.Validate(); err != nil {
			return Candidate[T]{}, err
		}
	}
	if status == Rejected {
		if decisionReason == nil {
			return Candidate[T]{}, &InvalidCandidateError{Field: "decision_reason", Detail: "rejected Candidate must include a decision reason"}
		}
	} else if decisionReason != nil {
		return Candidate[T]{}, &InvalidCandidateError{Field: "decision_reason", Detail: "only a rejected Candidate may include a decision reason"}
	}
	return Candidate[T]{
		candidateID: candidateID, version: version, family: family, status: status, proposal: proposal,
		sources: slices.Clone(sources), artifacts: slices.Clone(artifacts), target: cloneRef(target),
		reason: cloneText(reason), resultArtifact: cloneRef(result), decisionReason: cloneText(decisionReason),
	}, nil
}

func (c Candidate[T]) ID() string                    { return c.candidateID }
func (c Candidate[T]) Version() int64                { return c.version }
func (c Candidate[T]) Family() string                { return c.family }
func (c Candidate[T]) Status() Status                { return c.status }
func (c Candidate[T]) Proposal() T                   { return c.proposal }
func (c Candidate[T]) ProposalValue() any            { return c.proposal }
func (c Candidate[T]) Sources() []source.Ref         { return slices.Clone(c.sources) }
func (c Candidate[T]) Artifacts() []artifact.Ref     { return slices.Clone(c.artifacts) }
func (c Candidate[T]) Target() *artifact.Ref         { return cloneRef(c.target) }
func (c Candidate[T]) Reason() *string               { return cloneText(c.reason) }
func (c Candidate[T]) ResultArtifact() *artifact.Ref { return cloneRef(c.resultArtifact) }
func (c Candidate[T]) DecisionReason() *string       { return cloneText(c.decisionReason) }

type Snapshot interface {
	ID() string
	Version() int64
	Family() string
	Status() Status
	ProposalValue() any
	Sources() []source.Ref
	Artifacts() []artifact.Ref
	Target() *artifact.Ref
	Reason() *string
	ResultArtifact() *artifact.Ref
	DecisionReason() *string
}

type Page struct {
	Candidates []Snapshot
	NextCursor *string
}

func (p Page) Clone() Page {
	p.Candidates = slices.Clone(p.Candidates)
	p.NextCursor = cloneText(p.NextCursor)
	return p
}

func candidateText(field, value string, maximum int) error {
	if !utf8.ValidString(value) || strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
		return &InvalidCandidateError{Field: field, Detail: "must be a non-empty trimmed string"}
	}
	if maximum > 0 && utf8.RuneCountInString(value) > maximum {
		return &InvalidCandidateError{Field: field, Detail: fmt.Sprintf("must not exceed %d characters", maximum)}
	}
	return nil
}

func cloneRef(value *artifact.Ref) *artifact.Ref {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneText(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
