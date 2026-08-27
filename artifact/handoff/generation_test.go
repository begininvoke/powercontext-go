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

package handoff

import (
	"context"
	"errors"
	"testing"

	"github.com/ob-labs/powercontext-go/inference"
	"github.com/ob-labs/powercontext-go/source"
)

type handoffGeneratorFunc func(context.Context, GenerationInput) (inference.GenerationResult[GenerationOutput], error)

func (f handoffGeneratorFunc) Generate(ctx context.Context, input GenerationInput) (inference.GenerationResult[GenerationOutput], error) {
	return f(ctx, input)
}

func TestLLMGenerationPipelineMapsBoundedEvidence(t *testing.T) {
	t.Parallel()
	evidence := testHandoffSourceEvidence(t)
	state := NewGenerationStatement("The parser returns a stable public error.", []string{"source:0"})
	next := NewGenerationStatement("Run public-interface regression tests.", []string{"source:0"})
	output, err := NewGenerationOutput([]GenerationStatement{state}, Continuable, &next, nil)
	if err != nil {
		t.Fatal(err)
	}
	var recorded GenerationInput
	pipeline, err := NewLLMGenerationPipeline(handoffGeneratorFunc(func(_ context.Context, input GenerationInput) (inference.GenerationResult[GenerationOutput], error) {
		recorded = input
		return inference.GenerationResult[GenerationOutput]{Output: output}, nil
	}), NewContentEvidenceProjector(nil))
	if err != nil {
		t.Fatal(err)
	}
	request, err := NewGenerationRequest("Complete parser error handling.", []Evidence{evidence}, 4096)
	if err != nil {
		t.Fatal(err)
	}
	draft, err := pipeline.Generate(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	content := draft.AsContent()
	if content.Objective() != request.Objective() || content.NextAction() == nil {
		t.Fatalf("draft = %#v", content)
	}
	wantCitation := evidence.Citation()
	if got := content.State()[0].Citations(); len(got) != 1 || got[0].citationKey() != wantCitation.citationKey() {
		t.Fatalf("state citations = %#v", got)
	}
	if got := content.NextAction().Citations(); len(got) != 1 || got[0].citationKey() != wantCitation.citationKey() {
		t.Fatalf("next-action citations = %#v", got)
	}
	visible := recorded.Evidence()
	if recorded.Objective() != content.Objective() || recorded.MaxBytes() != 4096 ||
		len(visible) != 1 || visible[0].EvidenceID() != "source:0" {
		t.Fatalf("generator input = %#v", recorded)
	}
	projected, ok := visible[0].Content().(map[string]any)
	if !ok || projected["content"] != "The parser now returns a stable public error." {
		t.Fatalf("projected evidence = %#v", visible[0].Content())
	}
}

func TestLLMGenerationPipelineRejectsInvalidTypedOutputTree(t *testing.T) {
	t.Parallel()
	pipeline, err := NewLLMGenerationPipeline(handoffGeneratorFunc(func(context.Context, GenerationInput) (inference.GenerationResult[GenerationOutput], error) {
		return inference.GenerationResult[GenerationOutput]{Output: GenerationOutput{}}, nil
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	request, err := NewGenerationRequest("Complete parser error handling.", []Evidence{testHandoffSourceEvidence(t)}, 4096)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pipeline.Generate(context.Background(), request)
	var invalid *inference.InvalidOutputError
	if !errors.As(err, &invalid) || invalid.Detail() != "generator returned an invalid output tree" {
		t.Fatalf("error = %T %v", err, err)
	}
}

func TestLLMGenerationPipelineRejectsUnvalidatedRequest(t *testing.T) {
	t.Parallel()
	called := false
	pipeline, err := NewLLMGenerationPipeline(handoffGeneratorFunc(func(context.Context, GenerationInput) (inference.GenerationResult[GenerationOutput], error) {
		called = true
		return inference.GenerationResult[GenerationOutput]{}, nil
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pipeline.Generate(context.Background(), GenerationRequest{}); err == nil {
		t.Fatal("pipeline accepted an unvalidated zero request")
	}
	if called {
		t.Fatal("generator was called for an invalid request")
	}
}

func TestLLMGenerationPipelineRejectsOutsideEvidence(t *testing.T) {
	t.Parallel()
	output, _ := NewGenerationOutput(
		[]GenerationStatement{NewGenerationStatement("Unsupported.", []string{"source:99"})},
		Blocked, nil, nil,
	)
	pipeline, _ := NewLLMGenerationPipeline(handoffGeneratorFunc(func(context.Context, GenerationInput) (inference.GenerationResult[GenerationOutput], error) {
		return inference.GenerationResult[GenerationOutput]{Output: output}, nil
	}), nil)
	request, _ := NewGenerationRequest("Complete parser error handling.", []Evidence{testHandoffSourceEvidence(t)}, 4096)
	_, err := pipeline.Generate(context.Background(), request)
	var invalid *inference.InvalidOutputError
	if !errors.As(err, &invalid) || invalid.Detail() != "generated content cites evidence outside the request" {
		t.Fatalf("error = %T %v", err, err)
	}
}

func TestLLMGenerationPipelineRejectsUncitedStatement(t *testing.T) {
	t.Parallel()
	output, _ := NewGenerationOutput(
		[]GenerationStatement{NewGenerationStatement("Unsupported.", nil)}, Blocked, nil, nil,
	)
	pipeline, _ := NewLLMGenerationPipeline(handoffGeneratorFunc(func(context.Context, GenerationInput) (inference.GenerationResult[GenerationOutput], error) {
		return inference.GenerationResult[GenerationOutput]{Output: output}, nil
	}), nil)
	request, _ := NewGenerationRequest("Complete parser error handling.", []Evidence{testHandoffSourceEvidence(t)}, 4096)
	_, err := pipeline.Generate(context.Background(), request)
	var invalid *inference.InvalidOutputError
	if !errors.As(err, &invalid) || invalid.Detail() != "statement does not cite evidence" {
		t.Fatalf("error = %T %v", err, err)
	}
}

func TestGenerationInputJSONMatchesFrozenShape(t *testing.T) {
	t.Parallel()
	request, _ := NewGenerationRequest("Continue.", []Evidence{testHandoffSourceEvidence(t)}, 4096)
	input, _, err := projectGenerationInput(request, NewContentEvidenceProjector(nil))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := input.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	want := `{"objective":"Continue.","evidence":[{"evidence_id":"source:0","evidence_type":"source","content":{"source_type":"content","source_id":"turn-1","content":"The parser now returns a stable public error.","metadata":{"kind":"task-outcome"}}}],"max_bytes":4096}`
	if string(encoded) != want {
		t.Fatalf("JSON = %s\nwant = %s", encoded, want)
	}
}

func testHandoffSourceEvidence(t *testing.T) SourceEvidence {
	t.Helper()
	ref, err := source.NewRef(source.ContentType, "turn-1")
	if err != nil {
		t.Fatal(err)
	}
	citation, err := NewSourceCitation(ref)
	if err != nil {
		t.Fatal(err)
	}
	value, err := source.RestoreContentSource(
		"turn-1", source.Captured, nil,
		"The parser now returns a stable public error.",
		map[string]any{"kind": "task-outcome"},
	)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := NewSourceEvidence(citation, value)
	if err != nil {
		t.Fatal(err)
	}
	return evidence
}
