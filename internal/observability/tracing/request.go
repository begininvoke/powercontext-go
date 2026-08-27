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
