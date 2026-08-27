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

package runtime

import (
	"testing"

	"github.com/ob-labs/powercontext-go/artifact/memory"
	"github.com/ob-labs/powercontext-go/inference"
)

func TestMemorySearchPageRetainsAndClonesRerankTrace(t *testing.T) {
	inputTokens, outputTokens := int64(7), int64(3)
	original := MemorySearchPage{Rerank: &memory.RerankTrace{
		PolicyID:      "powercontext.memory.rerank.listwise.v1",
		CandidateHits: []memory.Hit{{Text: "candidate", MatchedBy: []memory.MatchedBy{memory.MatchedFTS}}},
		SelectedRanks: []int{1},
		Usage: inference.Usage{
			Requests: 1, InputTokens: &inputTokens, OutputTokens: &outputTokens,
		},
	}}

	cloned := cloneSearchPage(original)
	if cloned.Rerank == nil || cloned.Rerank.PolicyID != original.Rerank.PolicyID || cloned.Rerank.Usage.Requests != 1 {
		t.Fatalf("rerank trace = %#v", cloned.Rerank)
	}
	cloned.Rerank.CandidateHits[0].MatchedBy[0] = memory.MatchedVector
	cloned.Rerank.SelectedRanks[0] = 2
	*cloned.Rerank.Usage.InputTokens = 99
	if original.Rerank.CandidateHits[0].MatchedBy[0] != memory.MatchedFTS ||
		original.Rerank.SelectedRanks[0] != 1 || *original.Rerank.Usage.InputTokens != 7 {
		t.Fatalf("cloned rerank trace mutated its authority: %#v", original.Rerank)
	}
}
