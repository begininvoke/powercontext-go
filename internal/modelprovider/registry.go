package modelprovider

import (
	"fmt"
	"slices"
	"strings"

	"github.com/thunguo/powercontext-go/inference"
)

const PydanticAIVersion = "2.29.0"

type Capability string

const (
	Generation Capability = "generation"
	Embedding  Capability = "embedding"
)

type Protocol string

const (
	ProtocolOpenAIChat           Protocol = "openai-chat"
	ProtocolOpenAIResponses      Protocol = "openai-responses"
	ProtocolOpenAIEmbedding      Protocol = "openai-embedding"
	ProtocolAnthropic            Protocol = "anthropic"
	ProtocolGoogle               Protocol = "google"
	ProtocolBedrock              Protocol = "bedrock"
	ProtocolBedrockMantle        Protocol = "bedrock-mantle"
	ProtocolCohere               Protocol = "cohere"
	ProtocolMistral              Protocol = "mistral"
	ProtocolGroq                 Protocol = "groq"
	ProtocolXAI                  Protocol = "xai"
	ProtocolHuggingFace          Protocol = "huggingface"
	ProtocolVoyageAI             Protocol = "voyageai"
	ProtocolSentenceTransformers Protocol = "sentence-transformers"
)

type Milestone uint8

const (
	MilestoneA Milestone = iota + 1
	MilestoneB
)

type Route struct {
	prefix       string
	model        string
	canonical    string
	protocol     Protocol
	gateway      bool
	gatewayRoute string
	availableAt  Milestone
}

func (r Route) Prefix() string                     { return r.prefix }
func (r Route) Model() string                      { return r.model }
func (r Route) CanonicalProvider() string          { return r.canonical }
func (r Route) Protocol() Protocol                 { return r.protocol }
func (r Route) Gateway() bool                      { return r.gateway }
func (r Route) GatewayRoute() string               { return r.gatewayRoute }
func (r Route) AvailableAt() Milestone             { return r.availableAt }
func (r Route) Available(milestone Milestone) bool { return milestone >= r.availableAt }

var generationProtocols = map[string]Protocol{
	"alibaba":          ProtocolOpenAIChat,
	"anthropic":        ProtocolAnthropic,
	"azure":            ProtocolOpenAIChat,
	"azure-responses":  ProtocolOpenAIResponses,
	"bedrock":          ProtocolBedrock,
	"bedrock-mantle":   ProtocolBedrockMantle,
	"cerebras":         ProtocolOpenAIChat,
	"cohere":           ProtocolCohere,
	"crusoe":           ProtocolOpenAIChat,
	"deepseek":         ProtocolOpenAIChat,
	"fireworks":        ProtocolOpenAIChat,
	"github":           ProtocolOpenAIChat,
	"google":           ProtocolGoogle,
	"google-cloud":     ProtocolGoogle,
	"groq":             ProtocolGroq,
	"heroku":           ProtocolOpenAIChat,
	"huggingface":      ProtocolHuggingFace,
	"litellm":          ProtocolOpenAIChat,
	"mistral":          ProtocolMistral,
	"moonshotai":       ProtocolOpenAIChat,
	"nebius":           ProtocolOpenAIChat,
	"ollama":           ProtocolOpenAIChat,
	"openai":           ProtocolOpenAIResponses,
	"openai-chat":      ProtocolOpenAIChat,
	"openai-responses": ProtocolOpenAIResponses,
	"openrouter":       ProtocolOpenAIChat,
	"ovhcloud":         ProtocolOpenAIChat,
	"sambanova":        ProtocolOpenAIChat,
	"snowflake":        ProtocolOpenAIChat,
	"together":         ProtocolOpenAIChat,
	"vercel":           ProtocolOpenAIChat,
	"xai":              ProtocolXAI,
	"zai":              ProtocolOpenAIChat,
}

var embeddingProtocols = map[string]Protocol{
	"alibaba":               ProtocolOpenAIEmbedding,
	"azure":                 ProtocolOpenAIEmbedding,
	"bedrock":               ProtocolBedrock,
	"cerebras":              ProtocolOpenAIEmbedding,
	"cohere":                ProtocolCohere,
	"crusoe":                ProtocolOpenAIEmbedding,
	"deepseek":              ProtocolOpenAIEmbedding,
	"fireworks":             ProtocolOpenAIEmbedding,
	"github":                ProtocolOpenAIEmbedding,
	"google":                ProtocolGoogle,
	"google-cloud":          ProtocolGoogle,
	"heroku":                ProtocolOpenAIEmbedding,
	"litellm":               ProtocolOpenAIEmbedding,
	"moonshotai":            ProtocolOpenAIEmbedding,
	"nebius":                ProtocolOpenAIEmbedding,
	"ollama":                ProtocolOpenAIEmbedding,
	"openai":                ProtocolOpenAIEmbedding,
	"openrouter":            ProtocolOpenAIEmbedding,
	"ovhcloud":              ProtocolOpenAIEmbedding,
	"sambanova":             ProtocolOpenAIEmbedding,
	"sentence-transformers": ProtocolSentenceTransformers,
	"snowflake":             ProtocolOpenAIEmbedding,
	"together":              ProtocolOpenAIEmbedding,
	"vercel":                ProtocolOpenAIEmbedding,
	"voyageai":              ProtocolVoyageAI,
	"zai":                   ProtocolOpenAIEmbedding,
}

