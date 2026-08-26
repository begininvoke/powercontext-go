package modelprovider

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/ob-labs/powercontext-go/inference"
)

type EnvLookup func(string) (string, bool)

func ProcessEnvironment(name string) (string, bool) { return os.LookupEnv(name) }

type Factory struct {
	milestone  Milestone
	lookup     EnvLookup
	httpClient *http.Client
}

func NewFactory(milestone Milestone, lookup EnvLookup, httpClient *http.Client) (*Factory, error) {
	if milestone != MilestoneA && milestone != MilestoneB {
		return nil, inference.NewConfigurationError("model", "invalid provider milestone")
	}
	if lookup == nil {
		return nil, inference.NewConfigurationError("model", "environment lookup is required")
	}
	return &Factory{milestone: milestone, lookup: lookup, httpClient: httpClient}, nil
}

func (f *Factory) TextModel(modelID string) (inference.TextModel, error) {
	route, err := Resolve(modelID, Generation)
	if err != nil {
		return nil, err
	}
	if err := RequireAvailable(route, f.milestone); err != nil {
		return nil, err
	}
	if err := validateProviderModel(route); err != nil {
		return nil, err
	}
	switch route.protocol {
	case ProtocolOpenAIChat, ProtocolOpenAIResponses:
		config, configErr := f.openAIConfig(route)
		if configErr != nil {
			return nil, configErr
		}
		return NewOpenAITextModel(route, config)
	case ProtocolAnthropic:
		config, configErr := f.anthropicConfig(route)
		if configErr != nil {
			return nil, configErr
		}
		return NewAnthropicTextModel(route, config)
	case ProtocolGroq, ProtocolXAI, ProtocolMistral, ProtocolHuggingFace:
		config, configErr := f.chatHTTPConfig(route)
		if configErr != nil {
			return nil, configErr
		}
		return NewChatHTTPTextModel(route, config)
	case ProtocolGoogle:
		config, configErr := f.googleConfig(route)
		if configErr != nil {
			return nil, configErr
		}
		return NewGoogleTextModel(context.Background(), route, config)
	case ProtocolCohere:
		config, configErr := f.cohereConfig()
		if configErr != nil {
			return nil, configErr
		}
		return NewCohereTextModel(route, config)
	case ProtocolBedrock:
		config, configErr := f.bedrockConfig(route)
		if configErr != nil {
			return nil, configErr
		}
		return NewBedrockTextModel(context.Background(), route, config)
	case ProtocolBedrockMantle:
		config, configErr := f.bedrockMantleConfig()
		if configErr != nil {
			return nil, configErr
		}
		return NewBedrockMantleTextModel(route, config)
	default:
		return nil, inference.NewConfigurationError("model", "provider adapter is not implemented")
	}
}

func (f *Factory) EmbeddingTransport(modelID string) (inference.EmbeddingTransport, error) {
	route, err := Resolve(modelID, Embedding)
	if err != nil {
		return nil, err
	}
	if err := RequireAvailable(route, f.milestone); err != nil {
		return nil, err
	}
	if err := validateProviderModel(route); err != nil {
		return nil, err
	}
	switch route.protocol {
	case ProtocolOpenAIEmbedding:
		config, configErr := f.openAIConfig(route)
		if configErr != nil {
			return nil, configErr
		}
		return NewOpenAIEmbeddingTransport(route, config)
	case ProtocolGoogle:
		config, configErr := f.googleConfig(route)
		if configErr != nil {
			return nil, configErr
		}
		return NewGoogleEmbeddingTransport(context.Background(), route, config)
	case ProtocolCohere:
		config, configErr := f.cohereConfig()
		if configErr != nil {
			return nil, configErr
		}
		return NewCohereEmbeddingTransport(route, config)
	case ProtocolBedrock:
		config, configErr := f.bedrockConfig(route)
		if configErr != nil {
			return nil, configErr
		}
		return NewBedrockEmbeddingTransport(context.Background(), route, config)
	case ProtocolVoyageAI:
		config, configErr := f.voyageAIConfig()
		if configErr != nil {
			return nil, configErr
		}
		return NewVoyageAIEmbeddingTransport(route, config)
	case ProtocolSentenceTransformers:
		return newSentenceTransformersTransport(route, f.lookup)
	default:
		return nil, inference.NewConfigurationError("embedding-model", "provider adapter is not implemented")
	}
}

