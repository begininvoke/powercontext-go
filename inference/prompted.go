package inference

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"time"
)

const defaultPromptedOutputTemplate = "\nAlways respond with a JSON object that's compatible with this schema:\n\n{schema}\n\nDon't include any text or Markdown fencing before or after.\n"

var markdownFencePattern = regexp.MustCompile("(?s)```(?:[[:word:]]+)?\\r?\\n(\\{.*?\\})[[:space:]]*(?:\\r?\\n?```|$)")

type Limits struct {
	timeout     time.Duration
	maxRequests int
}

func NewLimits(timeout time.Duration, maxRequests int) (Limits, error) {
	if timeout <= 0 {
		return Limits{}, fmt.Errorf("inference timeout must be positive")
	}
	if maxRequests < 1 {
		return Limits{}, fmt.Errorf("inference max requests must be positive")
	}
	return Limits{timeout: timeout, maxRequests: maxRequests}, nil
}

func DefaultLimits() Limits {
	limits, _ := NewLimits(30*time.Second, 2)
	return limits
}

func (l Limits) Timeout() time.Duration { return l.timeout }
func (l Limits) MaxRequests() int       { return l.maxRequests }

// StructuredCodec keeps wire JSON and validation rules separate from domain
// values. Implementations must never return partially validated output.
type StructuredCodec[I, O any] interface {
	EncodeInput(I) ([]byte, error)
	OutputSchema() []byte
	DecodeOutput([]byte) (O, error)
}

type JSONCodec[I, O any] struct {
	schema []byte
	encode func(I) ([]byte, error)
	decode func([]byte) (O, error)
}

func NewJSONCodec[I, O any](
	schema []byte,
	encode func(I) ([]byte, error),
	decode func([]byte) (O, error),
) (*JSONCodec[I, O], error) {
	prepared, err := prepareObjectSchema(schema)
	if err != nil {
		return nil, NewConfigurationError("schema", err.Error())
	}
	if encode == nil {
		encode = encodeJSON[I]
	}
	if decode == nil {
		decode = decodeJSON[O]
	}
	return &JSONCodec[I, O]{schema: prepared, encode: encode, decode: decode}, nil
}

func (c *JSONCodec[I, O]) EncodeInput(value I) ([]byte, error) {
	encoded, err := c.encode(value)
	return slices.Clone(encoded), err
}

func (c *JSONCodec[I, O]) OutputSchema() []byte { return slices.Clone(c.schema) }

func (c *JSONCodec[I, O]) DecodeOutput(value []byte) (O, error) {
	return c.decode(slices.Clone(value))
}

type ValidationIssue struct {
	kind     string
	location []any
	message  string
	input    json.RawMessage
}

func NewValidationIssue(kind string, location []any, message string, input json.RawMessage) (ValidationIssue, error) {
	if kind == "" || message == "" {
		return ValidationIssue{}, fmt.Errorf("validation issue type and message must not be empty")
	}
	preparedLocation := make([]any, len(location))
	for index, component := range location {
		switch component := component.(type) {
		case string:
			preparedLocation[index] = component
		case int:
			preparedLocation[index] = component
		case int64:
			preparedLocation[index] = component
		default:
			return ValidationIssue{}, fmt.Errorf("validation issue location component %d has unsupported type %T", index, component)
		}
	}
	if len(input) != 0 && !json.Valid(input) {
		return ValidationIssue{}, fmt.Errorf("validation issue input must be valid JSON")
	}
	return ValidationIssue{
		kind: kind, location: preparedLocation, message: message, input: slices.Clone(input),
	}, nil
}

type ValidationError struct{ issues []ValidationIssue }

func NewValidationError(issues []ValidationIssue) (*ValidationError, error) {
	if len(issues) == 0 {
		return nil, fmt.Errorf("validation error must contain at least one issue")
	}
	return &ValidationError{issues: cloneValidationIssues(issues)}, nil
}

func (e *ValidationError) Error() string { return "structured output validation failed" }

func (e *ValidationError) Issues() []ValidationIssue { return cloneValidationIssues(e.issues) }

type PromptedGenerator[I, O any] struct {
	model        TextModel
	instructions string
	codec        StructuredCodec[I, O]
	schema       []byte
	limits       Limits
	settings     GenerationSettings
}

func NewPromptedGenerator[I, O any](
	model TextModel,
	instructions string,
	codec StructuredCodec[I, O],
	limits *Limits,
	settings GenerationSettings,
) (*PromptedGenerator[I, O], error) {
	if model == nil {
		return nil, NewConfigurationError("model", "")
	}
	if strings.TrimSpace(instructions) == "" {
		return nil, NewConfigurationError("instructions", "")
	}
	if codec == nil {
		return nil, NewConfigurationError("schema", "codec is nil")
	}
	resolvedLimits := DefaultLimits()
	if limits != nil {
		if limits.timeout <= 0 || limits.maxRequests < 1 {
			return nil, NewConfigurationError("request-rejected", "invalid limits")
		}
		resolvedLimits = *limits
	}
	schema, err := prepareObjectSchema(codec.OutputSchema())
	if err != nil {
		return nil, NewConfigurationError("schema", err.Error())
	}
	// The constructor probes and freezes the schema so mutable codec state cannot
	// silently change the provider contract between requests.
	return &PromptedGenerator[I, O]{
		model: model, instructions: instructions, codec: codec,
		schema: schema, limits: resolvedLimits, settings: settings,
	}, nil
}

