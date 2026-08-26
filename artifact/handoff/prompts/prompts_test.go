package prompts

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

func TestFrozenPromptHash(t *testing.T) {
	t.Parallel()
	want := "6352014a63ecdbdb79b83cd6be57fc5e93dc8e7124115e4309ad898f54198cfe"
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(Generation()))); got != want {
		t.Fatalf("prompt hash = %s, want frozen Python hash %s", got, want)
	}
}