func (f *Factory) chatHTTPConfig(route Route) (ChatHTTPConfig, error) {
	if route.gateway {
		key, baseURL, err := f.gatewayCredentials(route)
		if err != nil {
			return ChatHTTPConfig{}, err
		}
		return ChatHTTPConfig{
			APIKey: key, BaseURL: baseURL, EndpointPath: "/openai/v1/chat/completions",
			HTTPClient: f.httpClient, SendCandidateCount: true,
		}, nil
	}
	type spec struct {
		key             string
		baseEnvironment string
		baseURL         string
		endpoint        string
		jsonObject      bool
	}
	specs := map[Protocol]spec{
		ProtocolGroq: {
			key: "GROQ_API_KEY", baseEnvironment: "GROQ_BASE_URL",
			baseURL: "https://api.groq.com", endpoint: "/openai/v1/chat/completions",
		},
		ProtocolXAI: {
			key: "XAI_API_KEY", baseEnvironment: "XAI_BASE_URL",
			baseURL: "https://api.x.ai/v1", endpoint: "/chat/completions", jsonObject: true,
		},
		ProtocolMistral: {
			key: "MISTRAL_API_KEY", baseEnvironment: "MISTRAL_BASE_URL",
			baseURL: "https://api.mistral.ai", endpoint: "/v1/chat/completions",
		},
		ProtocolHuggingFace: {
			key: "HF_TOKEN", baseEnvironment: "HF_INFERENCE_BASE_URL",
			baseURL: "https://router.huggingface.co/v1", endpoint: "/chat/completions",
		},
	}
	selected, ok := specs[route.protocol]
	if !ok {
		return ChatHTTPConfig{}, inference.NewConfigurationError("model", "unsupported HTTP chat provider")
	}
	key, ok := f.nonEmpty(selected.key)
	if !ok {
		return ChatHTTPConfig{}, missingProviderCredential(route.canonical, selected.key)
	}
	baseURL, ok := f.nonEmpty(selected.baseEnvironment)
	if !ok {
		baseURL = selected.baseURL
	}
	return ChatHTTPConfig{
		APIKey: key, BaseURL: baseURL, EndpointPath: selected.endpoint,
		HTTPClient: f.httpClient, SupportsJSONObject: selected.jsonObject,
		SendCandidateCount: route.protocol == ProtocolGroq || route.protocol == ProtocolMistral,
	}, nil
}

func (f *Factory) googleConfig(route Route) (GoogleConfig, error) {
	if route.gateway {
		key, baseURL, err := f.gatewayCredentials(route)
		if err != nil {
			return GoogleConfig{}, err
		}
		return GoogleConfig{
			Backend: GoogleVertexAI, APIKey: key, BaseURL: baseURL, HTTPClient: f.httpClient,
			Headers: http.Header{"Authorization": []string{"Bearer " + key}},
		}, nil
	}
	if route.canonical == "google" {
		key, ok := f.firstNonEmpty("GOOGLE_API_KEY", "GEMINI_API_KEY")
		if !ok {
			return GoogleConfig{}, missingProviderCredential("google", "GOOGLE_API_KEY", "GEMINI_API_KEY")
		}
		baseURL, _ := f.nonEmpty("GOOGLE_BASE_URL")
		return GoogleConfig{
			Backend: GoogleGeminiAPI, APIKey: key, BaseURL: baseURL, HTTPClient: f.httpClient,
		}, nil
	}
	baseURL, _ := f.nonEmpty("GOOGLE_CLOUD_BASE_URL")
	if key, ok := f.firstNonEmpty("GOOGLE_API_KEY", "GEMINI_API_KEY"); ok {
		return GoogleConfig{
			Backend: GoogleVertexAI, APIKey: key, BaseURL: baseURL, HTTPClient: f.httpClient,
		}, nil
	}
	project, ok := f.nonEmpty("GOOGLE_CLOUD_PROJECT")
	if !ok && baseURL == "" {
		return GoogleConfig{}, inference.NewConfigurationError("model", "GOOGLE_CLOUD_PROJECT is required for Google Cloud ADC")
	}
	location, ok := f.nonEmpty("GOOGLE_CLOUD_LOCATION")
	if !ok {
		location = "us-central1"
	}
	return GoogleConfig{
		Backend: GoogleVertexAI, Project: project, Location: location,
		BaseURL: baseURL, HTTPClient: f.httpClient,
	}, nil
}

