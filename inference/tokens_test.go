package inference_test

import (
	"testing"

	"github.com/ob-labs/powercontext-go/inference"
)

func TestCharacterEstimatorIsDeterministicForASCIIAndNonASCIIText(t *testing.T) {
	estimator := inference.CharacterTokenEstimator()
	profile := estimator.Profile()
	if profile.EstimatorID() != "character:weighted" || profile.Version() != "1" {
		t.Fatalf("profile = %q/%q", profile.EstimatorID(), profile.Version())
	}
	tests := map[string]int{"": 0, "abcd": 1, "abcde": 2, "上下文": 3, "ab上下": 3}
	for input, want := range tests {
		got, err := estimator.Estimate(input)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("estimate(%q) = %d, want %d", input, got, want)
		}
	}
}
