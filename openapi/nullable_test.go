package openapi

import (
	"encoding/json"
	"testing"

	v1 "github.com/thunguo/powercontext-go/api/v1"
)

func TestGeneratedNullableReferenceRoundTrip(t *testing.T) {
	t.Parallel()
	payload := []byte(`{"objective":"Transfer state.","state":[{"text":"Current state.","citations":[{"kind":"source","source_ref":{"name":"content","source_id":"turn-1"}}]}],"disposition":"continuable","next_action":null,"omissions":[{"text":"No omission evidence.","citation":null}]}`)
	var draft v1.HandoffDraft
	if err := json.Unmarshal(payload, &draft); err != nil {
		t.Fatal(err)
	}
	if !draft.NextAction.IsNull() || !draft.Omissions[0].Citation.IsNull() {
		t.Fatalf("nullable references were not preserved: %#v", draft)
	}
	encoded, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip map[string]any
	if err := json.Unmarshal(encoded, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if roundTrip["next_action"] != nil {
		t.Fatalf("next_action = %#v, want null", roundTrip["next_action"])
	}
	omissions := roundTrip["omissions"].([]any)
	if omissions[0].(map[string]any)["citation"] != nil {
		t.Fatalf("omission citation = %#v, want null", omissions[0])
	}
}

func TestGeneratedOptionalNullableReferenceAcceptsExplicitNull(t *testing.T) {
	t.Parallel()
	var request v1.ContinueHandoffRequest
	if err := json.Unmarshal([]byte(`{"scope_id":"project","selection":"latest","prepared":null,"revision":null}`), &request); err != nil {
		t.Fatal(err)
	}
	if !request.Prepared.IsSet() || !request.Prepared.IsNull() || !request.Revision.IsSet() || !request.Revision.IsNull() {
		t.Fatalf("explicit null option state was lost: %#v", request)
	}
}
