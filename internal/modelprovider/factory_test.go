package modelprovider

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/thunguo/powercontext-go/inference"
)

type testEnvironment map[string]string

func (e testEnvironment) lookup(name string) (string, bool) {
	value, ok := e[name]
	return value, ok
}

func TestFactoryResolvesFrozenOpenAICompatibleProviderConfiguration(t *testing.T) {
	tests := []struct {
		model       string
		environment testEnvironment
		baseURL     string
		apiKey      string
		header      string
		headerValue string
	}{
		{model: "alibaba:qwen-max", environment: testEnvironment{"DASHSCOPE_API_KEY": "dash"}, baseURL: "https://dashscope-intl.aliyuncs.com/compatible-mode/v1", apiKey: "dash"},
		{model: "cerebras:llama-3.3", environment: testEnvironment{"CEREBRAS_API_KEY": "cerebras"}, baseURL: "https://api.cerebras.ai/v1", apiKey: "cerebras", header: "X-Cerebras-3rd-Party-Integration", headerValue: "pydantic-ai"},
		{model: "heroku:model", environment: testEnvironment{"HEROKU_INFERENCE_KEY": "heroku", "HEROKU_INFERENCE_URL": "https://heroku.test/custom"}, baseURL: "https://heroku.test/custom/v1", apiKey: "heroku"},
		{model: "ollama:qwen3", environment: testEnvironment{"OLLAMA_BASE_URL": "http://localhost:11434/v1"}, baseURL: "http://localhost:11434/v1", apiKey: "api-key-not-set"},
		{model: "openrouter:openai/gpt-5", environment: testEnvironment{"OPENROUTER_API_KEY": "router", "OPENROUTER_APP_URL": "https://app.test"}, baseURL: "https://openrouter.ai/api/v1", apiKey: "router", header: "HTTP-Referer", headerValue: "https://app.test"},
		{model: "sambanova:model", environment: testEnvironment{"SAMBANOVA_API_KEY": "samba", "SAMBANOVA_BASE_URL": "https://samba.test/v1"}, baseURL: "https://samba.test/v1", apiKey: "samba"},
		{model: "snowflake:openai-gpt-4.1", environment: testEnvironment{"SNOWFLAKE_ACCOUNT": "org-account.snowflakecomputing.com", "SNOWFLAKE_TOKEN": "snow"}, baseURL: "https://org-account.snowflakecomputing.com/api/v2/cortex/v1", apiKey: "snow"},
		{model: "vercel:openai/gpt-5", environment: testEnvironment{"VERCEL_OIDC_TOKEN": "vercel"}, baseURL: "https://ai-gateway.vercel.sh/v1", apiKey: "vercel", header: "HTTP-Referer", headerValue: "https://ai.pydantic.dev/"},
	}
	for _, test := range tests {
		t.Run(test.model, func(t *testing.T) {
			factory, err := NewFactory(MilestoneA, test.environment.lookup, nil)
			if err != nil {
				t.Fatal(err)
			}
			route, err := Resolve(test.model, Generation)
			if err != nil {
				t.Fatal(err)
			}
			config, err := factory.openAIConfig(route)
			if err != nil {
				t.Fatal(err)
			}
			if config.BaseURL != test.baseURL || config.APIKey != test.apiKey {
				t.Fatalf("config = %#v", config)
			}
			if test.header != "" && config.Headers.Get(test.header) != test.headerValue {
				t.Fatalf("headers = %#v", config.Headers)
			}
		})
	}
}

func TestFactoryOpenAIKeylessCustomEndpointAndGatewayRouting(t *testing.T) {
	factory, err := NewFactory(MilestoneA, testEnvironment{
		"OPENAI_BASE_URL": "http://localhost:8080/v1",
	}.lookup, nil)
	if err != nil {
		t.Fatal(err)
	}
	route, _ := Resolve("openai-chat:test", Generation)
	config, err := factory.openAIConfig(route)
	if err != nil {
		t.Fatal(err)
	}
	if config.APIKey != "api-key-not-set" || config.BaseURL != "http://localhost:8080/v1" {
		t.Fatalf("config = %#v", config)
	}

	gateway, _ := NewFactory(MilestoneA, testEnvironment{
		"PYDANTIC_AI_GATEWAY_API_KEY": "pylf_v1_us_token",
	}.lookup, nil)
	for _, test := range []struct {
		model string
		want  string
	}{
		{model: "gateway/chat:gpt-4o", want: "https://gateway-us.pydantic.dev/proxy/openai"},
		{model: "gateway/anthropic:claude", want: "https://gateway-us.pydantic.dev/proxy/anthropic"},
	} {
		route, _ := Resolve(test.model, Generation)
		if route.protocol == ProtocolAnthropic {
			config, err := gateway.anthropicConfig(route)
			if err != nil || config.BaseURL != test.want || config.AuthMode != AnthropicBearerToken {
				t.Fatalf("%s config = %#v, %v", test.model, config, err)
			}
		} else {
			config, err := gateway.openAIConfig(route)
			if err != nil || config.BaseURL != test.want {
				t.Fatalf("%s config = %#v, %v", test.model, config, err)
			}
		}
	}
}

