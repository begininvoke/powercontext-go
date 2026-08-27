package server

import (
	"context"
	"strings"
	"testing"

	pcruntime "github.com/ob-labs/powercontext-go/internal/runtime"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestRuntimeStageSpansInheritApplicationContextWithoutRawScope(t *testing.T) {
	t.Parallel()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(recorder),
	)
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	runtime, err := pcruntime.NewConfigured(pcruntime.RuntimeOptions{
		Tracing: newRuntimeStageTracing(provider),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, parent := provider.Tracer("test").Start(t.Context(), "powercontext remember_memory")
	if err := runtime.ScopedWrite(ctx, "project:private-scope", func(context.Context, string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	parent.End()
	spans := recorder.Ended()
	application := endedSpan(t, spans, "powercontext remember_memory")
	for _, name := range []string{"scope.context", "scope.lock"} {
		stage := endedSpan(t, spans, name)
		if stage.Parent().SpanID() != application.SpanContext().SpanID() {
			t.Fatalf("%s parent = %s, want %s", name, stage.Parent().SpanID(), application.SpanContext().SpanID())
		}
		serialized := strings.Builder{}
		for _, attribute := range stage.Attributes() {
			serialized.WriteString(attribute.Value.Emit())
		}
		if strings.Contains(serialized.String(), "private-scope") {
			t.Fatalf("%s leaked raw Scope: %s", name, serialized.String())
		}
	}
}
