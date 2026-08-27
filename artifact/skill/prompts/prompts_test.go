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

func TestFrozenPromptHash(t *testing.T) {
	t.Parallel()
	want := "8b11347d7c48a141d8dd684f6295ea5fd83ed0f8bd91f275d3504258bc0266be"
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(Generation()))); got != want {
		t.Fatalf("prompt hash = %s, want frozen Python hash %s", got, want)
	}
}
