package inference

import "context"

// Usage contains portable successful provider usage fields.
type Usage struct {
	Requests     int
	InputTokens  *int64
	OutputTokens *int64
}

type GenerationResult[T any] struct {
	Output T
	Usage  Usage
}

type StructuredGenerator[I, O any] interface {
	Generate(context.Context, I) (GenerationResult[O], error)
}

type EmbeddingResult struct {
	Vectors [][]float64
	Usage   Usage
}

type EmbeddingProfile interface {
	ID() string
	ModelName() string
	DimensionCount() int
	NormalizationMode() string
}

type EmbeddingModel interface {
	Profile() EmbeddingProfile
	Embed(context.Context, []string) (EmbeddingResult, error)
}
