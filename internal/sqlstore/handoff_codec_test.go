package sqlstore

import (
	"strings"
	"testing"
)

func TestHandoffCodecRejectsUnknownContentSchema(t *testing.T) {
	t.Parallel()
	_, err := decodeHandoff([]byte(`{
        "schema":"powercontext.handoff.v2",
        "objective":"objective",
        "state":[],
        "disposition":"complete",
        "next_action":null,
        "omissions":[]
    }`))
	if err == nil || !strings.Contains(err.Error(), `unsupported Handoff schema "powercontext.handoff.v2"`) {
		t.Fatalf("error = %v, want unsupported schema", err)
	}
}