func (f *Factory) cohereConfig() (CohereConfig, error) {
	key, ok := f.nonEmpty("CO_API_KEY")
	if !ok {
		return CohereConfig{}, missingProviderCredential("cohere", "CO_API_KEY")
	}
	baseURL, ok := f.nonEmpty("CO_BASE_URL")
	if !ok {
		baseURL = "https://api.cohere.com"
	}
	return CohereConfig{APIKey: key, BaseURL: baseURL, HTTPClient: f.httpClient}, nil
}

func (f *Factory) bedrockConfig(route Route) (BedrockConfig, error) {
	if route.gateway {
		key, baseURL, err := f.gatewayCredentials(route)
		if err != nil {
			return BedrockConfig{}, err
		}
		return BedrockConfig{
			Region: "pydantic-ai-gateway", BaseURL: baseURL, BearerToken: key, HTTPClient: f.httpClient,
		}, nil
	}
	region, _ := f.firstNonEmpty("AWS_DEFAULT_REGION", "AWS_REGION")
	baseURL, _ := f.nonEmpty("BEDROCK_BASE_URL")
	token, _ := f.nonEmpty("AWS_BEARER_TOKEN_BEDROCK")
	return BedrockConfig{Region: region, BaseURL: baseURL, BearerToken: token, HTTPClient: f.httpClient}, nil
}

func (f *Factory) bedrockMantleConfig() (BedrockMantleConfig, error) {
	token, _ := f.nonEmpty("AWS_BEARER_TOKEN_BEDROCK")
	region, hasRegion := f.firstNonEmpty("AWS_DEFAULT_REGION", "AWS_REGION")
	origin, ok := f.firstNonEmpty("BEDROCK_MANTLE_BASE_URL", "AWS_BEDROCK_BASE_URL")
	if !ok {
		if !hasRegion {
			return BedrockMantleConfig{}, inference.NewConfigurationError("model", "AWS region is required for Bedrock Mantle")
		}
		origin = "https://bedrock-mantle." + region + ".api.aws"
	}
	accessKey, secretKey, sessionToken, profile := "", "", "", ""
	if token == "" {
		accessKey, _ = f.nonEmpty("AWS_ACCESS_KEY_ID")
		secretKey, _ = f.nonEmpty("AWS_SECRET_ACCESS_KEY")
		sessionToken, _ = f.nonEmpty("AWS_SESSION_TOKEN")
		profile, _ = f.nonEmpty("AWS_PROFILE")
	}
	return BedrockMantleConfig{
		APIKey: token, Region: region, Origin: origin,
		AWSAccessKeyID: accessKey, AWSSecretAccessKey: secretKey,
		AWSSessionToken: sessionToken, AWSProfile: profile, HTTPClient: f.httpClient,
	}, nil
}

func (f *Factory) voyageAIConfig() (VoyageAIConfig, error) {
	key, ok := f.nonEmpty("VOYAGE_API_KEY")
	if !ok {
		return VoyageAIConfig{}, missingProviderCredential("voyageai", "VOYAGE_API_KEY")
	}
	baseURL, ok := f.nonEmpty("VOYAGE_BASE_URL")
	if !ok {
		baseURL = "https://api.voyageai.com/v1"
	}
	return VoyageAIConfig{APIKey: key, BaseURL: baseURL, HTTPClient: f.httpClient}, nil
}

type openAIProviderSpec struct {
	baseURL string
	keys    []string
	headers http.Header
}

