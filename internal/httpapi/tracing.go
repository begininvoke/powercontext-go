package httpapi

import (
	"context"
	"errors"

	v1 "github.com/thunguo/powercontext-go/api/v1"
	requesttrace "github.com/thunguo/powercontext-go/internal/observability/tracing"
	"go.opentelemetry.io/otel/trace"
)

// TraceApplication records the decoded application operation as a child of
// ogen's transport span. Decode/security failures therefore remain transport
// failures and never masquerade as entered application work.
func TraceApplication(provider trace.TracerProvider) Middleware {
	return func(request Request, next Next) (Response, error) {
		if request.OperationID == "get_liveness" || request.OperationID == "get_readiness" {
			return next(request)
		}
		ctx, span := requesttrace.StartOperation(
			request.Context,
			provider,
			"powercontext "+request.OperationID,
			request.OperationID,
			"application",
			trace.SpanKindInternal,
			"",
		)
		request.SetContext(ctx)
		response, err := next(request)
		outcome := "success"
		if err != nil {
			outcome = "failure"
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			outcome = "cancelled"
		}
		if value, ok := response.Type.(*v1.FlushMemoryResponseHeaders); ok && value.Response.Status == v1.FlushStatusIdle {
			outcome = "noop"
		}
		span.Finish(outcome, err)
		return response, err
	}
}
