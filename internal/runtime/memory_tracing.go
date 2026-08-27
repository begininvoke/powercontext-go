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
	"context"

	"github.com/ob-labs/powercontext-go/artifact/memory"
)

type tracedMemoryReranker struct {
	runtime  *Runtime
	delegate memory.Reranker
}

// TraceMemoryReranker records the bounded rerank stage around the actual model
// call, so provider spans remain children of memory.rerank and no query or
// candidate text becomes an attribute.
func TraceMemoryReranker(runtime *Runtime, delegate memory.Reranker) memory.Reranker {
	if runtime == nil || delegate == nil {
		return delegate
	}
	return &tracedMemoryReranker{runtime: runtime, delegate: delegate}
}

func (r *tracedMemoryReranker) PolicyID() string { return r.delegate.PolicyID() }

func (r *tracedMemoryReranker) Rerank(
	ctx context.Context,
	query string,
	candidates []memory.Hit,
	limit int,
) (memory.RerankDecision, error) {
	var decision memory.RerankDecision
	err := r.runtime.runStage(ctx, "memory.rerank", map[string]TraceAttribute{
		"powercontext.memory.rerank.candidate_count": len(candidates),
		"powercontext.memory.rerank.limit":           limit,
	}, func(stageContext context.Context, span StageSpan) error {
		var rerankErr error
		decision, rerankErr = r.delegate.Rerank(stageContext, query, candidates, limit)
		if rerankErr == nil {
			setStageAttributes(span, map[string]TraceAttribute{
				"powercontext.memory.rerank.selected_count":       len(decision.SelectedRanks()),
				"powercontext.memory.rerank.discarded_rank_count": decision.DiscardedRankCount(),
				"powercontext.memory.rerank.used_fallback":        decision.UsedFallback(),
			})
		}
		return rerankErr
	})
	return decision, err
}

var _ memory.Reranker = (*tracedMemoryReranker)(nil)