var openAIProviderSpecs = map[string]openAIProviderSpec{
	"alibaba": {
		baseURL: "https://dashscope-intl.aliyuncs.com/compatible-mode/v1",
		keys:    []string{"ALIBABA_API_KEY", "DASHSCOPE_API_KEY"},
	},
	"cerebras": {
		baseURL: "https://api.cerebras.ai/v1",
		keys:    []string{"CEREBRAS_API_KEY"},
		headers: http.Header{"X-Cerebras-3rd-Party-Integration": []string{"pydantic-ai"}},
	},
	"crusoe":     {baseURL: "https://api.inference.crusoecloud.com/v1", keys: []string{"CRUSOE_API_KEY"}},
	"deepseek":   {baseURL: "https://api.deepseek.com", keys: []string{"DEEPSEEK_API_KEY"}},
	"fireworks":  {baseURL: "https://api.fireworks.ai/inference/v1", keys: []string{"FIREWORKS_API_KEY"}},
	"github":     {baseURL: "https://models.github.ai/inference", keys: []string{"GITHUB_API_KEY"}},
	"moonshotai": {baseURL: "https://api.moonshot.ai/v1", keys: []string{"MOONSHOTAI_API_KEY"}},
	"nebius":     {baseURL: "https://api.studio.nebius.com/v1", keys: []string{"NEBIUS_API_KEY"}},
	"openrouter": {baseURL: "https://openrouter.ai/api/v1", keys: []string{"OPENROUTER_API_KEY"}},
	"ovhcloud":   {baseURL: "https://oai.endpoints.kepler.ai.cloud.ovh.net/v1", keys: []string{"OVHCLOUD_API_KEY"}},
	"together":   {baseURL: "https://api.together.xyz/v1", keys: []string{"TOGETHER_API_KEY"}},
	"zai":        {baseURL: "https://api.z.ai/api/paas/v4", keys: []string{"ZAI_API_KEY"}},
}

func (f *Factory) openAIConfig(route Route) (OpenAIConfig, error) {
	if route.gateway {
		key, baseURL, err := f.gatewayCredentials(route)
		if err != nil {
			return OpenAIConfig{}, err
		}
		return OpenAIConfig{APIKey: key, BaseURL: baseURL, HTTPClient: f.httpClient}, nil
	}

	provider := route.canonical
	switch provider {
	case "openai", "openai-chat", "openai-responses":
		return f.openAIProviderConfig()
	case "azure", "azure-responses":
		return f.azureConfig(route)
	case "heroku":
		return f.herokuConfig()
	case "litellm":
		return OpenAIConfig{
			APIKey: "litellm-placeholder", BaseURL: "https://api.openai.com/v1", HTTPClient: f.httpClient,
		}, nil
	case "ollama":
		return f.ollamaConfig()
	case "sambanova":
		return f.sambaNovaConfig()
	case "snowflake":
		return f.snowflakeConfig()
	case "vercel":
		return f.vercelConfig()
	default:
		spec, ok := openAIProviderSpecs[provider]
		if !ok {
			return OpenAIConfig{}, inference.NewConfigurationError("model", "unknown OpenAI-compatible provider")
		}
		key, ok := f.firstNonEmpty(spec.keys...)
		if !ok {
			return OpenAIConfig{}, missingProviderCredential(provider, spec.keys...)
		}
		headers := spec.headers.Clone()
		if headers == nil {
			headers = make(http.Header)
		}
		config := OpenAIConfig{
			APIKey: key, BaseURL: spec.baseURL, HTTPClient: f.httpClient, Headers: headers,
		}
		if provider == "openrouter" {
			if value, found := f.nonEmpty("OPENROUTER_APP_URL"); found {
				config.Headers.Set("HTTP-Referer", value)
			}
			if value, found := f.nonEmpty("OPENROUTER_APP_TITLE"); found {
				config.Headers.Set("X-Title", value)
			}
		}
		return config, nil
	}
}

func (f *Factory) openAIProviderConfig() (OpenAIConfig, error) {
	baseURL, hasBaseURL := f.nonEmpty("OPENAI_BASE_URL")
	if !hasBaseURL {
		baseURL = "https://api.openai.com/v1"
	}
	key, hasKey := f.nonEmpty("OPENAI_API_KEY")
	if !hasKey {
		if !hasBaseURL {
			return OpenAIConfig{}, missingProviderCredential("openai", "OPENAI_API_KEY")
		}
		key = "api-key-not-set"
	}
	return OpenAIConfig{APIKey: key, BaseURL: baseURL, HTTPClient: f.httpClient}, nil
}