func (g *PromptedGenerator[I, O]) Generate(ctx context.Context, value I) (GenerationResult[O], error) {
	var zero GenerationResult[O]
	prompt, err := g.codec.EncodeInput(value)
	if err != nil || !json.Valid(prompt) {
		return zero, NewConfigurationError("serialize", "")
	}

	instructions := []string{
		g.instructions,
		strings.Replace(defaultPromptedOutputTemplate, "{schema}", pythonJSONSpacing(g.schema), 1),
	}
	first, _ := NewMessage(RoleUser, string(prompt))
	messages := []Message{first}
	usage := Usage{}

	callCtx, cancel := context.WithTimeout(ctx, g.limits.timeout)
	defer cancel()

	for requestNumber := 1; requestNumber <= g.limits.maxRequests; requestNumber++ {
		response, requestErr := g.model.Complete(
			callCtx,
			newTextRequest(instructions, messages, g.settings),
		)
		if requestErr != nil {
			return zero, mapInferenceCallError(ctx, callCtx, requestErr, "generate", g.limits.timeout)
		}
		usage = addRequestUsage(usage, response.Usage())

		output, decodeErr := g.codec.DecodeOutput([]byte(stripMarkdownFences(response.Content())))
		if decodeErr == nil {
			return GenerationResult[O]{Output: output, Usage: usage}, nil
		}
		if requestNumber == g.limits.maxRequests {
			return zero, NewInvalidOutputError("generate", "provider did not return a valid result")
		}

		assistant, _ := NewMessage(RoleAssistant, response.Content())
		assistant.identity = response.ResponseIdentity()
		retry, _ := NewMessage(RoleUser, validationFeedback(decodeErr))
		messages = append(messages, assistant, retry)
	}
	panic("unreachable")
}

func encodeJSON[T any](value T) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'}), nil
}

func decodeJSON[T any](value []byte) (T, error) {
	var result T
	decoder := json.NewDecoder(bytes.NewReader(value))
	if err := decoder.Decode(&result); err != nil {
		return result, err
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return result, err
	}
	return result, nil
}

func prepareObjectSchema(schema []byte) ([]byte, error) {
	if len(schema) == 0 {
		return nil, fmt.Errorf("output schema must not be empty")
	}
	decoder := json.NewDecoder(bytes.NewReader(schema))
	var object map[string]json.RawMessage
	if err := decoder.Decode(&object); err != nil {
		return nil, err
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, err
	}
	encodedType, ok := object["type"]
	if !ok {
		return nil, fmt.Errorf("output schema root must have type object")
	}
	var schemaType string
	if json.Unmarshal(encodedType, &schemaType) != nil || schemaType != "object" {
		return nil, fmt.Errorf("output schema root must have type object")
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, schema); err != nil {
		return nil, err
	}
	return compact.Bytes(), nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return fmt.Errorf("JSON value has trailing data")
	}
	return err
}

func pythonJSONSpacing(compact []byte) string {
	var result strings.Builder
	result.Grow(len(compact) + len(compact)/8)
	inString := false
	escaped := false
	for _, character := range compact {
		result.WriteByte(character)
		if inString {
			if escaped {
				escaped = false
			} else if character == '\\' {
				escaped = true
			} else if character == '"' {
				inString = false
			}
			continue
		}
		if character == '"' {
			inString = true
		} else if character == ',' || character == ':' {
			result.WriteByte(' ')
		}
	}
	return result.String()
}

func stripMarkdownFences(value string) string {
	if strings.HasPrefix(value, "{") {
		return value
	}
	match := markdownFencePattern.FindStringSubmatch(value)
	if len(match) == 2 {
		return match[1]
	}
	return value
}

func validationFeedback(err error) string {
	var validation *ValidationError
	if !errors.As(err, &validation) {
		return "Validation feedback:\nprovider did not return a valid result\n\nFix the errors and try again."
	}
	type issueWire struct {
		Type  string          `json:"type"`
		Loc   []any           `json:"loc"`
		Msg   string          `json:"msg"`
		Input json.RawMessage `json:"input,omitempty"`
	}
	wires := make([]issueWire, len(validation.issues))
	for index, issue := range validation.issues {
		wires[index] = issueWire{Type: issue.kind, Loc: slices.Clone(issue.location), Msg: issue.message}
		if len(issue.location) > 1 {
			wires[index].Input = slices.Clone(issue.input)
		}
	}
	encoded, marshalErr := json.MarshalIndent(wires, "", "  ")
	if marshalErr != nil {
		return "Validation feedback:\nprovider did not return a valid result\n\nFix the errors and try again."
	}
	noun := "error"
	if len(wires) != 1 {
		noun = "errors"
	}
	return fmt.Sprintf("%d validation %s:\n```json\n%s\n```\n\nFix the errors and try again.", len(wires), noun, encoded)
}

func addRequestUsage(total, request Usage) Usage {
	total.Requests++
	total.InputTokens = addOptionalCount(total.InputTokens, request.InputTokens, total.Requests == 1)
	total.OutputTokens = addOptionalCount(total.OutputTokens, request.OutputTokens, total.Requests == 1)
	return total
}

func addOptionalCount(total, value *int64, first bool) *int64 {
	if value == nil || (!first && total == nil) {
		return nil
	}
	if first {
		return cloneInt64(value)
	}
	sum := *total + *value
	return &sum
}

func cloneValidationIssues(values []ValidationIssue) []ValidationIssue {
	result := make([]ValidationIssue, len(values))
	for index, value := range values {
		value.location = slices.Clone(value.location)
		value.input = slices.Clone(value.input)
		result[index] = value
	}
	return result
}
