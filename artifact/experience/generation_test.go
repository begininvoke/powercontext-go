package experience

import (
	"context"
	"testing"

	"github.com/thunguo/powercontext-go/artifact"
	"github.com/thunguo/powercontext-go/inference"
)

type generationGeneratorFunc func(context.Context, artifact.GenerationInput) (inference.GenerationResult[GenerationOutput], error)

func (f generationGeneratorFunc) Generate(ctx context.Context, input artifact.GenerationInput) (inference.GenerationResult[GenerationOutput], error) {
	return f(ctx, input)
}

func TestLLMGeneratorReturnsTypedProposalAndNoOp(t *testing.T) {
	t.Parallel()
	content := testExperienceContent(t)
	evidence, err := artifact.NewGenerationEvidence("source:0", artifact.SourceEvidence, "observed", false)
	if err != nil {
		t.Fatal(err)
	}
	input, err := artifact.NewGenerationInput([]artifact.GenerationEvidence{evidence}, nil)
	if err != nil {
		t.Fatal(err)
	}
	generator, _ := NewLLMGenerator(generationGeneratorFunc(func(context.Context, artifact.GenerationInput) (inference.GenerationResult[GenerationOutput], error) {
		return inference.GenerationResult[GenerationOutput]{Output: NewGenerationOutput(&content)}, nil
	}))
	got, err := generator.Generate(context.Background(), input)
	if err != nil || got == nil || got.Lesson() != content.Lesson() {
		t.Fatalf("proposal=%#v err=%v", got, err)
	}

	noOp, _ := NewLLMGenerator(generationGeneratorFunc(func(context.Context, artifact.GenerationInput) (inference.GenerationResult[GenerationOutput], error) {
		return inference.GenerationResult[GenerationOutput]{Output: NewGenerationOutput(nil)}, nil
	}))
	got, err = noOp.Generate(context.Background(), input)
	if err != nil || got != nil {
		t.Fatalf("proposal=%#v err=%v", got, err)
	}
}

func TestGenerationInputJSONMatchesFrozenShape(t *testing.T) {
	t.Parallel()
	evidence, _ := artifact.NewGenerationEvidence("source:0", artifact.SourceEvidence, "<observed>", true)
	target := "artifact:0"
	input, _ := artifact.NewGenerationInput([]artifact.GenerationEvidence{evidence}, &target)
	encoded, err := encodeGenerationInput(input)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"evidence":[{"evidence_id":"source:0","kind":"source","content":"<observed>","truncated":true}],"target_evidence_id":"artifact:0"}`
	if string(encoded) != want {
		t.Fatalf("JSON = %s, want %s", encoded, want)
	}
}
