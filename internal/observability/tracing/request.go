package tracing

import (
	"context"
	"sync/atomic"

	"go.opentelemetry.io/otel/trace"
)

type requestStateKey struct{}

type requestState struct {
	id   atomic.Value // string
	span atomic.Value // trace.SpanContext
}

// WithRequestID creates mutable per-request correlation state. Mutation is
// intentionally limited to SetRequestID so transports can replace a fallback
// ID with the final ingress span ID without changing context identity.
func WithRequestID(ctx context.Context, id string) context.Context {
	state := new(requestState)
	state.id.Store(id)
	state.span.Store(trace.SpanContext{})
	return context.WithValue(ctx, requestStateKey{}, state)
}

// RequestSpanContext returns the ingress transport span captured for an outer
// access logger. The request context itself cannot be replaced after ogen or
// the MCP SDK starts its child context, so correlation lives beside request ID.
func RequestSpanContext(ctx context.Context) (trace.SpanContext, bool) {
	state, ok := ctx.Value(requestStateKey{}).(*requestState)
	if !ok {
		return trace.SpanContext{}, false
	}
	span, ok := state.span.Load().(trace.SpanContext)
	return span, ok && span.IsValid()
}

func setRequestSpanContext(ctx context.Context, span trace.SpanContext) bool {
	state, ok := ctx.Value(requestStateKey{}).(*requestState)
	if !ok || !span.IsValid() {
		return false
	}
	state.span.Store(span)
	return true
}

func RequestID(ctx context.Context) (string, bool) {
	state, ok := ctx.Value(requestStateKey{}).(*requestState)
	if !ok {
		return "", false
	}
	return state.id.Load().(string), true
}

func SetRequestID(ctx context.Context, id string) bool {
	state, ok := ctx.Value(requestStateKey{}).(*requestState)
	if !ok || id == "" || id == "0000000000000000" {
		return false
	}
	state.id.Store(id)
	return true
}
