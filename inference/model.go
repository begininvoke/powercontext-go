// Copyright (c) 2026 OceanBase.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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
