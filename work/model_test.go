package work

import (
	"errors"
	"strings"
	"testing"

	"github.com/ob-labs/powercontext-go/artifact"
	"github.com/ob-labs/powercontext-go/artifact/handoff"
	"github.com/ob-labs/powercontext-go/source"
)

func TestVerifiedWorkClaimsRequireExactEvidence(t *testing.T) {
	t.Parallel()
	if _, err := NewClaim("Tests pass.", Verified, nil); err == nil || !strings.Contains(err.Error(), "require exact evidence") {
		t.Fatalf("verified claim error = %v", err)
	}

	citation := workSourceCitation(t, "test-output")
	if _, err := NewClaim("Tests pass.", Declared, []handoff.Citation{citation}); err == nil ||
		!strings.Contains(err.Error(), "cannot present evidence as verified") {
		t.Fatalf("declared claim error = %v", err)
	}

	evidence := make([]handoff.Citation, MaxClaimEvidence+1)
	for index := range evidence {
		evidence[index] = workSourceCitation(t, "evidence-"+string(rune('a'+index)))
	}
	if _, err := NewClaim("The boundary reserves one citation.", Verified, evidence); err == nil ||
		!strings.Contains(err.Error(), "at most 31 items") {
		t.Fatalf("oversized evidence error = %v", err)
	}
}

func TestAcknowledgementRequiresInspectedTargetAndReceiverChecks(t *testing.T) {
	t.Parallel()
	revision, err := artifact.NewRef(handoff.Family, "handoff", 1)
	if err != nil {
		t.Fatal(err)
	}
	confirmed, err := NewReceiverChecks(LiveStateConfirmed, ReadinessConfirmed, ReadinessConfirmed)
	if err != nil {
		t.Fatal(err)
	}
	unchecked, err := NewReceiverChecks(LiveStateNotChecked, ReadinessNotChecked, ReadinessNotChecked)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		selection handoff.Selection
		checks    *ReceiverChecks
		revision  *artifact.Ref
		contains  string
	}{
		{name: "latest", selection: handoff.LatestSelection, checks: &confirmed, contains: "must be prepared or exact"},
		{name: "missing checks", selection: handoff.ExactSelection, revision: &revision, contains: "requires all receiver checks"},
		{name: "unchecked", selection: handoff.ExactSelection, checks: &unchecked, revision: &revision, contains: "requires all receiver checks"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewAcknowledge(
				"receipt-1", "receiver-agent", ReceiptAccepted, test.selection,
				test.checks, nil, test.revision, nil,
			)
			if err == nil || !strings.Contains(err.Error(), test.contains) {
				t.Fatalf("error = %v, want %q", err, test.contains)
			}
			var invalid *InvalidError
			if !errors.As(err, &invalid) {
				t.Fatalf("error = %T, want InvalidError", err)
			}
		})
	}
}

func workSourceCitation(t *testing.T, sourceID string) handoff.SourceCitation {
	t.Helper()
	ref, err := source.NewRef(source.ContentType, sourceID)
	if err != nil {
		t.Fatal(err)
	}
	citation, err := handoff.NewSourceCitation(ref)
	if err != nil {
		t.Fatal(err)
	}
	return citation
}
