package modelprovider

import (
	"context"
	"net/http"
	"strings"

	"github.com/ob-labs/powercontext-go/inference"
)

type ChatHTTPConfig struct {
	APIKey             string
	BaseURL            string
	EndpointPath       string
	HTTPClient         *http.Client
	Headers            http.Header
	SupportsJSONObject bool
	SendCandidateCount bool
}

type ChatHTTPTextModel struct {
	route              Route
	transport          providerHTTPClient
	endpointPath       string
	supportsJSONObject bool
	sendCandidateCount bool
}

func NewChatHTTPTextModel(route Route, config ChatHTTPConfig) (*ChatHTTPTextModel, error) {
	switch route.protocol {
	case ProtocolGroq, ProtocolXAI, ProtocolMistral, ProtocolHuggingFace:
	default:
		return nil, inference.NewConfigurationError("model", "route is not a supported HTTP chat protocol")
	}
	if strings.TrimSpace(config.APIKey) == "" {
		return nil, inference.NewConfigurationError("model", "provider API key is required")
	}
	headers := config.Headers.Clone()
	if headers == nil {
		headers = make(http.Header)
	}
	headers.Set("Authorization", "Bearer "+config.APIKey)
	transport, err := newProviderHTTPClient(config.HTTPClient, config.BaseURL, headers)
	if err != nil {
		return nil, err
	}
	return &ChatHTTPTextModel{
		route: route, transport: transport, endpointPath: config.EndpointPath,
		supportsJSONObject: config.SupportsJSONObject, sendCandidateCount: config.SendCandidateCount,
	}, nil
}

type chatHTTPMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatHTTPResponseFormat struct {
	Type string `json:"type"`
}

type chatHTTPRequest struct {
	Model          string                  `json:"model"`
	Messages       []chatHTTPMessage       `json:"messages"`
	Stream         bool                    `json:"stream"`
	N              *int                    `json:"n,omitempty"`
	Temperature    *float64                `json:"temperature,omitempty"`
	MaxTokens      *int64                  `json:"max_tokens,omitempty"`
	ResponseFormat *chatHTTPResponseFormat `json:"response_format,omitempty"`
}

type chatHTTPResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     *int64 `json:"prompt_tokens"`
		CompletionTokens *int64 `json:"completion_tokens"`
		InputTokens      *int64 `json:"input_tokens"`
		OutputTokens     *int64 `json:"output_tokens"`
	} `json:"usage"`
}

func (m *ChatHTTPTextModel) Complete(ctx context.Context, request inference.TextRequest) (inference.TextResponse, error) {
	messages := make([]chatHTTPMessage, 0, len(request.Instructions())+len(request.Messages()))
	for _, instruction := range request.Instructions() {
		messages = append(messages, chatHTTPMessage{Role: "system", Content: instruction})
	}
	for _, message := range request.Messages() {
		role := "user"
		if message.Role() == inference.RoleAssistant {
			role = "assistant"
		}
		messages = append(messages, chatHTTPMessage{Role: role, Content: message.Content()})
	}
	payload := chatHTTPRequest{
		Model: m.route.model, Messages: messages, Stream: false,
		Temperature: request.Settings().Temperature(), MaxTokens: request.Settings().MaxTokens(),
	}
	if request.StructuredOutput() && m.supportsJSONObject {
		payload.ResponseFormat = &chatHTTPResponseFormat{Type: "json_object"}
	}
	if m.sendCandidateCount {
		one := 1
		payload.N = &one
	}
	var response chatHTTPResponse
	if err := m.transport.postJSON(ctx, m.endpointPath, payload, &response, "generate"); err != nil {
		return inference.TextResponse{}, err
	}
	if len(response.Choices) == 0 {
		return inference.TextResponse{}, inference.NewInvalidOutputError("generate", "provider returned no choices")
	}
	inputTokens := response.Usage.PromptTokens
	if inputTokens == nil {
		inputTokens = response.Usage.InputTokens
	}
	outputTokens := response.Usage.CompletionTokens
	if outputTokens == nil {
		outputTokens = response.Usage.OutputTokens
	}
	return inference.NewTextResponse(response.Choices[0].Message.Content, inference.Usage{
		InputTokens: cloneInt64Pointer(inputTokens), OutputTokens: cloneInt64Pointer(outputTokens),
	})
}

func cloneInt64Pointer(value *int64) *int64 {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

var _ inference.TextModel = (*ChatHTTPTextModel)(nil)
