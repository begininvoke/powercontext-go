package review_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/ob-labs/powercontext-go/artifact"
	"github.com/ob-labs/powercontext-go/review"
	"github.com/ob-labs/powercontext-go/source"
)

func TestCandidateRequiresBoundedExactEvidence(t *testing.T) {
	t.Parallel()

	_, err := review.NewCandidate(
		"candidate-1", 1, "experience", review.Pending, "proposal",
		nil, nil, nil, nil, nil, nil,
	)
	assertInvalidCandidateField(t, err, "evidence")

	sources := make([]source.Ref, review.MaxEvidence+1)
	for index := range sources {
		sources[index] = mustSourceRef(t, "content", fmt.Sprintf("source-%d", index))
	}
	_, err = review.NewCandidate(
		"candidate-1", 1, "experience", review.Pending, "proposal",
		sources, nil, nil, nil, nil, nil,
	)
	assertInvalidCandidateField(t, err, "evidence")
}

func TestCandidateTerminalFieldsMatchStatus(t *testing.T) {
	t.Parallel()

	evidence := []source.Ref{mustSourceRef(t, "content", "source-1")}
	result := mustArtifactRef(t, "experience", "experience-1", 1)
	decision := "not supported by the evidence"

	tests := []struct {
		name     string
		status   review.Status
		result   *artifact.Ref
		decision *string
		field    string
	}{
		{name: "approved requires result", status: review.Approved, field: "result_artifact"},
		{name: "pending rejects result", status: review.Pending, result: &result, field: "result_artifact"},
		{name: "rejected requires decision", status: review.Rejected, field: "decision_reason"},
		{name: "pending rejects decision", status: review.Pending, decision: &decision, field: "decision_reason"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := review.NewCandidate(
				"candidate-1", 1, "experience", test.status, "proposal",
				evidence, nil, nil, nil, test.result, test.decision,
			)
			assertInvalidCandidateField(t, err, test.field)
		})
	}
}

func TestCandidateCopiesEvidenceAndOptionalFields(t *testing.T) {
	t.Parallel()

	sourceRef := mustSourceRef(t, "content", "source-1")
	artifactRef := mustArtifactRef(t, "experience", "experience-1", 1)
	reason := "bounded reason"
	candidate, err := review.NewCandidate(
		"candidate-1", 1, "experience", review.Pending, "proposal",
		[]source.Ref{sourceRef}, []artifact.Ref{artifactRef}, &artifactRef, &reason, nil, nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	sources := candidate.Sources()
	artifacts := candidate.Artifacts()
	target := candidate.Target()
	gotReason := candidate.Reason()
	sources[0] = mustSourceRef(t, "content", "mutated")
	artifacts[0] = mustArtifactRef(t, "experience", "mutated", 9)
	*target = artifacts[0]
	*gotReason = "mutated"

	if candidate.Sources()[0] != sourceRef || candidate.Artifacts()[0] != artifactRef ||
		*candidate.Target() != artifactRef || *candidate.Reason() != reason {
		t.Fatalf("Candidate exposed mutable aggregate state: %#v", candidate)
	}
}

func assertInvalidCandidateField(t *testing.T, err error, field string) {
	t.Helper()
	var invalid *review.InvalidCandidateError
	if !errors.As(err, &invalid) || invalid.Field != field {
		t.Fatalf("expected invalid Candidate field %q, got %v", field, err)
	}
}

func mustSourceRef(t *testing.T, sourceType, id string) source.Ref {
	t.Helper()
	value, err := source.NewRef(sourceType, id)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustArtifactRef(t *testing.T, family, id string, revision int64) artifact.Ref {
	t.Helper()
	value, err := artifact.NewRef(family, id, revision)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
