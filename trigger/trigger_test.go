package trigger_test

import (
	"testing"

	"github.com/thunguo/powercontext-go/trigger"
)

func TestTransitionCopiesActions(t *testing.T) {
	actions := []string{"one", "two"}
	transition := trigger.NewTransition(3, actions...)
	actions[0] = "changed"
	got := transition.Actions()
	if got[0] != "one" || transition.State() != 3 {
		t.Fatalf("unexpected transition: %v %v", transition.State(), got)
	}
	got[0] = "changed again"
	if transition.Actions()[0] != "one" {
		t.Fatal("actions accessor leaked mutable storage")
	}
}
