package skill

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

func TestLLMGeneratorReturnsCompleteSkill(t *testing.T) {
	t.Parallel()
	content, err := NewContent("verify-config", "Verify configuration", "Run the exact fixture.", []string{"The fixture passes."})
	if err != nil {
		t.Fatal(err)
	}
	evidence, _ := artifact.NewGenerationEvidence("source:0", artifact.SourceEvidence, "observed", false)
	input, _ := artifact.NewGenerationInput([]artifact.GenerationEvidence{evidence}, nil)
	generator, _ := NewLLMGenerator(generationGeneratorFunc(func(context.Context, artifact.GenerationInput) (inference.GenerationResult[GenerationOutput], error) {
		return inference.GenerationResult[GenerationOutput]{Output: NewGenerationOutput(&content)}, nil
	}))
	got, err := generator.Generate(context.Background(), input)
	if err != nil || got == nil || got.Name() != "verify-config" {
		t.Fatalf("proposal=%#v err=%v", got, err)
	}
	validation := got.Validation()
	validation[0] = "mutated"
	if got.Validation()[0] != "The fixture passes." {
		t.Fatal("Skill proposal leaked mutable validation storage")
	}
}

func TestDecodeGenerationOutputRejectsIncompleteSkill(t *testing.T) {
	t.Parallel()
	if _, err := decodeGenerationOutput([]byte(`{"proposal":{"name":"x"}}`)); err == nil {
		t.Fatal("expected incomplete proposal to fail")
	}
}
