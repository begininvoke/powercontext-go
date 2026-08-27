package modelprovider

import (
	"context"
	"fmt"
	"net/http"
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

var gatewayKeyPattern = regexp.MustCompile(`^pylf_v[0-9]+_([a-z]+)_[A-Za-z0-9_-]+$`)

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
