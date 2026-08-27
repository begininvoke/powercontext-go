package work

import (
	"testing"

	"github.com/ob-labs/powercontext-go/artifact"
	"github.com/ob-labs/powercontext-go/artifact/handoff"
	"github.com/ob-labs/powercontext-go/source"
)

func TestPythonWorkRecordJSONAndDigests(t *testing.T) {
	declared := mustClaim(t, "Declared fact.")
	contract, err := NewContract(
		"迁移 <ready>", []Claim{declared}, []string{"Implement."}, nil,
		[]string{"Pass tests."}, nil, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	assertPythonRecord(t, contract, `{
  "schema": "powercontext.work-contract.v1",
  "trust": "untrusted_input",
  "objective": "迁移 <ready>",
  "facts": [
    {
      "text": "Declared fact.",
      "basis": "declared",
      "evidence": []
    }
  ],
  "in_scope": [
    "Implement."
  ],
  "exclusions": [],
  "completion_criteria": [
    "Pass tests."
  ],
  "authorization_notes": [],
  "open_questions": []
}`, "sha256:5ae1608f4c921b4cbf4aff64b230a251b352e93272738f79720489d5f4298bd3")

	state := mustClaim(t, "Ready.")
	current, err := NewCurrentHandoff("Continue.", []Claim{state}, handoff.Continuable, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertPythonRecord(t, current, `{
  "schema": "powercontext.current-work-handoff.v1",
  "trust": "untrusted_input",
  "objective": "Continue.",
  "state": [
    {
      "text": "Ready.",
      "basis": "declared",
      "evidence": []
    }
  ],
  "disposition": "continuable",
  "next_action": null,
  "omissions": []
}`, "sha256:426fb94142b58428d84400425700491fafcc3bda880ee8b09d0ac6a8e4d0bb98")

	receiptRef, err := source.NewRef("content", "receipt-1")
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := NewTaskOutcome(
		"Continue.", OutcomeSucceeded, "Done.", &receiptRef,
		[]Claim{mustClaim(t, "Passed.")}, nil, nil, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	assertPythonRecord(t, outcome, `{
  "schema": "powercontext.task-outcome.v1",
  "trust": "untrusted_observation",
  "objective": "Continue.",
  "status": "succeeded",
  "summary": "Done.",
  "handoff_receipt_ref": {
    "source_type": "content",
    "source_id": "receipt-1"
  },
  "observations": [
    {
      "text": "Passed.",
      "basis": "declared",
      "evidence": []
    }
  ],
  "checks": [],
  "produced_artifacts": [],
  "remaining_work": []
}`, "sha256:bf9176ed53b70fa734264f697f209c3f39c134adca5d9cc3a5bcd83a81de3073")

	revision, err := artifact.NewRef(handoff.Family, "handoff", 1)
	if err != nil {
		t.Fatal(err)
	}
	checks, err := NewReceiverChecks(LiveStateConfirmed, ReadinessConfirmed, ReadinessConfirmed)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := NewHandoffReceipt(
		"agent", ReceiptAccepted, handoff.ExactSelection, &revision, nil, &checks,
		EvidenceAvailable, nil, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	assertPythonRecord(t, receipt, `{
  "schema": "powercontext.handoff-receipt.v1",
  "trust": "untrusted_observation",
  "receiver": "agent",
  "status": "accepted",
  "selection": "exact",
  "selected_revision": {
    "family": "handoff",
    "artifact_id": "handoff",
    "revision": 1
  },
  "prepared_digest": null,
  "receiver_checks": {
    "live_state": "confirmed",
    "capability": "confirmed",
    "authorization": "confirmed"
  },
  "evidence_status": "available",
  "unavailable_evidence": [],
  "message": null
}`, "sha256:a64831a9220e9811fa352009ae88c10473aa80a866048a418bde4c9fa4322f75")
}

func TestPythonPreparedHandoffDigest(t *testing.T) {
	ref, err := source.NewRef("content", "boundary-1")
	if err != nil {
		t.Fatal(err)
	}
	citation, err := handoff.NewSourceCitation(ref)
	if err != nil {
		t.Fatal(err)
	}
	statement, err := handoff.NewStatement("Ready.", []handoff.Citation{citation})
	if err != nil {
		t.Fatal(err)
	}
	content, err := handoff.NewContent("Continue.", []handoff.Statement{statement}, handoff.Continuable, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := handoff.NewPrepared("scope", nil, content)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := PreparedDigest(prepared)
	if err != nil {
		t.Fatal(err)
	}
	if want := "sha256:6fcf075eb34f932b8fde382b23082d10e70f563d020531d024ebaae15a6b55ec"; digest != want {
		t.Fatalf("Prepared Handoff digest = %s, want Python %s", digest, want)
	}
}

func TestDecodeRecordRejectsUnknownAndMismatchedFields(t *testing.T) {
	for _, payload := range []string{
		`{"schema":"powercontext.work-contract.v1","trust":"untrusted_input","objective":"x","facts":[],"in_scope":["x"],"exclusions":[],"completion_criteria":["x"],"authorization_notes":[],"open_questions":[],"extra":true}`,
		`{"schema":"powercontext.task-outcome.v1","trust":"untrusted_input","objective":"x","status":"unknown","summary":"x","handoff_receipt_ref":null,"observations":[{"text":"x","basis":"declared","evidence":[]}],"checks":[],"produced_artifacts":[],"remaining_work":[]}`,
	} {
		kind := WorkContractSourceKind
		if payload[40:52] == "task-outcome" {
			kind = TaskOutcomeSourceKind
		}
		if _, err := DecodeRecord(kind, []byte(payload)); err == nil {
			t.Fatalf("DecodeRecord accepted %s", payload)
		}
	}
}

func assertPythonRecord(t *testing.T, value any, expected, expectedDigest string) {
	t.Helper()
	encoded, err := EncodeRecord(value, true)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != expected {
		t.Fatalf("record JSON:\n%s\nwant Python:\n%s", encoded, expected)
	}
	digest, err := ContentDigest(value)
	if err != nil {
		t.Fatal(err)
	}
	if digest != expectedDigest {
		t.Fatalf("content digest = %s, want Python %s", digest, expectedDigest)
	}
}

func mustClaim(t *testing.T, text string) Claim {
	t.Helper()
	value, err := NewClaim(text, Declared, nil)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
