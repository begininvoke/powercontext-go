package modelprovider

import (
	"testing"

	"github.com/ob-labs/powercontext-go/inference"
)

func TestOpenAIBehaviorMatchesFrozenProviderProfiles(t *testing.T) {
	tests := []struct {
		model        string
		jsonObject   bool
		legacyTokens bool
		systemRole   OpenAISystemRole
		encrypted    bool
		dropSampling bool
	}{
		{model: "openai:gpt-4o", jsonObject: true, systemRole: OpenAISystem},
		{model: "openai:gpt-5", jsonObject: true, systemRole: OpenAISystem, encrypted: true, dropSampling: true},
		{model: "openai-chat:o1-mini", jsonObject: true, systemRole: OpenAIUser, encrypted: true, dropSampling: true},
		{model: "openai:gpt-5.2", jsonObject: true, systemRole: OpenAISystem, encrypted: true},
		{model: "alibaba:qwen-max", systemRole: OpenAISystem},
		{model: "alibaba:qwen3.5-plus", jsonObject: true, systemRole: OpenAISystem},
		{model: "cerebras:llama-3.3-70b", systemRole: OpenAISystem},
		{model: "cerebras:gpt-oss-120b", jsonObject: true, systemRole: OpenAISystem},
		{model: "crusoe:meta-llama/Llama-3.3", jsonObject: true, systemRole: OpenAISystem},
		{model: "deepseek:deepseek-reasoner", jsonObject: true, systemRole: OpenAISystem},
		{model: "fireworks:accounts/test/models/test", systemRole: OpenAISystem},
		{model: "openrouter:openai/gpt-5", jsonObject: true, legacyTokens: true, systemRole: OpenAISystem, encrypted: true, dropSampling: true},
		{model: "openrouter:anthropic/claude-sonnet-4", legacyTokens: true, systemRole: OpenAISystem},
		{model: "snowflake:openai-gpt-4.1", jsonObject: true, systemRole: OpenAISystem},
		{model: "snowflake:claude-sonnet-4", systemRole: OpenAISystem},
		{model: "litellm:unknown", jsonObject: true, systemRole: OpenAISystem},
		{model: "litellm:anthropic/claude-sonnet-4", systemRole: OpenAISystem},
		{model: "vercel:openai/gpt-5", jsonObject: true, systemRole: OpenAISystem, encrypted: true, dropSampling: true},
		{model: "gateway/chat:gpt-4o", jsonObject: true, systemRole: OpenAISystem},
	}
	for _, test := range tests {
		t.Run(test.model, func(t *testing.T) {
			route, err := Resolve(test.model, Generation)
			if err != nil {
				t.Fatal(err)
			}
			behavior := openAIBehaviorFor(route)
			if behavior.supportsJSONObject != test.jsonObject ||
				behavior.useLegacyMaxTokens != test.legacyTokens ||
				behavior.systemRole != test.systemRole ||
				behavior.includeEncryptedReasoning != test.encrypted ||
				behavior.dropSampling != test.dropSampling {
				t.Fatalf("behavior = %#v", behavior)
			}
		})
	}
}

func TestOpenAIChatOmitsUnsupportedJSONObjectAndUsesLegacyTokenField(t *testing.T) {
	fake := &openAIFake{responses: []any{chatResponse("chat_1", `{"value":"stable"}`, 1, 1)}}
	route, _ := Resolve("openrouter:anthropic/claude-sonnet-4", Generation)
	model, err := NewOpenAITextModel(route, OpenAIConfig{
		APIKey: "test", BaseURL: "https://provider.test/v1", HTTPClient: fake.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	maximum := int64(17)
	settings, _ := inference.NewGenerationSettings(nil, &maximum)
	generator, err := inference.NewPromptedGenerator[openAITestInput, openAITestOutput](
		model, "Return a value.", openAITestCodec(t), nil, settings,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := generator.Generate(t.Context(), openAITestInput{Value: "bounded"}); err != nil {
		t.Fatal(err)
	}
	body := fake.Requests()[0].body
	if _, exists := body["response_format"]; exists {
		t.Fatalf("unsupported response_format sent: %#v", body["response_format"])
	}
	if body["max_tokens"] != float64(17) {
		t.Fatalf("max_tokens = %#v", body["max_tokens"])
	}
	if _, exists := body["max_completion_tokens"]; exists {
		t.Fatalf("max_completion_tokens sent: %#v", body["max_completion_tokens"])
	}
	if body["stream"] != false {
		t.Fatalf("stream = %#v", body["stream"])
	}
}
