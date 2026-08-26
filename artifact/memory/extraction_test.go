package memory

import (
	"context"
	"errors"
	"testing"

	"github.com/ob-labs/powercontext-go/artifact/memory/prompts"
	"github.com/ob-labs/powercontext-go/inference"
	"github.com/ob-labs/powercontext-go/source"
)

func TestRuntimeDefaultsToCodingExtractionProfile(t *testing.T) {
	instructions, err := ExtractionInstructions(CodingProfile)
	if err != nil {
		t.Fatal(err)
	}
	version, err := ExtractionInstructionsVersion(CodingProfile)
	if err != nil {
		t.Fatal(err)
	}
	if instructions != prompts.Coding() || version != prompts.CodingVersion {
		t.Fatalf("coding profile = %q/%q", version, instructions)
	}
}

func TestConversationProfileSelectsVersionedPolicy(t *testing.T) {
	instructions, err := ExtractionInstructions(ConversationProfile)
	if err != nil {
		t.Fatal(err)
	}
	version, err := ExtractionInstructionsVersion(ConversationProfile)
	if err != nil {
		t.Fatal(err)
	}
	if instructions != prompts.Conversation() || version != prompts.ConversationVersion || instructions == prompts.Coding() {
		t.Fatalf("conversation profile = %q/%q", version, instructions)
	}
}

type extractionGeneratorFunc func(context.Context, ExtractionInput) (inference.GenerationResult[ExtractionOutput], error)

func (f extractionGeneratorFunc) Generate(ctx context.Context, input ExtractionInput) (inference.GenerationResult[ExtractionOutput], error) {
	return f(ctx, input)
}

func TestLLMCandidatePipelineMapsOnlyBoundedEvidence(t *testing.T) {
	t.Parallel()
	content := testMemoryContentSource(t)
	request, err := NewCandidateRequest([]source.Value{content}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := NewExtractionCandidate(
		ExtractionAdd, "preference", "Use uv.", []string{"source:0", "source:0"}, nil, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	var recorded ExtractionInput
	generator := extractionGeneratorFunc(func(_ context.Context, input ExtractionInput) (inference.GenerationResult[ExtractionOutput], error) {
		recorded = input
		return inference.GenerationResult[ExtractionOutput]{Output: NewExtractionOutput([]ExtractionCandidate{candidate})}, nil
	})
	pipeline, err := NewLLMCandidatePipeline(generator, NewContentEvidenceProjector(nil))
	if err != nil {
		t.Fatal(err)
	}
	got, err := pipeline.Extract(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind() != "preference" || len(got[0].Sources()) != 1 || got[0].Entry() != nil {
		t.Fatalf("candidates = %#v", got)
	}
	visible := recorded.Evidence()
	if len(visible) != 1 || visible[0].EvidenceID() != "source:0" {
		t.Fatalf("evidence = %#v", visible)
	}
	projected, ok := visible[0].Content().(map[string]any)
	if !ok || projected["content"] != "Use uv for dependency management." {
		t.Fatalf("projected content = %#v", visible[0].Content())
	}
}

func TestLLMCandidatePipelineRejectsOutsideEvidence(t *testing.T) {
	t.Parallel()
	candidate, _ := NewExtractionCandidate(
		ExtractionAdd, "fact", "unsupported", []string{"source:99"}, nil, nil,
	)
	pipeline, _ := NewLLMCandidatePipeline(extractionGeneratorFunc(func(context.Context, ExtractionInput) (inference.GenerationResult[ExtractionOutput], error) {
		return inference.GenerationResult[ExtractionOutput]{Output: NewExtractionOutput([]ExtractionCandidate{candidate})}, nil
	}), nil)
	request, _ := NewCandidateRequest([]source.Value{testMemoryContentSource(t)}, nil, nil)
	_, err := pipeline.Extract(context.Background(), request)
	var invalid *inference.InvalidOutputError
	if !errors.As(err, &invalid) || invalid.Operation() != "memory-extract" {
		t.Fatalf("error = %T %v", err, err)
	}
}

func TestLLMCandidatePipelineAllowsOneRevisionPerEntry(t *testing.T) {
	t.Parallel()
	id := "entry-1"
	first, _ := NewExtractionCandidate(ExtractionRevise, "fact", "one", []string{"source:0"}, &id, nil)
	second, _ := NewExtractionCandidate(ExtractionRevise, "fact", "two", []string{"source:0"}, &id, nil)
	pipeline, _ := NewLLMCandidatePipeline(extractionGeneratorFunc(func(context.Context, ExtractionInput) (inference.GenerationResult[ExtractionOutput], error) {
		return inference.GenerationResult[ExtractionOutput]{Output: NewExtractionOutput([]ExtractionCandidate{first, second})}, nil
	}), nil)
	current := EntryVersion{EntryID: id, Kind: "fact", Text: "old"}
	request, _ := NewCandidateRequest([]source.Value{testMemoryContentSource(t)}, nil, []EntryVersion{current})
	_, err := pipeline.Extract(context.Background(), request)
	var invalid *inference.InvalidOutputError
	if !errors.As(err, &invalid) || invalid.Detail() != "an active entry can only be revised once per extraction" {
		t.Fatalf("error = %T %v", err, err)
	}
}

func TestExtractionInputJSONMatchesFrozenShape(t *testing.T) {
	t.Parallel()
	request, _ := NewCandidateRequest(
		[]source.Value{testMemoryContentSource(t)}, nil,
		[]EntryVersion{{EntryID: "entry-1", Kind: "fact", Text: "old"}},
	)
	input, _, err := extractionInput(request, NewContentEvidenceProjector(nil))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := input.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	want := `{"evidence":[{"evidence_id":"source:0","evidence_type":"source","content":{"source_type":"content","source_id":"turn-1","content":"Use uv for dependency management.","metadata":{"kind":"prompt"}}}],"current_entries":[{"entry_id":"entry-1","kind":"fact","text":"old"}]}`
	if string(encoded) != want {
		t.Fatalf("JSON = %s\nwant = %s", encoded, want)
	}
}

func testMemoryContentSource(t *testing.T) source.ContentSource {
	t.Helper()
	value, err := source.RestoreContentSource(
		"turn-1", source.Captured, nil, "Use uv for dependency management.", map[string]any{"kind": "prompt"},
	)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
