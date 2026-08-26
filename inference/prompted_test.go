package inference

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

type promptedQuestion struct {
	Value string `json:"value"`
}

type promptedCandidate struct {
	Text   string `json:"text"`
	Intent string `json:"intent"`
}

type promptedProposal struct {
	Candidates []promptedCandidate `json:"candidates"`
}

type recordedTextModel struct {
	mu        sync.Mutex
	requests  []TextRequest
	responses []TextResponse
	errors    []error
	complete  func(context.Context, TextRequest) (TextResponse, error)
}

func (m *recordedTextModel) Complete(ctx context.Context, request TextRequest) (TextResponse, error) {
	m.mu.Lock()
	m.requests = append(m.requests, request)
	if m.complete != nil {
		complete := m.complete
		m.mu.Unlock()
		return complete(ctx, request)
	}
	response := m.responses[0]
	m.responses = m.responses[1:]
	var err error
	if len(m.errors) > 0 {
		err = m.errors[0]
		m.errors = m.errors[1:]
	}
	m.mu.Unlock()
	return response, err
}

func (m *recordedTextModel) Requests() []TextRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.requests)
}

func TestPromptedGeneratorMatchesFrozenConversationAndUsage(t *testing.T) {
	firstUsage := Usage{InputTokens: int64Pointer(55), OutputTokens: int64Pointer(9)}
	secondUsage := Usage{InputTokens: int64Pointer(63), OutputTokens: int64Pointer(11)}
	first, err := NewTextResponse(`{"candidates":[{"text":"traveler prefers aisle seats"}]}`, firstUsage)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewTextResponse(`{"candidates":[{"text":"redacted","intent":"add"}]}`, secondUsage)
	if err != nil {
		t.Fatal(err)
	}
	model := &recordedTextModel{responses: []TextResponse{first, second}}
	codec := proposalCodec(t)
	settings, err := NewGenerationSettings(float64Pointer(0), nil)
	if err != nil {
		t.Fatal(err)
	}
	generator, err := NewPromptedGenerator[promptedQuestion, promptedProposal](
		model, "Propose candidates.", codec, nil, settings,
	)
	if err != nil {
		t.Fatal(err)
	}

	result, err := generator.Generate(context.Background(), promptedQuestion{Value: "bounded <evidence>"})
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Output.Candidates[0].Intent; got != "add" {
		t.Fatalf("intent = %q", got)
	}
	if result.Usage.Requests != 2 || *result.Usage.InputTokens != 118 || *result.Usage.OutputTokens != 20 {
		t.Fatalf("usage = %+v", result.Usage)
	}

	requests := model.Requests()
	if len(requests) != 2 {
		t.Fatalf("request count = %d", len(requests))
	}
	for index, request := range requests {
		temperature := request.Settings().Temperature()
		if temperature == nil || *temperature != 0 {
			t.Fatalf("request %d temperature = %v, want explicit 0", index, temperature)
		}
	}
	firstMessages := requests[0].Messages()
	if len(firstMessages) != 1 || firstMessages[0].Role() != RoleUser || firstMessages[0].Content() != `{"value":"bounded <evidence>"}` {
		t.Fatalf("first messages = %#v", firstMessages)
	}
	instructions := requests[0].Instructions()
	if len(instructions) != 2 || instructions[0] != "Propose candidates." {
		t.Fatalf("instructions = %#v", instructions)
	}
	wantSchemaInstruction := "\nAlways respond with a JSON object that's compatible with this schema:\n\n" +
		`{"$defs": {"Candidate": {"properties": {"text": {"type": "string"}, "intent": {"type": "string"}}, "required": ["text", "intent"], "title": "Candidate", "type": "object"}}, "properties": {"candidates": {"items": {"$ref": "#/$defs/Candidate"}, "type": "array"}}, "required": ["candidates"], "title": "Proposal", "type": "object"}` +
		"\n\nDon't include any text or Markdown fencing before or after.\n"
	if instructions[1] != wantSchemaInstruction {
		t.Fatalf("schema instruction mismatch:\n%s", instructions[1])
	}
	secondMessages := requests[1].Messages()
	if len(secondMessages) != 3 || secondMessages[1].Role() != RoleAssistant || secondMessages[2].Role() != RoleUser {
		t.Fatalf("retry messages = %#v", secondMessages)
	}
	wantFeedback := "1 validation error:\n```json\n[\n  {\n    \"type\": \"missing\",\n    \"loc\": [\n      \"candidates\",\n      0,\n      \"intent\"\n    ],\n    \"msg\": \"Field required\",\n    \"input\": {\n      \"text\": \"traveler prefers aisle seats\"\n    }\n  }\n]\n```\n\nFix the errors and try again."
	if secondMessages[2].Content() != wantFeedback {
		t.Fatalf("retry feedback mismatch:\n%s", secondMessages[2].Content())
	}
}

