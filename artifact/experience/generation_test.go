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

package experience

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
