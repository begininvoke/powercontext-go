package endpoint

import (
	"context"

	v1 "github.com/thunguo/powercontext-go/api/v1"
	requesttrace "github.com/thunguo/powercontext-go/internal/observability/tracing"
)

func requestID(ctx context.Context) v1.OptString {
	value, ok := requesttrace.RequestID(ctx)
	if !ok {
		return v1.OptString{}
	}
	return v1.NewOptString(value)
}
