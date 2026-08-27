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

package skill

import (
	"context"
	"testing"

	"github.com/ob-labs/powercontext-go/artifact"
	"github.com/ob-labs/powercontext-go/inference"
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
