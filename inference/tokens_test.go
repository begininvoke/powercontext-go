// Copyright (c) 2026 OceanBase.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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
