package httpapi

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	serverlogging "github.com/thunguo/powercontext-go/internal/observability/logging"
	requesttrace "github.com/thunguo/powercontext-go/internal/observability/tracing"
	"go.opentelemetry.io/otel/propagation"
)

var publicPaths = map[string]struct{}{
	"/":                {},
	"/handoff-reports": {},
	"/health/live":     {},
	"/health/ready":    {},
}

// Options configures transport-only behavior around the generated server.
type Options struct {
	BearerToken         string
	HandoffReportRoutes bool
	Access              *AccessLogOptions
}

type AccessLogOptions struct {
	Logger           *slog.Logger
	ResolveOperation func(*http.Request) string
	Skip             func(*http.Request) bool
}

// Wrap installs server-owned request IDs, optional static authentication and
// the feature gate for Handoff Report operations. It must wrap the complete
// HTTP mux so non-OpenAPI surfaces such as metrics and the Dashboard observe
// the same policy.
func Wrap(next http.Handler, options Options) (http.Handler, error) {
	if next == nil {
		return nil, errors.New("httpapi: handler must not be nil")
	}
	if strings.ContainsAny(options.BearerToken, "\r\n") {
		return nil, errors.New("httpapi: bearer token must not contain line breaks")
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parent := requesttrace.ExtractTraceContext(r.Context(), propagation.HeaderCarrier(r.Header))
		ctx := r.WithContext(requesttrace.WithRequestID(parent, randomRequestID()))
		writer := &requestIDWriter{ResponseWriter: w, ctx: ctx.Context()}
		access := options.Access
		logAccess := access != nil && access.Logger != nil && (access.Skip == nil || !access.Skip(ctx))
		started := time.Now()
		operation := "unmatched"
		if logAccess && access.ResolveOperation != nil {
			if resolved := access.ResolveOperation(ctx); resolved != "" {
				operation = resolved
			}
		}
		if logAccess {
			defer func() {
				if recovered := recover(); recovered != nil {
					logHTTPCompletion(ctx.Context(), access.Logger, writer, operation, time.Since(started), true)
					panic(recovered)
				}
				logHTTPCompletion(ctx.Context(), access.Logger, writer, operation, time.Since(started), false)
			}()
		}

		if !options.HandoffReportRoutes && strings.HasPrefix(r.URL.Path, "/v1/handoff-reports/") {
			http.NotFound(writer, ctx)
			return
		}
		if options.BearerToken != "" && !isPublicPath(r.URL.Path) && !validBearer(r.Header.Get("Authorization"), options.BearerToken) {
			writer.Header().Set("WWW-Authenticate", "Bearer")
			writeError(writer, http.StatusUnauthorized, Error{
				Code:    "unauthorized",
				Message: "A valid bearer token is required.",
			})
			return
		}

		next.ServeHTTP(writer, ctx)
	}), nil
}

func isPublicPath(path string) bool {
	if _, ok := publicPaths[path]; ok {
		return true
	}
	return strings.HasPrefix(path, "/static/")
}

func validBearer(header, want string) bool {
	scheme, credential, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "bearer") || credential == "" {
		return false
	}
	wantHash := sha256.Sum256([]byte(want))
	gotHash := sha256.Sum256([]byte(credential))
	return subtle.ConstantTimeCompare(gotHash[:], wantHash[:]) == 1
}

type requestIDWriter struct {
	http.ResponseWriter
	ctx         context.Context
	wroteHeader bool
	statusCode  int
}

func (w *requestIDWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *requestIDWriter) WriteHeader(statusCode int) {
	if w.wroteHeader {
		return
	}
	w.Header().Set(RequestIDHeader, mustRequestID(w.ctx))
	w.Header().Del("X-Request-ID")
	w.wroteHeader = true
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *requestIDWriter) Write(body []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}

func (w *requestIDWriter) ReadFrom(reader io.Reader) (int64, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if readerFrom, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		return readerFrom.ReadFrom(reader)
	}
	return io.Copy(struct{ io.Writer }{w.ResponseWriter}, reader)
}

func (w *requestIDWriter) Flush() {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *requestIDWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("httpapi: response writer does not support hijacking")
	}
	if !w.wroteHeader {
		w.Header().Set(RequestIDHeader, mustRequestID(w.ctx))
	}
	return hijacker.Hijack()
}

func mustRequestID(ctx context.Context) string {
	value, ok := requesttrace.RequestID(ctx)
	if !ok {
		return "0000000000000001"
	}
	return value
}

func (w *requestIDWriter) Push(target string, options *http.PushOptions) error {
	if pusher, ok := w.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, options)
	}
	return http.ErrNotSupported
}

func logHTTPCompletion(
	ctx context.Context,
	logger *slog.Logger,
	writer *requestIDWriter,
	operation string,
	duration time.Duration,
	panicked bool,
) {
	statusCode := writer.statusCode
	outcome := "success"
	if panicked {
		statusCode = http.StatusInternalServerError
		outcome = "failure"
	} else if context.Cause(ctx) != nil {
		statusCode = 0
		outcome = "cancelled"
	} else {
		if statusCode == 0 {
			statusCode = http.StatusOK
		}
		if statusCode >= 400 {
			outcome = "failure"
		}
	}
	serverlogging.LogTransportCompletion(ctx, logger, serverlogging.TransportObservation{
		Operation: operation, Outcome: outcome, Transport: "http", Duration: duration, StatusCode: statusCode,
	})
}

// Error is the stable wire-level error detail. Details is encoded as JSON null
// when absent, matching the frozen Python envelope.
type Error struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

type errorEnvelope struct {
	Error Error `json:"error"`
}

func writeError(w http.ResponseWriter, statusCode int, detail Error) {
	payload, err := json.Marshal(errorEnvelope{Error: detail})
	if err != nil {
		payload = []byte(`{"error":{"code":"internal_error","message":"The Server failed.","details":null}}`)
		statusCode = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_, _ = w.Write(payload)
}
