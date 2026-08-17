package review

import (
	"fmt"

	"github.com/thunguo/powercontext-go/artifact"
)

type CandidateNotFoundError struct{ CandidateID string }

func (*CandidateNotFoundError) Error() string { return "Candidate was not found" }

type CandidateConflictError struct {
	CandidateID     string
	ExpectedVersion int64
	CurrentVersion  int64
}

func (*CandidateConflictError) Error() string { return "Candidate version is stale" }

type CandidateTerminalError struct {
	CandidateID string
	Status      Status
}

func (*CandidateTerminalError) Error() string { return "Candidate is already terminal" }

type InvalidCandidateError struct {
	Field  string
	Detail string
}

func (e *InvalidCandidateError) Error() string {
	return fmt.Sprintf("invalid Candidate %s: %s", e.Field, e.Detail)
}

type ArtifactTargetConflictError struct {
	Target  artifact.Ref
	Current artifact.Ref
}

func (*ArtifactTargetConflictError) Error() string {
	return "Candidate target is not the current Artifact head"
}

type GenerationCapabilityUnavailableError struct{ Family string }

func (e *GenerationCapabilityUnavailableError) Error() string {
	return e.Family + " generation is not configured"
}
