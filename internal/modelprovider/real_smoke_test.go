package modelprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/thunguo/powercontext-go/inference"
)

// TestRealProviderSmoke is deliberately opt-in. CI's deterministic provider
// matrix uses fake HTTP servers; this test is the narrow credentialed check
// that protocol adapters can complete one real request without logging model
// input, model output, or credentials.
func TestRealProviderSmoke(t *testing.T) {
	generationModel := os.Getenv("POWERCONTEXT_REAL_SMOKE_GENERATION_MODEL")
	embeddingModel := os.Getenv("POWERCONTEXT_REAL_SMOKE_EMBEDDING_MODEL")
	if generationModel == "" && embeddingModel == "" {
		t.Skip("set a POWERCONTEXT_REAL_SMOKE_*_MODEL variable to call a real provider")
	}

	client := &http.Client{
		Timeout: 75 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	factory, err := NewFactory(MilestoneB, ProcessEnvironment, client)
	if err != nil {
		t.Fatal(err)
	}
	if generationModel != "" {
		t.Run("generation", func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
			defer cancel()
			model, buildErr := factory.TextModel(generationModel)
			if buildErr != nil {
				t.Fatal(buildErr)
			}
			codec, buildErr := inference.NewJSONCodec[map[string]string, realSmokeOutput](
				[]byte(`{"type":"object","properties":{"ok":{"type":"boolean"}},"required":["ok"],"additionalProperties":false}`),
				nil,
				decodeRealSmokeOutput,
			)
			if buildErr != nil {
				t.Fatal(buildErr)
			}
			limits, _ := inference.NewLimits(75*time.Second, 1)
			settings, _ := inference.NewGenerationSettings(nil, nil)
			generator, buildErr := inference.NewPromptedGenerator(
				model,
				"Return an object whose ok field is true.",
				codec,
				&limits,
				settings,
			)
			if buildErr != nil {
				t.Fatal(buildErr)
			}
			result, callErr := generator.Generate(ctx, map[string]string{"request": "provider-smoke"})
			if callErr != nil {
				t.Fatal(callErr)
			}
			if !result.Output.OK || result.Usage.Requests != 1 {
				t.Fatal("real generation provider returned an invalid smoke result")
			}
		})
	}

	if embeddingModel != "" {
		t.Run("embedding", func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
			defer cancel()
			transport, buildErr := factory.EmbeddingTransport(embeddingModel)
			if buildErr != nil {
				t.Fatal(buildErr)
			}
			result, callErr := transport.Embed(
				ctx,
				[]string{"PowerContext provider smoke test."},
				inference.EmbeddingDocument,
			)
			if callErr != nil {
				t.Fatal(callErr)
			}
			vectors := result.Embeddings()
			if len(vectors) != 1 || len(vectors[0]) == 0 {
				t.Fatal("real embedding provider returned an invalid vector count")
			}
			for _, value := range vectors[0] {
				if math.IsNaN(value) || math.IsInf(value, 0) {
					t.Fatal("real embedding provider returned a non-finite vector")
				}
			}
		})
	}
}

type realSmokeOutput struct {
	OK bool `json:"ok"`
}

func decodeRealSmokeOutput(value []byte) (realSmokeOutput, error) {
	var output realSmokeOutput
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&output); err != nil {
		return realSmokeOutput{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return realSmokeOutput{}, errors.New("trailing JSON data")
	}
	return output, nil
}