func (f *Factory) anthropicConfig(route Route) (AnthropicConfig, error) {
	if route.gateway {
		key, baseURL, err := f.gatewayCredentials(route)
		if err != nil {
			return AnthropicConfig{}, err
		}
		return AnthropicConfig{
			APIKey: key, BaseURL: baseURL, AuthMode: AnthropicBearerToken, HTTPClient: f.httpClient,
		}, nil
	}
	key, ok := f.nonEmpty("ANTHROPIC_API_KEY")
	if !ok {
		return AnthropicConfig{}, missingProviderCredential("anthropic", "ANTHROPIC_API_KEY")
	}
	baseURL, ok := f.nonEmpty("ANTHROPIC_BASE_URL")
	if !ok {
		baseURL = "https://api.anthropic.com"
	}
	return AnthropicConfig{APIKey: key, BaseURL: baseURL, AuthMode: AnthropicAPIKey, HTTPClient: f.httpClient}, nil
}

func (f *Factory) herokuConfig() (OpenAIConfig, error) {
	key, ok := f.nonEmpty("HEROKU_INFERENCE_KEY")
	if !ok {
		return OpenAIConfig{}, missingProviderCredential("heroku", "HEROKU_INFERENCE_KEY")
	}
	baseURL, ok := f.nonEmpty("HEROKU_INFERENCE_URL")
	if !ok {
		baseURL = "https://us.inference.heroku.com"
	}
	baseURL = strings.TrimRight(baseURL, "/")
	if !strings.HasSuffix(baseURL, "/v1") {
		baseURL += "/v1"
	}
	return OpenAIConfig{APIKey: key, BaseURL: baseURL, HTTPClient: f.httpClient}, nil
}

func (f *Factory) ollamaConfig() (OpenAIConfig, error) {
	baseURL, ok := f.nonEmpty("OLLAMA_BASE_URL")
	if !ok {
		return OpenAIConfig{}, inference.NewConfigurationError("model", "OLLAMA_BASE_URL is required")
	}
	key, ok := f.nonEmpty("OLLAMA_API_KEY")
	if !ok {
		key = "api-key-not-set"
	}
	return OpenAIConfig{APIKey: key, BaseURL: baseURL, HTTPClient: f.httpClient}, nil
}

func (f *Factory) sambaNovaConfig() (OpenAIConfig, error) {
	key, ok := f.nonEmpty("SAMBANOVA_API_KEY")
	if !ok {
		return OpenAIConfig{}, missingProviderCredential("sambanova", "SAMBANOVA_API_KEY")
	}
	baseURL, ok := f.nonEmpty("SAMBANOVA_BASE_URL")
	if !ok {
		baseURL = "https://api.sambanova.ai/v1"
	}
	return OpenAIConfig{APIKey: key, BaseURL: baseURL, HTTPClient: f.httpClient}, nil
}

func (f *Factory) snowflakeConfig() (OpenAIConfig, error) {
	account, ok := f.nonEmpty("SNOWFLAKE_ACCOUNT")
	if !ok {
		return OpenAIConfig{}, missingProviderCredential("snowflake", "SNOWFLAKE_ACCOUNT")
	}
	account = strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(strings.TrimSuffix(account, "/"), "https://"), "http://"), ".snowflakecomputing.com")
	if !snowflakeAccountPattern.MatchString(account) {
		return OpenAIConfig{}, inference.NewConfigurationError("model", "invalid Snowflake account identifier")
	}
	token, ok := f.nonEmpty("SNOWFLAKE_TOKEN")
	if !ok {
		return OpenAIConfig{}, missingProviderCredential("snowflake", "SNOWFLAKE_TOKEN")
	}
	return OpenAIConfig{
		APIKey:     token,
		BaseURL:    fmt.Sprintf("https://%s.snowflakecomputing.com/api/v2/cortex/v1", account),
		HTTPClient: f.httpClient,
	}, nil
}

func (f *Factory) vercelConfig() (OpenAIConfig, error) {
	key, ok := f.firstNonEmpty("VERCEL_AI_GATEWAY_API_KEY", "VERCEL_OIDC_TOKEN")
	if !ok {
		return OpenAIConfig{}, missingProviderCredential("vercel", "VERCEL_AI_GATEWAY_API_KEY", "VERCEL_OIDC_TOKEN")
	}
	return OpenAIConfig{
		APIKey: key, BaseURL: "https://ai-gateway.vercel.sh/v1", HTTPClient: f.httpClient,
		Headers: http.Header{"Http-Referer": []string{"https://ai.pydantic.dev/"}, "X-Title": []string{"pydantic-ai"}},
	}, nil
}

