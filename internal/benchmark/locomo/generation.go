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

package locomo

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/ob-labs/powercontext-go/inference"
	benchmarkprompts "github.com/ob-labs/powercontext-go/internal/benchmark/locomo/prompts"
)

// RetrievedMemory is the exact bounded context exposed to the answer model.
// The gold answer and evidence annotations deliberately cannot be represented.
type RetrievedMemory struct {
	Rank          int      `json:"rank"`
	RetrievalRank int      `json:"retrieval_rank"`
	Text          string   `json:"text"`
	Score         float64  `json:"score"`
	MatchedBy     []string `json:"matched_by"`
	SourceIDs     []string `json:"source_ids"`
	SourceDates   []string `json:"source_dates"`
}

type AnswerSourceSession struct {
	SourceID string `json:"source_id"`
	DateTime string `json:"date_time"`
	Content  string `json:"content"`
}

type AnswerInput struct {
	SpeakerA       string                `json:"speaker_a"`
	SpeakerB       string                `json:"speaker_b"`
	Question       string                `json:"question"`
	Memories       []RetrievedMemory     `json:"memories"`
	SourceSessions []AnswerSourceSession `json:"source_sessions"`
}

type AnswerOutput struct {
	Answer string `json:"answer"`
}

type JudgeInput struct {
	Question        string `json:"question"`
	GoldAnswer      string `json:"gold_answer"`
	GeneratedAnswer string `json:"generated_answer"`
}

type JudgeOutput struct {
	Label string `json:"label"`
}

var answerOutputSchema = []byte(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["answer"],
  "properties": {"answer": {"type": "string", "minLength": 1}}
}`)

var judgeOutputSchema = []byte(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["label"],
  "properties": {"label": {"type": "string", "enum": ["CORRECT", "WRONG"]}}
}`)

func NewAnswerGenerator(
	model inference.TextModel,
	limits *inference.Limits,
	sourceContent, inferenceAware bool,
) (*inference.PromptedGenerator[AnswerInput, AnswerOutput], error) {
	instructions, _, err := benchmarkprompts.AnswerInstructions(sourceContent, inferenceAware)
	if err != nil {
		return nil, err
	}
	codec, err := inference.NewJSONCodec[AnswerInput, AnswerOutput](
		answerOutputSchema, nil, decodeAnswerOutput,
	)
	if err != nil {
		return nil, err
	}
	settings, err := benchmarkSettings()
	if err != nil {
		return nil, err
	}
	return inference.NewPromptedGenerator(model, instructions, codec, limits, settings)
}

func NewJudgeGenerator(
	model inference.TextModel,
	limits *inference.Limits,
	profile benchmarkprompts.JudgeProfile,
) (*inference.PromptedGenerator[JudgeInput, JudgeOutput], error) {
	instructions, _, err := benchmarkprompts.JudgeInstructions(profile)
	if err != nil {
		return nil, err
	}
	codec, err := inference.NewJSONCodec[JudgeInput, JudgeOutput](
		judgeOutputSchema, nil, decodeJudgeOutput,
	)
	if err != nil {
		return nil, err
	}
	settings, err := benchmarkSettings()
	if err != nil {
		return nil, err
	}
	return inference.NewPromptedGenerator(model, instructions, codec, limits, settings)
}

func benchmarkSettings() (inference.GenerationSettings, error) {
	temperature := BenchmarkTemperature
	return inference.NewGenerationSettings(&temperature, nil)
}

func decodeAnswerOutput(encoded []byte) (AnswerOutput, error) {
	var value AnswerOutput
	if err := decodeStrictObject(encoded, &value); err != nil {
		return AnswerOutput{}, err
	}
	value.Answer = strings.TrimSpace(value.Answer)
	if value.Answer == "" {
		return AnswerOutput{}, fmt.Errorf("answer must not be empty")
	}
	return value, nil
}

func decodeJudgeOutput(encoded []byte) (JudgeOutput, error) {
	var value JudgeOutput
	if err := decodeStrictObject(encoded, &value); err != nil {
		return JudgeOutput{}, err
	}
	if value.Label != "CORRECT" && value.Label != "WRONG" {
		return JudgeOutput{}, fmt.Errorf("judge label must be CORRECT or WRONG")
	}
	return value, nil
}

func decodeStrictObject(encoded []byte, output any) error {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("structured output has trailing data")
		}
		return err
	}
	return nil
}