func TestPromptedGeneratorStripsMarkdownFence(t *testing.T) {
	response, _ := NewTextResponse("prefix```json\n{\"candidates\":[{\"text\":\"x\",\"intent\":\"add\"}]}\n```", Usage{})
	model := &recordedTextModel{responses: []TextResponse{response}}
	generator, err := NewPromptedGenerator[promptedQuestion, promptedProposal](
		model, "Propose candidates.", proposalCodec(t), nil, GenerationSettings{},
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := generator.Generate(context.Background(), promptedQuestion{Value: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Output.Candidates[0].Text != "x" {
		t.Fatalf("output = %#v", result.Output)
	}
}

func TestPromptedGeneratorExhaustsRequestBudgetWithoutReturningUsage(t *testing.T) {
	bad, _ := NewTextResponse(`{"candidates":[]}`, Usage{InputTokens: int64Pointer(1)})
	model := &recordedTextModel{responses: []TextResponse{bad, bad}}
	generator, err := NewPromptedGenerator[promptedQuestion, promptedProposal](
		model, "Propose candidates.", proposalCodec(t), nil, GenerationSettings{},
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := generator.Generate(context.Background(), promptedQuestion{Value: "x"})
	var invalid *InvalidOutputError
	if !errors.As(err, &invalid) {
		t.Fatalf("error = %v", err)
	}
	if result.Usage.Requests != 0 {
		t.Fatalf("failed result leaked usage: %+v", result.Usage)
	}
	if len(model.Requests()) != 2 {
		t.Fatalf("provider calls = %d", len(model.Requests()))
	}
}

func TestPromptedGeneratorPreservesStableProviderFailureAndCause(t *testing.T) {
	provider := errors.New("secret provider response")
	model := &recordedTextModel{complete: func(context.Context, TextRequest) (TextResponse, error) {
		return TextResponse{}, WrapUnavailableError("generate", provider)
	}}
	generator, err := NewPromptedGenerator[promptedQuestion, promptedProposal](
		model, "Propose candidates.", proposalCodec(t), nil, GenerationSettings{},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = generator.Generate(t.Context(), promptedQuestion{Value: "bounded evidence"})
	var unavailable *UnavailableError
	if !errors.As(err, &unavailable) || !errors.Is(err, provider) {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), provider.Error()) {
		t.Fatalf("error leaked provider detail: %q", err)
	}
	if len(model.Requests()) != 1 {
		t.Fatalf("provider calls = %d", len(model.Requests()))
	}
}

func TestPromptedGeneratorTotalWallClockTimeoutAndCallerCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		model := &recordedTextModel{complete: func(ctx context.Context, _ TextRequest) (TextResponse, error) {
			<-ctx.Done()
			return TextResponse{}, ctx.Err()
		}}
		limits, _ := NewLimits(7*time.Second, 2)
		generator, err := NewPromptedGenerator[promptedQuestion, promptedProposal](
			model, "Propose candidates.", proposalCodec(t), &limits, GenerationSettings{},
		)
		if err != nil {
			t.Fatal(err)
		}
		started := time.Now()
		_, err = generator.Generate(context.Background(), promptedQuestion{Value: "x"})
		var timeout *TimeoutError
		if !errors.As(err, &timeout) || timeout.Timeout() != 7*time.Second {
			t.Fatalf("error = %v", err)
		}
		if elapsed := time.Since(started); elapsed != 7*time.Second {
			t.Fatalf("elapsed = %s", elapsed)
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	model := &recordedTextModel{complete: func(ctx context.Context, _ TextRequest) (TextResponse, error) {
		return TextResponse{}, ctx.Err()
	}}
	generator, err := NewPromptedGenerator[promptedQuestion, promptedProposal](
		model, "Propose candidates.", proposalCodec(t), nil, GenerationSettings{},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = generator.Generate(ctx, promptedQuestion{Value: "x"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled error = %v", err)
	}
}

func TestValidationFeedbackOmitsTopLevelInput(t *testing.T) {
	issue, err := NewValidationIssue("missing", []any{"value"}, "Field required", json.RawMessage(`{"secret":"value"}`))
	if err != nil {
		t.Fatal(err)
	}
	validation, err := NewValidationError([]ValidationIssue{issue})
	if err != nil {
		t.Fatal(err)
	}
	feedback := validationFeedback(validation)
	if strings.Contains(feedback, "secret") || strings.Contains(feedback, `"input"`) {
		t.Fatalf("top-level input leaked: %s", feedback)
	}
}

func TestJSONCodecRejectsNonObjectAndTrailingSchema(t *testing.T) {
	for _, schema := range [][]byte{
		[]byte(`{"type":"array"}`),
		[]byte(`{"properties":{}}`),
		[]byte(`{"type":"object"} {}`),
	} {
		if _, err := NewJSONCodec[promptedQuestion, promptedProposal](schema, nil, nil); err == nil {
			t.Fatalf("schema accepted: %s", schema)
		}
	}
}

func proposalCodec(t *testing.T) *JSONCodec[promptedQuestion, promptedProposal] {
	t.Helper()
	schema := []byte(`{"$defs":{"Candidate":{"properties":{"text":{"type":"string"},"intent":{"type":"string"}},"required":["text","intent"],"title":"Candidate","type":"object"}},"properties":{"candidates":{"items":{"$ref":"#/$defs/Candidate"},"type":"array"}},"required":["candidates"],"title":"Proposal","type":"object"}`)
	codec, err := NewJSONCodec[promptedQuestion, promptedProposal](schema, nil, func(value []byte) (promptedProposal, error) {
		var result promptedProposal
		if err := json.Unmarshal(value, &result); err != nil {
			return result, err
		}
		if len(result.Candidates) == 0 {
			issue, _ := NewValidationIssue("too_short", []any{"candidates"}, "Array should have at least 1 item", value)
			validation, _ := NewValidationError([]ValidationIssue{issue})
			return result, validation
		}
		for index, candidate := range result.Candidates {
			if candidate.Intent == "" {
				input, _ := json.Marshal(map[string]string{"text": candidate.Text})
				issue, _ := NewValidationIssue("missing", []any{"candidates", index, "intent"}, "Field required", input)
				validation, _ := NewValidationError([]ValidationIssue{issue})
				return result, validation
			}
		}
		return result, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return codec
}

func int64Pointer(value int64) *int64       { return &value }
func float64Pointer(value float64) *float64 { return &value }