func TestFactoryAzureLegacyPathsHeadersAndQuery(t *testing.T) {
	fake := &openAIFake{responses: []any{
		chatResponse("chat_1", `{"value":"stable"}`, 1, 1),
		responsesResponse("resp_1", "msg_1", `{"value":"stable"}`, 1, 1),
	}}
	factory, err := NewFactory(MilestoneA, testEnvironment{
		"AZURE_OPENAI_ENDPOINT": "https://resource.openai.azure.com",
		"AZURE_OPENAI_API_KEY":  "azure-key",
		"OPENAI_API_VERSION":    "2024-10-21",
	}.lookup, fake.Client())
	if err != nil {
		t.Fatal(err)
	}
	for _, modelID := range []string{"azure:deployment", "azure-responses:deployment"} {
		model, err := factory.TextModel(modelID)
		if err != nil {
			t.Fatal(err)
		}
		generator, err := inference.NewPromptedGenerator[openAITestInput, openAITestOutput](
			model, "Return a value.", openAITestCodec(t), nil, inference.GenerationSettings{},
		)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := generator.Generate(t.Context(), openAITestInput{Value: "bounded"}); err != nil {
			t.Fatal(err)
		}
	}
	requests := fake.Requests()
	if len(requests) != 2 {
		t.Fatalf("requests = %#v", requests)
	}
	if requests[0].path != "/openai/deployments/deployment/chat/completions?api-version=2024-10-21" {
		t.Fatalf("chat path = %q", requests[0].path)
	}
	if requests[1].path != "/openai/responses?api-version=2024-10-21" {
		t.Fatalf("responses path = %q", requests[1].path)
	}
	for _, request := range requests {
		if request.authorization != "" || request.apiKey != "azure-key" {
			t.Fatalf("Azure request authentication = %#v", request)
		}
	}
}

func TestFactoryRejectsMissingCredentialsWithoutLeakingValues(t *testing.T) {
	for _, modelID := range []string{
		"openai:gpt-5", "anthropic:claude", "openrouter:openai/gpt-5", "ollama:qwen3",
		"gateway/chat:gpt-4o",
	} {
		factory, _ := NewFactory(MilestoneA, testEnvironment{"UNRELATED": "secret-value"}.lookup, nil)
		_, err := factory.TextModel(modelID)
		var configuration *inference.ConfigurationError
		if !errors.As(err, &configuration) || strings.Contains(err.Error(), "secret-value") {
			t.Fatalf("%s error = %v", modelID, err)
		}
	}
}

func TestFactoryRejectsMilestoneBProviderUntilRequestedMilestone(t *testing.T) {
	factory, _ := NewFactory(MilestoneA, testEnvironment{}.lookup, nil)
	_, err := factory.TextModel("google:gemini-3")
	var configuration *inference.ConfigurationError
	if !errors.As(err, &configuration) {
		t.Fatalf("error = %v", err)
	}
}

func TestFactoryOpenRouterRequiresUpstreamModelPrefix(t *testing.T) {
	factory, _ := NewFactory(MilestoneA, testEnvironment{"OPENROUTER_API_KEY": "key"}.lookup, nil)
	_, err := factory.TextModel("openrouter:model-without-provider")
	var configuration *inference.ConfigurationError
	if !errors.As(err, &configuration) {
		t.Fatalf("error = %v", err)
	}
}

func TestFactoryUsesPydanticGatewayBearerForAnthropic(t *testing.T) {
	fake := &anthropicFake{responses: []any{anthropicResponse("msg_1", `{"value":"stable"}`, 1, 1)}}
	factory, _ := NewFactory(MilestoneA, testEnvironment{
		"PYDANTIC_AI_GATEWAY_API_KEY":  "opaque-key",
		"PYDANTIC_AI_GATEWAY_BASE_URL": "https://gateway.test/proxy",
	}.lookup, &http.Client{Transport: fake})
	model, err := factory.TextModel("gateway/anthropic:claude-test")
	if err != nil {
		t.Fatal(err)
	}
	generator, _ := inference.NewPromptedGenerator[openAITestInput, openAITestOutput](
		model, "Return a value.", openAITestCodec(t), nil, inference.GenerationSettings{},
	)
	if _, err := generator.Generate(t.Context(), openAITestInput{Value: "bounded"}); err != nil {
		t.Fatal(err)
	}
	request := fake.Requests()[0]
	if request.path != "/proxy/anthropic/v1/messages" || request.authorization != "Bearer opaque-key" || request.apiKey != "" {
		t.Fatalf("request = %#v", request)
	}
}
