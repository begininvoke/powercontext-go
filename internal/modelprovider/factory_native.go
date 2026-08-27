package modelprovider

import (
	"net/http"

	"github.com/ob-labs/powercontext-go/inference"
)

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
