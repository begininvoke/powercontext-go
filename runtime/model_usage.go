package runtime

import (
	"context"

	"github.com/thunguo/powercontext-go/inference"
	"github.com/thunguo/powercontext-go/stats"
)

// ModelUsageRecorder is the best-effort application-side sink used by
// inference decorators. Implementations own persistence failure handling so a
// statistics outage can never turn a successful model call into a failed
// business operation.
type ModelUsageRecorder interface {
	RecordModelUsage(
		context.Context,
		string,
		stats.ModelPurpose,
		stats.ModelOperation,
		inference.Usage,
	)
}

type ModelUsageRecorderFunc func(
	context.Context,
	string,
	stats.ModelPurpose,
	stats.ModelOperation,
	inference.Usage,
)

func (f ModelUsageRecorderFunc) RecordModelUsage(
	ctx context.Context,
	scopeID string,
	purpose stats.ModelPurpose,
	operation stats.ModelOperation,
	usage inference.Usage,
) {
	if f != nil {
		f(ctx, scopeID, purpose, operation, usage)
	}
}

type modelUsageContextKey struct{}

type modelUsageBinding struct {
	recorder          ModelUsageRecorder
	scopeID           string
	generationPurpose stats.ModelPurpose
	embeddingPurpose  stats.ModelPurpose
}

func (r *Runtime) withModelUsage(
	ctx context.Context,
	scopeID string,
	generationPurpose stats.ModelPurpose,
	embeddingPurpose stats.ModelPurpose,
) context.Context {
	if r == nil || r.modelUsage == nil {
		return ctx
	}
	return context.WithValue(ctx, modelUsageContextKey{}, modelUsageBinding{
		recorder: r.modelUsage, scopeID: scopeID,
		generationPurpose: generationPurpose, embeddingPurpose: embeddingPurpose,
	})
}

func reportModelUsage(ctx context.Context, operation stats.ModelOperation, usage inference.Usage) {
	binding, ok := ctx.Value(modelUsageContextKey{}).(modelUsageBinding)
	if !ok || binding.recorder == nil {
		return
	}
	purpose := binding.embeddingPurpose
	if operation == stats.Generation {
		purpose = binding.generationPurpose
	}
	if purpose == "" {
		return
	}
	binding.recorder.RecordModelUsage(ctx, binding.scopeID, purpose, operation, usage)
}

type usageReportingStructuredGenerator[I, O any] struct {
	delegate inference.StructuredGenerator[I, O]
}

// ReportStructuredUsage decorates a structured generator without changing its
// provider contract. Only successful results are reported, and attribution is
// resolved from the admitted Runtime operation's context.
func ReportStructuredUsage[I, O any](
	delegate inference.StructuredGenerator[I, O],
) inference.StructuredGenerator[I, O] {
	if delegate == nil {
		return nil
	}
	return usageReportingStructuredGenerator[I, O]{delegate: delegate}
}

func (g usageReportingStructuredGenerator[I, O]) Generate(
	ctx context.Context,
	input I,
) (inference.GenerationResult[O], error) {
	result, err := g.delegate.Generate(ctx, input)
	if err == nil {
		reportModelUsage(ctx, stats.Generation, result.Usage)
	}
	return result, err
}

type usageReportingEmbeddingModel struct {
	delegate inference.EmbeddingModel
}

// ReportEmbeddingUsage decorates an embedding model while preserving its
// immutable profile. Empty and failed calls retain the delegate's behavior.
func ReportEmbeddingUsage(delegate inference.EmbeddingModel) inference.EmbeddingModel {
	if delegate == nil {
		return nil
	}
	return usageReportingEmbeddingModel{delegate: delegate}
}

func (m usageReportingEmbeddingModel) Profile() inference.EmbeddingProfile {
	return m.delegate.Profile()
}

func (m usageReportingEmbeddingModel) Embed(
	ctx context.Context,
	texts []string,
) (inference.EmbeddingResult, error) {
	result, err := m.delegate.Embed(ctx, texts)
	if err == nil {
		reportModelUsage(ctx, stats.Embedding, result.Usage)
	}
	return result, err
}
