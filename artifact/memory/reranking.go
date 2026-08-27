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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"

	"github.com/ob-labs/powercontext-go/artifact/memory/prompts"
	"github.com/ob-labs/powercontext-go/inference"
)

type RerankCandidate struct {
	rank int
	text string
}

func (c RerankCandidate) Rank() int    { return c.rank }
func (c RerankCandidate) Text() string { return c.text }

type RerankInput struct {
	query      string
	maxResults int
	candidates []RerankCandidate
}

func (i RerankInput) Query() string                 { return i.query }
func (i RerankInput) MaxResults() int               { return i.maxResults }
func (i RerankInput) Candidates() []RerankCandidate { return slices.Clone(i.candidates) }

func (i RerankInput) MarshalJSON() ([]byte, error) {
	type candidateDTO struct {
		Rank int    `json:"rank"`
		Text string `json:"text"`
	}
	values := make([]candidateDTO, len(i.candidates))
	for index, candidate := range i.candidates {
		values[index] = candidateDTO{Rank: candidate.rank, Text: candidate.text}
	}
	return marshalRerankJSON(struct {
		Query      string         `json:"query"`
		MaxResults int            `json:"max_results"`
		Candidates []candidateDTO `json:"candidates"`
	}{Query: i.query, MaxResults: i.maxResults, Candidates: values})
}

func marshalRerankJSON(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'}), nil
}

type RerankOutput struct{ selectedRanks []int }

func NewRerankOutput(selectedRanks []int) RerankOutput {
	return RerankOutput{selectedRanks: slices.Clone(selectedRanks)}
}

func (o RerankOutput) SelectedRanks() []int { return slices.Clone(o.selectedRanks) }

type RerankDecision struct {
	selectedRanks      []int
	usage              inference.Usage
	discardedRankCount int
	usedFallback       bool
}

func (d RerankDecision) SelectedRanks() []int    { return slices.Clone(d.selectedRanks) }
func (d RerankDecision) Usage() inference.Usage  { return cloneInferenceUsage(d.usage) }
func (d RerankDecision) DiscardedRankCount() int { return d.discardedRankCount }
func (d RerankDecision) UsedFallback() bool      { return d.usedFallback }

type Reranker interface {
	PolicyID() string
	Rerank(context.Context, string, []Hit, int) (RerankDecision, error)
}

type LLMReranker struct {
	generator inference.StructuredGenerator[RerankInput, RerankOutput]
}

func NewLLMReranker(
	generator inference.StructuredGenerator[RerankInput, RerankOutput],
) (*LLMReranker, error) {
	if generator == nil {
		return nil, fmt.Errorf("Memory rerank generator must not be nil")
	}
	return &LLMReranker{generator: generator}, nil
}

func NewRerankPromptedGenerator(
	model inference.TextModel,
	limits *inference.Limits,
) (*inference.PromptedGenerator[RerankInput, RerankOutput], error) {
	codec, err := inference.NewJSONCodec[RerankInput, RerankOutput](
		prompts.RerankSchema(), nil, decodeRerankOutput,
	)
	if err != nil {
		return nil, err
	}
	temperature := 0.0
	settings, err := inference.NewGenerationSettings(&temperature, nil)
	if err != nil {
		return nil, err
	}
	return inference.NewPromptedGenerator(model, prompts.Rerank(), codec, limits, settings)
}

func (*LLMReranker) PolicyID() string { return prompts.RerankVersion }

func (r *LLMReranker) Rerank(
	ctx context.Context,
	query string,
	candidates []Hit,
	limit int,
) (RerankDecision, error) {
	if len(candidates) < 1 {
		return RerankDecision{}, fmt.Errorf("rerank requires at least one candidate")
	}
	if limit < 1 || limit > len(candidates) {
		return RerankDecision{}, fmt.Errorf("rerank limit must be between one and the candidate count")
	}
	if query == "" {
		return RerankDecision{}, fmt.Errorf("rerank query must not be empty")
	}
	projected := make([]RerankCandidate, len(candidates))
	for index, candidate := range candidates {
		if candidate.Text == "" {
			return RerankDecision{}, fmt.Errorf("rerank candidate text must not be empty")
		}
		projected[index] = RerankCandidate{rank: index + 1, text: candidate.Text}
	}
	result, err := r.generator.Generate(ctx, RerankInput{
		query: query, maxResults: limit, candidates: projected,
	})
	if err != nil {
		return RerankDecision{}, err
	}
	selected, discarded := normalizeRanks(result.Output.selectedRanks, len(candidates), limit)
	decision := RerankDecision{
		selectedRanks:      selected,
		usage:              cloneInferenceUsage(result.Usage),
		discardedRankCount: discarded,
	}
	if len(selected) == 0 {
		decision.selectedRanks = make([]int, limit)
		for index := range limit {
			decision.selectedRanks[index] = index + 1
		}
		decision.usedFallback = true
	}
	return decision, nil
}

func normalizeRanks(values []int, candidateCount, limit int) ([]int, int) {
	selected := make([]int, 0, limit)
	seen := make(map[int]struct{}, limit)
	discarded := 0
	for _, rank := range values {
		if rank < 1 || rank > candidateCount {
			discarded++
			continue
		}
		if _, exists := seen[rank]; exists {
			discarded++
			continue
		}
		seen[rank] = struct{}{}
		selected = append(selected, rank)
		if len(selected) == limit {
			break
		}
	}
	return selected, discarded
}

func decodeRerankOutput(encoded []byte) (RerankOutput, error) {
	var value struct {
		SelectedRanks *[]int `json:"selected_ranks"`
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil || value.SelectedRanks == nil {
		return RerankOutput{}, fmt.Errorf("Memory rerank output is invalid")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return RerankOutput{}, fmt.Errorf("Memory rerank output has trailing data")
	}
	return NewRerankOutput(*value.SelectedRanks), nil
}

func cloneInferenceUsage(value inference.Usage) inference.Usage {
	if value.InputTokens != nil {
		copy := *value.InputTokens
		value.InputTokens = &copy
	}
	if value.OutputTokens != nil {
		copy := *value.OutputTokens
		value.OutputTokens = &copy
	}
	return value
}
