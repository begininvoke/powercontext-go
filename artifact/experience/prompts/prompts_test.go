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
