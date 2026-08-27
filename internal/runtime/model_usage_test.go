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

package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/ob-labs/powercontext-go/inference"
	"github.com/ob-labs/powercontext-go/internal/stats"
)

type recordedModelUsage struct {
	scopeID   string
	purpose   stats.ModelPurpose
	operation stats.ModelOperation
	usage     inference.Usage
}

type modelUsageRecorderStub struct{ values []recordedModelUsage }

func (r *modelUsageRecorderStub) RecordModelUsage(
	_ context.Context,
	scopeID string,
	purpose stats.ModelPurpose,
	operation stats.ModelOperation,
	usage inference.Usage,
) {
	r.values = append(r.values, recordedModelUsage{
		scopeID: scopeID, purpose: purpose, operation: operation, usage: usage,
	})
}

type structuredGeneratorFunc[I, O any] func(context.Context, I) (inference.GenerationResult[O], error)

func (f structuredGeneratorFunc[I, O]) Generate(
	ctx context.Context,
	input I,
) (inference.GenerationResult[O], error) {
	return f(ctx, input)
}

func TestUsageReportingGeneratorUsesScopedPurposeAndSkipsFailures(t *testing.T) {
	recorder := &modelUsageRecorderStub{}
	lifecycle := NewWithModelUsageRecorder(recorder)
	inputTokens, outputTokens := int64(12), int64(4)
	wantUsage := inference.Usage{Requests: 2, InputTokens: &inputTokens, OutputTokens: &outputTokens}
	generator := ReportStructuredUsage(structuredGeneratorFunc[string, string](func(
		_ context.Context,
		input string,
	) (inference.GenerationResult[string], error) {
		if input == "fail" {
			return inference.GenerationResult[string]{}, errors.New("provider failed")
		}
		return inference.GenerationResult[string]{Output: input, Usage: wantUsage}, nil
	}))

	err := lifecycle.ScopedRead(t.Context(), "scope-usage", func(ctx context.Context, scope string) error {
		ctx = lifecycle.withModelUsage(ctx, scope, stats.ExperienceGeneration, "")
		if _, err := generator.Generate(ctx, "ok"); err != nil {
			return err
		}
		_, err := generator.Generate(ctx, "fail")
		return err
	})
	if err == nil {
		t.Fatal("failing delegate returned success")
	}
	if len(recorder.values) != 1 {
		t.Fatalf("recorded usage = %#v, want one successful call", recorder.values)
	}
	got := recorder.values[0]
	if got.scopeID != "scope-usage" || got.purpose != stats.ExperienceGeneration ||
		got.operation != stats.Generation || got.usage.Requests != 2 {
		t.Fatalf("recorded usage = %#v", got)
	}
}

type embeddingModelStub struct{ usage inference.Usage }

func (m embeddingModelStub) Profile() inference.EmbeddingProfile { return embeddingProfileStub{} }
func (m embeddingModelStub) Embed(context.Context, []string) (inference.EmbeddingResult, error) {
	return inference.EmbeddingResult{Vectors: [][]float64{{1}}, Usage: m.usage}, nil
}

type embeddingProfileStub struct{}

func (embeddingProfileStub) ID() string                { return "test" }
func (embeddingProfileStub) ModelName() string         { return "test" }
func (embeddingProfileStub) DimensionCount() int       { return 1 }
func (embeddingProfileStub) NormalizationMode() string { return "none" }

func TestUsageReportingEmbeddingPreservesProfileAndAttribution(t *testing.T) {
	recorder := &modelUsageRecorderStub{}
	lifecycle := NewWithModelUsageRecorder(recorder)
	inputTokens := int64(3)
	model := ReportEmbeddingUsage(embeddingModelStub{
		usage: inference.Usage{Requests: 1, InputTokens: &inputTokens},
	})
	if model.Profile().ID() != "test" {
		t.Fatalf("profile ID = %q", model.Profile().ID())
	}
	err := lifecycle.ScopedRead(t.Context(), "scope-embedding", func(ctx context.Context, scope string) error {
		ctx = lifecycle.withModelUsage(ctx, scope, "", stats.MemoryRecall)
		_, err := model.Embed(ctx, []string{"query"})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(recorder.values) != 1 || recorder.values[0].purpose != stats.MemoryRecall ||
		recorder.values[0].operation != stats.Embedding {
		t.Fatalf("recorded usage = %#v", recorder.values)
	}
}
