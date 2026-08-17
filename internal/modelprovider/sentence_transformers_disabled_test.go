//go:build !local_embeddings || !cgo || (!ORT && !ALL)

package modelprovider

import (
	"errors"
	"strings"
	"testing"

	"github.com/thunguo/powercontext-go/inference"
)

func TestSentenceTransformersRequiresFullBuild(t *testing.T) {
	factory, err := NewFactory(MilestoneB, testEnvironment{}.lookup, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = factory.EmbeddingTransport("sentence-transformers:sentence-transformers/all-MiniLM-L6-v2")
	var configuration *inference.ConfigurationError
	if !errors.As(err, &configuration) || configuration.Code() != "embedding-model" {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), "local_embeddings") || strings.Contains(err.Error(), "ORT") {
		t.Fatalf("public error exposed build details: %v", err)
	}
}
