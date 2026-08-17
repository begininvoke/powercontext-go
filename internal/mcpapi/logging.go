package mcpapi

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	serverlogging "github.com/thunguo/powercontext-go/internal/observability/logging"
)

// AccessLogMiddleware observes logical MCP protocol requests. It must be
// installed inside the tracing middleware so the record shares the MCP
// transport span and request ID; Streamable HTTP frames are logged nowhere.
func AccessLogMiddleware(logger *slog.Logger) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
			started := time.Now()
			result, err := next(ctx, method, request)
			outcome := "success"
			if err != nil {
				outcome = "failure"
			} else if call, ok := result.(*mcp.CallToolResult); ok && call.IsError {
				outcome = "failure"
			}
			if errors.Is(context.Cause(ctx), context.Canceled) || errors.Is(context.Cause(ctx), context.DeadlineExceeded) {
				outcome = "cancelled"
			}
			serverlogging.LogTransportCompletion(ctx, logger, serverlogging.TransportObservation{
				Operation: "mcp." + mcpMethodName(method), Outcome: outcome, Transport: "mcp", Duration: time.Since(started),
			})
			return result, err
		}
	}
}

func mcpMethodName(method string) string {
	value := strings.ReplaceAll(strings.TrimPrefix(method, "/"), "/", ".")
	if value == "" {
		return "unknown"
	}
	return value
}
