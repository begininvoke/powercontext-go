//go:build !local_embeddings || !cgo || (!ORT && !ALL)

package modelprovider

import "github.com/ob-labs/powercontext-go/inference"

func newSentenceTransformersTransport(Route, EnvLookup) (inference.EmbeddingTransport, error) {
	return nil, inference.NewConfigurationError(
		"embedding-model",
		"sentence-transformers requires CGO and the local_embeddings,ORT build tags",
	)
}
