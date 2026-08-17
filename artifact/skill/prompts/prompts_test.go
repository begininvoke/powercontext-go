package prompts

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

func TestFrozenPromptHash(t *testing.T) {
	t.Parallel()
	want := "8b11347d7c48a141d8dd684f6295ea5fd83ed0f8bd91f275d3504258bc0266be"
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(Generation()))); got != want {
		t.Fatalf("prompt hash = %s, want frozen Python hash %s", got, want)
	}
}
