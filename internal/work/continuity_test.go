package work

import (
	"testing"

	"github.com/ob-labs/powercontext-go/artifact"
	"github.com/ob-labs/powercontext-go/artifact/handoff"
	"github.com/ob-labs/powercontext-go/source"
)

func TestContinuityDoesNotCoverAcceptanceWithUnlinkedOutcome(t *testing.T) {
	t.Parallel()
	revision, err := artifact.NewRef(handoff.Family, "handoff", 1)
	if err != nil {
		t.Fatal(err)
	}
	checks, err := NewReceiverChecks(LiveStateConfirmed, ReadinessConfirmed, ReadinessConfirmed)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := NewHandoffReceipt(
		"receiver-agent", ReceiptAccepted, handoff.ExactSelection, &revision, nil, &checks,
		EvidenceAvailable, nil, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := NewTaskOutcome(
		"Complete unrelated work.", OutcomeSucceeded, "An unrelated attempt completed.", nil,
		[]Claim{mustClaim(t, "This result does not identify the accepted receipt.")}, nil, nil, nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	continuity, err := ProjectContinuity("project", []source.JournalEntry{
		workJournalEntry(t, 1, HandoffReceiptSourceKind, "receipt-1", receipt),
		workJournalEntry(t, 2, TaskOutcomeSourceKind, "unlinked-outcome", outcome),
	}, &revision)
	if err != nil {
		t.Fatal(err)
	}
	coverage := continuity.Coverage()
	if coverage.TransferState() != TransferAccepted || coverage.OutcomeState() != OutcomeAwaiting ||
		coverage.HandoffResultCovered() {
		t.Fatalf("coverage = %#v", coverage)
	}
}

func TestContinuityExcludesMalformedWorkRecords(t *testing.T) {
	t.Parallel()
	ordinary, err := source.RestoreContentSource(
		"ordinary-1", source.Captured, nil, "ordinary", map[string]any{"kind": []any{}},
	)
	if err != nil {
		t.Fatal(err)
	}
	ordinaryRef, err := source.NewRef(source.ContentType, "ordinary-1")
	if err != nil {
		t.Fatal(err)
	}
	ordinaryEntry, err := source.NewJournalEntry(ordinaryRef, ordinary, 1)
	if err != nil {
		t.Fatal(err)
	}
	malformed, err := source.RestoreContentSource(
		"malformed-1", source.Captured, nil, "{}", map[string]any{"kind": string(TaskOutcomeSourceKind)},
	)
	if err != nil {
		t.Fatal(err)
	}
	malformedRef, err := source.NewRef(source.ContentType, "malformed-1")
	if err != nil {
		t.Fatal(err)
	}
	malformedEntry, err := source.NewJournalEntry(malformedRef, malformed, 2)
	if err != nil {
		t.Fatal(err)
	}

	continuity, err := ProjectContinuity("project", []source.JournalEntry{ordinaryEntry, malformedEntry}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if continuity.TotalEventCount() != 0 || continuity.InvalidRecordCount() != 1 || len(continuity.Events()) != 0 {
		t.Fatalf("continuity = %#v", continuity)
	}
}

func workJournalEntry(t *testing.T, position int64, kind Kind, sourceID string, record any) source.JournalEntry {
	t.Helper()
	payload, err := EncodeRecord(record, true)
	if err != nil {
		t.Fatal(err)
	}
	content, err := source.RestoreContentSource(
		sourceID, source.Captured, nil, string(payload), map[string]any{"kind": string(kind)},
	)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := source.NewRef(source.ContentType, sourceID)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := source.NewJournalEntry(ref, content, position)
	if err != nil {
		t.Fatal(err)
	}
	return entry
}
