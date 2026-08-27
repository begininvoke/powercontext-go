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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/ob-labs/powercontext-go/artifact"
	"github.com/ob-labs/powercontext-go/artifact/experience/prompts"
	"github.com/ob-labs/powercontext-go/inference"
)

// GenerationOutput is the schema-bound model result before caller-selected
// lineage is applied. A nil proposal is an explicit no-op.
type GenerationOutput struct{ proposal *Content }

func NewGenerationOutput(proposal *Content) GenerationOutput {
	return GenerationOutput{proposal: cloneContentPointer(proposal)}
}

func (o GenerationOutput) Proposal() *Content { return cloneContentPointer(o.proposal) }

type Generator interface {
	Generate(context.Context, artifact.GenerationInput) (*Content, error)
}

type LLMGenerator struct {
	generator inference.StructuredGenerator[artifact.GenerationInput, GenerationOutput]
}

func NewLLMGenerator(
	generator inference.StructuredGenerator[artifact.GenerationInput, GenerationOutput],
) (*LLMGenerator, error) {
	if generator == nil {
		return nil, fmt.Errorf("Experience generator must not be nil")
	}
	return &LLMGenerator{generator: generator}, nil
}

func NewPromptedGenerator(
	model inference.TextModel,
	limits *inference.Limits,
	settings inference.GenerationSettings,
) (*inference.PromptedGenerator[artifact.GenerationInput, GenerationOutput], error) {
	codec, err := inference.NewJSONCodec[artifact.GenerationInput, GenerationOutput](
		prompts.GenerationSchema(), encodeGenerationInput, decodeGenerationOutput,
	)
	if err != nil {
		return nil, err
	}
	return inference.NewPromptedGenerator(model, prompts.Generation(), codec, limits, settings)
}

func (g *LLMGenerator) Generate(ctx context.Context, value artifact.GenerationInput) (*Content, error) {
	result, err := g.generator.Generate(ctx, value)
	if err != nil {
		return nil, err
	}
	return result.Output.Proposal(), nil
}

type generationInputDTO struct {
	Evidence         []generationEvidenceDTO `json:"evidence"`
	TargetEvidenceID *string                 `json:"target_evidence_id"`
}

type generationEvidenceDTO struct {
	EvidenceID string                `json:"evidence_id"`
	Kind       artifact.EvidenceKind `json:"kind"`
	Content    string                `json:"content"`
	Truncated  bool                  `json:"truncated"`
}

func encodeGenerationInput(value artifact.GenerationInput) ([]byte, error) {
	evidence := value.Evidence()
	projected := make([]generationEvidenceDTO, len(evidence))
	for index, item := range evidence {
		projected[index] = generationEvidenceDTO{
			EvidenceID: item.EvidenceID,
			Kind:       item.Kind,
			Content:    item.Content,
			Truncated:  item.Truncated,
		}
	}
	return marshalJSON(generationInputDTO{
		Evidence: projected, TargetEvidenceID: value.TargetEvidenceID(),
	})
}

func decodeGenerationOutput(encoded []byte) (GenerationOutput, error) {
	fields, err := decodeObject(encoded)
	if err != nil {
		return GenerationOutput{}, err
	}
	raw, exists := fields["proposal"]
	if !exists || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return GenerationOutput{}, nil
	}
	content, err := decodeContent(raw)
	if err != nil {
		return GenerationOutput{}, err
	}
	return NewGenerationOutput(&content), nil
}

func decodeContent(encoded []byte) (Content, error) {
	fields, err := decodeObject(encoded)
	if err != nil {
		return Content{}, err
	}
	values := make([]string, 4)
	for index, name := range []string{"situation", "action", "outcome", "lesson"} {
		raw, exists := fields[name]
		if !exists || json.Unmarshal(raw, &values[index]) != nil {
			return Content{}, fmt.Errorf("Experience proposal field %s is missing or invalid", name)
		}
	}
	return NewContent(values[0], values[1], values[2], values[3])
}

func decodeObject(encoded []byte) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	var fields map[string]json.RawMessage
	if err := decoder.Decode(&fields); err != nil || fields == nil {
		return nil, fmt.Errorf("expected a JSON object")
	}
	if err := requireJSONEOF(decoder); err != nil {
		return nil, err
	}
	return fields, nil
}

func marshalJSON(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'}), nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("JSON value has trailing data")
		}
		return err
	}
	return nil
}

func cloneContentPointer(value *Content) *Content {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
