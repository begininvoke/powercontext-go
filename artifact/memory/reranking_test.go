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

package memory

import (
	"context"
	"reflect"
	"testing"

	"github.com/ob-labs/powercontext-go/inference"
)

type rerankGeneratorFunc func(context.Context, RerankInput) (inference.GenerationResult[RerankOutput], error)

func (f rerankGeneratorFunc) Generate(ctx context.Context, input RerankInput) (inference.GenerationResult[RerankOutput], error) {
	return f(ctx, input)
}

func TestLLMRerankerNormalizesSparseRanks(t *testing.T) {
	t.Parallel()
	inputTokens := int64(7)
	var recorded RerankInput
	generator := rerankGeneratorFunc(func(_ context.Context, input RerankInput) (inference.GenerationResult[RerankOutput], error) {
		recorded = input
		return inference.GenerationResult[RerankOutput]{
			Output: NewRerankOutput([]int{3, 3, 0, 2, 1}),
			Usage:  inference.Usage{Requests: 1, InputTokens: &inputTokens},
		}, nil
	})
	reranker, err := NewLLMReranker(generator)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := reranker.Rerank(context.Background(), "exact date", []Hit{
		{Text: "first"}, {Text: "second"}, {Text: "third"},
	}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decision.SelectedRanks(), []int{3, 2}) || decision.DiscardedRankCount() != 2 || decision.UsedFallback() {
		t.Fatalf("decision = %#v", decision)
	}
	if recorded.MaxResults() != 2 || recorded.Candidates()[2].Rank() != 3 {
		t.Fatalf("generator input = %#v", recorded)
	}
	usage := decision.Usage()
	*usage.InputTokens = 99
	if *decision.Usage().InputTokens != 7 {
		t.Fatal("rerank decision leaked mutable usage pointers")
	}
}

func TestLLMRerankerFallsBackToCoarseOrder(t *testing.T) {
	t.Parallel()
	reranker, _ := NewLLMReranker(rerankGeneratorFunc(func(context.Context, RerankInput) (inference.GenerationResult[RerankOutput], error) {
		return inference.GenerationResult[RerankOutput]{Output: NewRerankOutput([]int{0, 9, 0})}, nil
	}))
	decision, err := reranker.Rerank(context.Background(), "query", []Hit{{Text: "one"}, {Text: "two"}}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decision.SelectedRanks(), []int{1, 2}) || decision.DiscardedRankCount() != 3 || !decision.UsedFallback() {
		t.Fatalf("decision = %#v", decision)
	}
}

func TestNormalizeRanksStopsAfterLimit(t *testing.T) {
	t.Parallel()
	selected, discarded := normalizeRanks([]int{1, 2, 2, 99}, 3, 2)
	if !reflect.DeepEqual(selected, []int{1, 2}) || discarded != 0 {
		t.Fatalf("selected=%v discarded=%d", selected, discarded)
	}
}

func TestRerankInputJSONMatchesFrozenShape(t *testing.T) {
	t.Parallel()
	input := RerankInput{
		query: "<when>", maxResults: 1,
		candidates: []RerankCandidate{{rank: 1, text: "answer"}},
	}
	encoded, err := input.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	want := `{"query":"<when>","max_results":1,"candidates":[{"rank":1,"text":"answer"}]}`
	if string(encoded) != want {
		t.Fatalf("JSON = %s, want %s", encoded, want)
	}
}
