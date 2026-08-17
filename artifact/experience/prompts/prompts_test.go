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
		"incubation": {Incubation(), "e1b1083e70c67a607a4d033cca5b6653ef2f7c5c726537ef4845aa284031bbab"},
		"generation": {Generation(), "919daa5e393f39a9bd79036e0e72050a432bade2bf2e0eaa2b2c067c951b30b0"},
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