var gatewayAliases = map[string]string{
	"chat":      "openai-chat",
	"responses": "openai-responses",
	"converse":  "bedrock",
	"google":    "google-cloud",
}

var gatewayProviders = []string{
	"anthropic", "bedrock", "chat", "converse", "google", "google-cloud",
	"groq", "openai", "openai-chat", "openai-responses", "responses",
}

func Resolve(modelID string, capability Capability) (Route, error) {
	prefix, model, ok := strings.Cut(modelID, ":")
	if !ok || strings.TrimSpace(prefix) == "" || strings.TrimSpace(model) == "" || modelID != strings.TrimSpace(modelID) {
		return Route{}, inference.NewConfigurationError("request-rejected", "invalid model identifier")
	}
	if prefix != strings.TrimSpace(prefix) || model != strings.TrimSpace(model) {
		return Route{}, inference.NewConfigurationError("request-rejected", "invalid model identifier")
	}

	gateway := strings.HasPrefix(prefix, "gateway/")
	canonical := prefix
	gatewayRoute := ""
	if gateway {
		upstream := strings.TrimPrefix(prefix, "gateway/")
		if !slices.Contains(gatewayProviders, upstream) {
			return Route{}, inference.NewConfigurationError("request-rejected", "unknown gateway provider")
		}
		canonical = gatewayAliases[upstream]
		if canonical == "" {
			canonical = upstream
		}
		gatewayRoute = canonical
		switch canonical {
		case "openai-chat", "openai-responses":
			gatewayRoute = "openai"
		case "google-cloud":
			gatewayRoute = "google-vertex"
		}
	}

	protocol, valid := protocolFor(canonical, capability, gateway)
	if !valid {
		return Route{}, inference.NewConfigurationError("request-rejected", fmt.Sprintf("provider %s does not support %s", prefix, capability))
	}
	availableAt := MilestoneB
	if capability == Generation && (protocol == ProtocolOpenAIChat || protocol == ProtocolOpenAIResponses || protocol == ProtocolAnthropic) {
		availableAt = MilestoneA
	}
	if capability == Embedding && protocol == ProtocolOpenAIEmbedding {
		availableAt = MilestoneA
	}
	return Route{
		prefix: prefix, model: model, canonical: canonical, protocol: protocol,
		gateway: gateway, gatewayRoute: gatewayRoute, availableAt: availableAt,
	}, nil
}

func RequireAvailable(route Route, milestone Milestone) error {
	if route.Available(milestone) {
		return nil
	}
	return inference.NewConfigurationError(
		"request-rejected",
		fmt.Sprintf("provider %s is not available in milestone %d", route.prefix, milestone),
	)
}

func GenerationProviders() []string { return sortedKeys(generationProtocols) }
func EmbeddingProviders() []string  { return sortedKeys(embeddingProtocols) }
func GatewayProviders() []string    { return slices.Clone(gatewayProviders) }

func protocolFor(canonical string, capability Capability, gateway bool) (Protocol, bool) {
	if !gateway {
		switch capability {
		case Generation:
			protocol, ok := generationProtocols[canonical]
			return protocol, ok
		case Embedding:
			protocol, ok := embeddingProtocols[canonical]
			return protocol, ok
		default:
			return "", false
		}
	}
	if capability == Generation {
		protocol, ok := generationProtocols[canonical]
		return protocol, ok
	}
	if capability == Embedding {
		// Pydantic AI 2.29.0 only routes the canonical Gateway OpenAI,
		// Vertex, and Bedrock names through its embedding dispatcher. API
		// flavor aliases such as gateway/chat intentionally remain invalid.
		switch canonical {
		case "openai":
			return ProtocolOpenAIEmbedding, true
		case "google-cloud":
			return ProtocolGoogle, true
		case "bedrock":
			return ProtocolBedrock, true
		}
	}
	return "", false
}

func sortedKeys[V any](values map[string]V) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	slices.Sort(result)
	return result
}
