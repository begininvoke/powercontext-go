package modelprovider

import (
	"encoding/json"
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/ob-labs/powercontext-go/inference"
)

type frozenProviderMatrix struct {
	PydanticAIVersion string   `json:"pydantic_ai_version"`
	Generation        []string `json:"generation"`
	Embedding         []string `json:"embedding"`
	Gateway           []string `json:"gateway"`
}

func TestRegistryMatchesFrozenPydanticAIOracle(t *testing.T) {
	encoded, err := os.ReadFile("../../test/conformance/testdata/python-v0.0.2/provider-matrix.json")
	if err != nil {
		t.Fatal(err)
	}
	var oracle frozenProviderMatrix
	if err := json.Unmarshal(encoded, &oracle); err != nil {
		t.Fatal(err)
	}
	if oracle.PydanticAIVersion != PydanticAIVersion {
		t.Fatalf("version = %q", oracle.PydanticAIVersion)
	}
	for _, pair := range []struct {
		name string
		got  []string
		want []string
	}{
		{name: "generation", got: GenerationProviders(), want: oracle.Generation},
		{name: "embedding", got: EmbeddingProviders(), want: oracle.Embedding},
		{name: "gateway", got: GatewayProviders(), want: oracle.Gateway},
	} {
		slices.Sort(pair.want)
		if !slices.Equal(pair.got, pair.want) {
			t.Errorf("%s providers:\n got %#v\nwant %#v", pair.name, pair.got, pair.want)
		}
	}
}

func TestResolveMatchesGenerationRoutingAndMilestones(t *testing.T) {
	tests := []struct {
		model       string
		protocol    Protocol
		canonical   string
		gateway     bool
		gatewayPath string
		availableA  bool
	}{
		{model: "openai:gpt-5", protocol: ProtocolOpenAIResponses, canonical: "openai", availableA: true},
		{model: "openai-chat:gpt-4o", protocol: ProtocolOpenAIChat, canonical: "openai-chat", availableA: true},
		{model: "azure-responses:deployment", protocol: ProtocolOpenAIResponses, canonical: "azure-responses", availableA: true},
		{model: "anthropic:claude-sonnet-4-5", protocol: ProtocolAnthropic, canonical: "anthropic", availableA: true},
		{model: "google:gemini-2.5-pro", protocol: ProtocolGoogle, canonical: "google"},
		{model: "gateway/chat:gpt-4o", protocol: ProtocolOpenAIChat, canonical: "openai-chat", gateway: true, gatewayPath: "openai", availableA: true},
		{model: "gateway/responses:gpt-5", protocol: ProtocolOpenAIResponses, canonical: "openai-responses", gateway: true, gatewayPath: "openai", availableA: true},
		{model: "gateway/google:gemini-2.5-pro", protocol: ProtocolGoogle, canonical: "google-cloud", gateway: true, gatewayPath: "google-vertex"},
		{model: "gateway/converse:anthropic.claude", protocol: ProtocolBedrock, canonical: "bedrock", gateway: true, gatewayPath: "bedrock"},
	}
	for _, test := range tests {
		t.Run(test.model, func(t *testing.T) {
			route, err := Resolve(test.model, Generation)
			if err != nil {
				t.Fatal(err)
			}
			if route.Protocol() != test.protocol || route.CanonicalProvider() != test.canonical || route.Gateway() != test.gateway || route.GatewayRoute() != test.gatewayPath {
				t.Fatalf("route = %#v", route)
			}
			if route.Available(MilestoneA) != test.availableA || !route.Available(MilestoneB) {
				t.Fatalf("availability = %d", route.AvailableAt())
			}
		})
	}
}

func TestResolveMatchesEmbeddingRouting(t *testing.T) {
	for _, model := range []string{
		"openai:text-embedding-3-small",
		"deepseek:some-embedding",
		"gateway/openai:text-embedding-3-small",
	} {
		route, err := Resolve(model, Embedding)
		if err != nil {
			t.Fatalf("%s: %v", model, err)
		}
		if route.Protocol() != ProtocolOpenAIEmbedding || !route.Available(MilestoneA) {
			t.Fatalf("%s route = %#v", model, route)
		}
	}
	for _, model := range []string{
		"bedrock:amazon.titan-embed-text-v2:0",
		"google:gemini-embedding-001",
		"cohere:embed-v4.0",
		"sentence-transformers:all-MiniLM-L6-v2",
	} {
		route, err := Resolve(model, Embedding)
		if err != nil {
			t.Fatalf("%s: %v", model, err)
		}
		if route.Model() == "" || route.Available(MilestoneA) || !route.Available(MilestoneB) {
			t.Fatalf("%s route = %#v", model, route)
		}
	}
	for _, model := range []string{
		"anthropic:not-an-embedding",
		"openai-chat:text-embedding-3-small",
		"gateway/chat:text-embedding-3-small",
		"gateway/anthropic:not-an-embedding",
		"gateway/groq:not-an-embedding",
	} {
		if _, err := Resolve(model, Embedding); err == nil {
			t.Fatalf("invalid embedding route accepted: %s", model)
		}
	}
}

func TestResolveRejectsMalformedAndUnknownModelIDs(t *testing.T) {
	for _, model := range []string{"", "openai", " openai:gpt-5", "openai: gpt-5", "unknown:model", "gateway/unknown:model"} {
		_, err := Resolve(model, Generation)
		var configuration *inference.ConfigurationError
		if !errors.As(err, &configuration) {
			t.Fatalf("%q error = %v", model, err)
		}
	}
}
