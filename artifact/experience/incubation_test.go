package experience

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ob-labs/powercontext-go/inference"
	"github.com/ob-labs/powercontext-go/source"
)

type incubationGeneratorFunc func(context.Context, IncubationInput) (inference.GenerationResult[IncubationOutput], error)

func (f incubationGeneratorFunc) Generate(ctx context.Context, input IncubationInput) (inference.GenerationResult[IncubationOutput], error) {
	return f(ctx, input)
}

func TestLLMCandidatePipelineMapsBoundedTaskOutcomes(t *testing.T) {
	t.Parallel()
	proposal := testExperienceContent(t)
	candidate, err := NewIncubationCandidate(proposal, []string{
		"source:content/task-1", "source:content/task-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	var recorded IncubationInput
	generator := incubationGeneratorFunc(func(_ context.Context, input IncubationInput) (inference.GenerationResult[IncubationOutput], error) {
		recorded = input
		return inference.GenerationResult[IncubationOutput]{Output: NewIncubationOutput([]IncubationCandidate{candidate})}, nil
	})
	pipeline, err := NewLLMCandidatePipeline(generator)
	if err != nil {
		t.Fatal(err)
	}
	ordinary := testContentSource(t, "ordinary", "prompt", "ignore")
	task := testContentSource(t, "task-1", TaskOutcomeSourceKind, strings.Repeat("界", MaxIncubationSourceChars+10))

	got, err := pipeline.Incubate(context.Background(), []source.Value{ordinary, task})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[0].Sources()) != 1 {
		t.Fatalf("candidates = %#v", got)
	}
	if got[0].Sources()[0].String() != "content:task-1" || got[0].Reason() != IncubationReason {
		t.Fatalf("candidate lineage = %#v, reason = %q", got[0].Sources(), got[0].Reason())
	}
	visible := recorded.Evidence()
	if len(visible) != 1 || visible[0].EvidenceID() != "source:content/task-1" {
		t.Fatalf("generator evidence = %#v", visible)
	}
	if count := len([]rune(visible[0].Content())); count != MaxIncubationSourceChars {
		t.Fatalf("visible characters = %d", count)
	}
}

func TestLLMCandidatePipelineSkipsIneligibleWindow(t *testing.T) {
	t.Parallel()
	called := false
	generator := incubationGeneratorFunc(func(context.Context, IncubationInput) (inference.GenerationResult[IncubationOutput], error) {
		called = true
		return inference.GenerationResult[IncubationOutput]{}, nil
	})
	pipeline, _ := NewLLMCandidatePipeline(generator)
	got, err := pipeline.Incubate(context.Background(), []source.Value{
		testContentSource(t, "ordinary", "prompt", "ignore"),
	})
	if err != nil || len(got) != 0 || called {
		t.Fatalf("got=%#v err=%v called=%v", got, err, called)
	}
}

func TestLLMCandidatePipelineRejectsEvidenceOutsideWindow(t *testing.T) {
	t.Parallel()
	proposal := testExperienceContent(t)
	candidate, _ := NewIncubationCandidate(proposal, []string{"source:content/not-in-window"})
	generator := incubationGeneratorFunc(func(context.Context, IncubationInput) (inference.GenerationResult[IncubationOutput], error) {
		return inference.GenerationResult[IncubationOutput]{Output: NewIncubationOutput([]IncubationCandidate{candidate})}, nil
	})
	pipeline, _ := NewLLMCandidatePipeline(generator)
	_, err := pipeline.Incubate(context.Background(), []source.Value{
		testContentSource(t, "task-1", TaskOutcomeSourceKind, "passed"),
	})
	var invalid *inference.InvalidOutputError
	if !errors.As(err, &invalid) || invalid.Operation() != "experience-incubate" {
		t.Fatalf("error = %T %v", err, err)
	}
}

func TestIncubationInputJSONMatchesFrozenShape(t *testing.T) {
	t.Parallel()
	input := IncubationInput{evidence: []IncubationEvidence{{evidenceID: "source:content/task", content: "<ok>"}}}
	encoded, err := input.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(encoded), `{"evidence":[{"evidence_id":"source:content/task","content":"<ok>"}]}`; got != want {
		t.Fatalf("JSON = %s, want %s", got, want)
	}
}

func testContentSource(t *testing.T, id, kind, content string) source.ContentSource {
	t.Helper()
	value, err := source.RestoreContentSource(id, source.Captured, nil, content, map[string]any{"kind": kind})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func testExperienceContent(t *testing.T) Content {
	t.Helper()
	value, err := NewContent(
		"A strict fixture failed.",
		"Set strict mode and ran the fixture.",
		"The independently observed check passed.",
		"Run the strict fixture after configuration changes.",
	)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
