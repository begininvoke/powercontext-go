package prompts

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

func TestFrozenPromptHashes(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		text string
		want string
	}{
		"coding":       {Coding(), "835eeb45aaeef72fd1d1b639bad2f39e116d6311b3bbc028a19343e797ca8b18"},
		"conversation": {Conversation(), "57d44a686f446da1890c47deae777c3b4ef181f5b3b2e6913ffc0dbb12306e27"},
		"rerank":       {Rerank(), "9a79f4ffce84170c000cc85e6998102920b1c8b49d93bfe2bcdab540a666e148"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := fmt.Sprintf("%x", sha256.Sum256([]byte(test.text)))
			if got != test.want {
				t.Fatalf("prompt hash = %s, want frozen Python hash %s", got, test.want)
			}
		})
	}
}