func (f *Factory) azureConfig(route Route) (OpenAIConfig, error) {
	endpoint, ok := f.nonEmpty("AZURE_OPENAI_ENDPOINT")
	if !ok {
		return OpenAIConfig{}, inference.NewConfigurationError("model", "AZURE_OPENAI_ENDPOINT is required")
	}
	key, ok := f.nonEmpty("AZURE_OPENAI_API_KEY")
	if !ok {
		return OpenAIConfig{}, missingProviderCredential("azure", "AZURE_OPENAI_API_KEY")
	}
	endpoint = strings.TrimRight(endpoint, "/")
	parsed, parseErr := url.Parse(endpoint)
	if parseErr != nil || parsed.Host == "" {
		return OpenAIConfig{}, inference.NewConfigurationError("model", "invalid Azure OpenAI endpoint")
	}
	if strings.HasSuffix(endpoint, "/v1") || strings.HasSuffix(strings.ToLower(parsed.Hostname()), ".models.ai.azure.com") {
		if !strings.HasSuffix(endpoint, "/v1") {
			endpoint += "/v1"
		}
		return OpenAIConfig{APIKey: key, BaseURL: endpoint, HTTPClient: f.httpClient}, nil
	}
	version, ok := f.nonEmpty("OPENAI_API_VERSION")
	if !ok {
		return OpenAIConfig{}, inference.NewConfigurationError("model", "OPENAI_API_VERSION is required for Azure OpenAI")
	}
	baseURL := endpoint + "/openai"
	if route.protocol != ProtocolOpenAIResponses {
		baseURL += "/deployments/" + url.PathEscape(route.model)
	}
	return OpenAIConfig{
		APIKey: key, BaseURL: baseURL, HTTPClient: f.httpClient,
		Headers: http.Header{"Authorization": nil, "Api-Key": []string{key}},
		Query:   url.Values{"api-version": []string{version}},
	}, nil
}

var (
	snowflakeAccountPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	gatewayKeyPattern       = regexp.MustCompile(`^pylf_v[0-9]+_([a-z]+)_[A-Za-z0-9_-]+$`)
)

func (f *Factory) gatewayCredentials(route Route) (string, string, error) {
	key, ok := f.firstNonEmpty("PYDANTIC_AI_GATEWAY_API_KEY", "PAIG_API_KEY")
	if !ok {
		return "", "", missingProviderCredential("gateway", "PYDANTIC_AI_GATEWAY_API_KEY")
	}
	baseURL, ok := f.firstNonEmpty("PYDANTIC_AI_GATEWAY_BASE_URL", "PAIG_BASE_URL")
	if !ok {
		matches := gatewayKeyPattern.FindStringSubmatch(key)
		if len(matches) != 2 {
			return "", "", inference.NewConfigurationError("model", "gateway base URL cannot be inferred from API key")
		}
		region := matches[1]
		if strings.HasPrefix(region, "staging") {
			baseURL = "https://gateway.pydantic.info/proxy"
		} else {
			baseURL = "https://gateway-" + region + ".pydantic.dev/proxy"
		}
	}
	return key, strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(route.gatewayRoute, "/"), nil
}

func (f *Factory) nonEmpty(name string) (string, bool) {
	value, ok := f.lookup(name)
	return value, ok && value != ""
}

func (f *Factory) firstNonEmpty(names ...string) (string, bool) {
	for _, name := range names {
		if value, ok := f.nonEmpty(name); ok {
			return value, true
		}
	}
	return "", false
}

func validateProviderModel(route Route) error {
	if !route.gateway && route.canonical == "openrouter" {
		model := strings.TrimPrefix(route.model, "~")
		if !strings.Contains(model, "/") {
			return inference.NewConfigurationError("model", "OpenRouter model must include its upstream provider")
		}
	}
	return nil
}

func missingProviderCredential(provider string, names ...string) error {
	return inference.NewConfigurationError(
		"model",
		fmt.Sprintf("%s provider requires %s", provider, strings.Join(names, " or ")),
	)
}
