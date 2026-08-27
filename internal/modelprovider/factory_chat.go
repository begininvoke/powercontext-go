package modelprovider

import "github.com/ob-labs/powercontext-go/inference"

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
