package server

import (
	"context"

	requesttrace "github.com/ob-labs/powercontext-go/internal/observability/tracing"
	pcruntime "github.com/ob-labs/powercontext-go/internal/runtime"
	"go.opentelemetry.io/otel/trace"
)

type runtimeStageTracing struct{ provider trace.TracerProvider }

func newRuntimeStageTracing(provider trace.TracerProvider) pcruntime.StageTracing {
	return runtimeStageTracing{provider: provider}
}

func (t runtimeStageTracing) StartStage(
	ctx context.Context,
	name string,
	attributes map[string]pcruntime.TraceAttribute,
) (context.Context, pcruntime.StageSpan) {
	spanContext, operation := requesttrace.StartOperation(
		ctx, t.provider, name, name, "stage", trace.SpanKindInternal, "",
	)
	operation.SetAttributes(attributes)
	return spanContext, operation
}

var _ pcruntime.StageTracing = runtimeStageTracing{}
