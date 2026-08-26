package inference

import (
	"context"
	"fmt"
	"math"
	"slices"
)

type MessageRole uint8

const (
	RoleUser MessageRole = iota + 1
	RoleAssistant
)

type Message struct {
	role     MessageRole
	content  string
	identity *ResponseIdentity
}

func NewMessage(role MessageRole, content string) (Message, error) {
	if role != RoleUser && role != RoleAssistant {
		return Message{}, fmt.Errorf("invalid inference message role %d", role)
	}
	return Message{role: role, content: content}, nil
}

func (m Message) Role() MessageRole                   { return m.role }
func (m Message) Content() string                     { return m.content }
func (m Message) ResponseIdentity() *ResponseIdentity { return cloneResponseIdentity(m.identity) }

// ResponseIdentity carries the minimum provider-generated identity needed to
// replay an assistant turn without retaining an entire provider response.
type ResponseIdentity struct {
	protocol string
	itemID   string
}

func NewResponseIdentity(protocol, itemID string) (ResponseIdentity, error) {
	if protocol == "" || itemID == "" {
		return ResponseIdentity{}, fmt.Errorf("response identity protocol and item ID must not be empty")
	}
	return ResponseIdentity{protocol: protocol, itemID: itemID}, nil
}

func (i ResponseIdentity) Protocol() string { return i.protocol }
func (i ResponseIdentity) ItemID() string   { return i.itemID }

type GenerationSettings struct {
	temperature *float64
	maxTokens   *int64
}

func NewGenerationSettings(temperature *float64, maxTokens *int64) (GenerationSettings, error) {
	if temperature != nil && (math.IsNaN(*temperature) || math.IsInf(*temperature, 0)) {
		return GenerationSettings{}, fmt.Errorf("generation temperature must be finite")
	}
	if maxTokens != nil && *maxTokens < 1 {
		return GenerationSettings{}, fmt.Errorf("generation max tokens must be positive")
	}
	return GenerationSettings{
		temperature: cloneFloat64(temperature),
		maxTokens:   cloneInt64(maxTokens),
	}, nil
}

func (s GenerationSettings) Temperature() *float64 { return cloneFloat64(s.temperature) }
func (s GenerationSettings) MaxTokens() *int64     { return cloneInt64(s.maxTokens) }

type TextRequest struct {
	instructions []string
	messages     []Message
	settings     GenerationSettings
	structured   bool
}

func newTextRequest(instructions []string, messages []Message, settings GenerationSettings) TextRequest {
	return newTextRequestWithMode(instructions, messages, settings, true)
}

func newProbeTextRequest(messages []Message, settings GenerationSettings) TextRequest {
	return newTextRequestWithMode(nil, messages, settings, false)
}

func newTextRequestWithMode(
	instructions []string,
	messages []Message,
	settings GenerationSettings,
	structured bool,
) TextRequest {
	return TextRequest{
		instructions: slices.Clone(instructions),
		messages:     slices.Clone(messages),
		settings:     settings,
		structured:   structured,
	}
}

func (r TextRequest) Instructions() []string       { return slices.Clone(r.instructions) }
func (r TextRequest) Messages() []Message          { return slices.Clone(r.messages) }
func (r TextRequest) Settings() GenerationSettings { return r.settings }
func (r TextRequest) StructuredOutput() bool       { return r.structured }

type TextResponse struct {
	content  string
	usage    Usage
	identity *ResponseIdentity
}

func NewTextResponse(content string, usage Usage) (TextResponse, error) {
	if err := validateUsage(usage); err != nil {
		return TextResponse{}, err
	}
	return TextResponse{content: content, usage: cloneUsage(usage)}, nil
}

func (r TextResponse) Content() string                     { return r.content }
func (r TextResponse) Usage() Usage                        { return cloneUsage(r.usage) }
func (r TextResponse) ResponseIdentity() *ResponseIdentity { return cloneResponseIdentity(r.identity) }

func (r TextResponse) WithResponseIdentity(identity ResponseIdentity) TextResponse {
	r.identity = cloneResponseIdentity(&identity)
	return r
}

// TextModel is the deliberately narrow provider contract consumed by the
// PromptedOutput-compatible structured generator.
type TextModel interface {
	Complete(context.Context, TextRequest) (TextResponse, error)
}

func validateUsage(value Usage) error {
	if value.Requests < 0 {
		return fmt.Errorf("inference usage requests must not be negative")
	}
	if value.InputTokens != nil && *value.InputTokens < 0 {
		return fmt.Errorf("inference input tokens must not be negative")
	}
	if value.OutputTokens != nil && *value.OutputTokens < 0 {
		return fmt.Errorf("inference output tokens must not be negative")
	}
	return nil
}

func cloneUsage(value Usage) Usage {
	value.InputTokens = cloneInt64(value.InputTokens)
	value.OutputTokens = cloneInt64(value.OutputTokens)
	return value
}

func cloneFloat64(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneResponseIdentity(value *ResponseIdentity) *ResponseIdentity {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
