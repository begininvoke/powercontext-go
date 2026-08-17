package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	v1 "github.com/thunguo/powercontext-go/api/v1"
	"go.opentelemetry.io/otel/trace"
)

var requestIDPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

func TestWrapOwnsRequestID(t *testing.T) {
	t.Parallel()

	var observed string
	handler, err := Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed, _ = RequestID(r.Context())
		w.Header().Set("X-Request-ID", "application-value")
		w.WriteHeader(http.StatusNoContent)
	}), Options{})
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/v1/capabilities", nil)
	request.Header.Set(RequestIDHeader, "caller-request-id")
	request.Header.Set("X-Request-ID", "legacy-request-id")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	got := response.Header().Get(RequestIDHeader)
	if !requestIDPattern.MatchString(got) || got == "caller-request-id" {
		t.Fatalf("unexpected request ID %q", got)
	}
	if got != observed {
		t.Fatalf("response request ID %q differs from context %q", got, observed)
	}
	if got := response.Header().Get("X-Request-ID"); got != "" {
		t.Fatalf("legacy request ID leaked into response: %q", got)
	}
}

func TestWrapBearerPolicy(t *testing.T) {
	t.Parallel()

	called := 0
	handler, err := Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called++
		w.WriteHeader(http.StatusNoContent)
	}), Options{BearerToken: "server-secret", HandoffReportRoutes: true})
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name          string
		path          string
		authorization string
		wantStatus    int
	}{
		{name: "missing", path: "/v1/capabilities", wantStatus: http.StatusUnauthorized},
		{name: "wrong", path: "/v1/capabilities", authorization: "Bearer wrong", wantStatus: http.StatusUnauthorized},
		{name: "wrong scheme", path: "/v1/capabilities", authorization: "Basic server-secret", wantStatus: http.StatusUnauthorized},
		{name: "spaces in credential", path: "/v1/capabilities", authorization: "Bearer server-secret extra", wantStatus: http.StatusUnauthorized},
		{name: "accepted", path: "/v1/capabilities", authorization: "bEaReR server-secret", wantStatus: http.StatusNoContent},
		{name: "live is public", path: "/health/live", wantStatus: http.StatusNoContent},
		{name: "ready is public", path: "/health/ready", wantStatus: http.StatusNoContent},
		{name: "dashboard shell is public", path: "/", wantStatus: http.StatusNoContent},
		{name: "report shell is public", path: "/handoff-reports", wantStatus: http.StatusNoContent},
		{name: "static is public", path: "/static/dashboard.js", wantStatus: http.StatusNoContent},
		{name: "metrics is protected", path: "/metrics", wantStatus: http.StatusUnauthorized},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			if test.authorization != "" {
				request.Header.Set("Authorization", test.authorization)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d: %s", response.Code, test.wantStatus, response.Body.String())
			}
			if !requestIDPattern.MatchString(response.Header().Get(RequestIDHeader)) {
				t.Fatalf("missing server request ID: %q", response.Header().Get(RequestIDHeader))
			}
			if test.wantStatus == http.StatusUnauthorized {
				assertUnauthorized(t, response)
			}
		})
	}

	if called != 6 {
		t.Fatalf("application called %d times, want 6", called)
	}
}

func TestWrapRejectsDisabledHandoffReportRoutesBeforeApplication(t *testing.T) {
	t.Parallel()

	called := false
	handler, err := Wrap(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}), Options{})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/handoff-reports/projects/list", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", response.Code)
	}
	if called {
		t.Fatal("disabled route reached application")
	}
}

func TestOgenIngressSpanOwnsRequestIDIncludingDecodeFailures(t *testing.T) {
	t.Parallel()

	spanID := trace.SpanID{0x10, 0x32, 0x54, 0x76, 0x98, 0xba, 0xdc, 0xfe}
	handler := &healthHandler{}
	security, err := NewSecurity("")
	if err != nil {
		t.Fatal(err)
	}
	generated, err := v1.NewServer(
		handler,
		security,
		v1.WithTracerProvider(TracerProvider(fixedTracerProvider{spanID: spanID})),
		v1.WithMiddleware(BindSpanRequestID),
		v1.WithErrorHandler(ErrorHandler(nil)),
	)
	if err != nil {
		t.Fatal(err)
	}
	server, err := Wrap(generated, Options{HandoffReportRoutes: true})
	if err != nil {
		t.Fatal(err)
	}

	live := httptest.NewRecorder()
	server.ServeHTTP(live, httptest.NewRequest(http.MethodGet, "/health/live", nil))
	if live.Code != http.StatusOK || live.Header().Get(RequestIDHeader) != spanID.String() {
		t.Fatalf("live response = (%d, %q): %s", live.Code, live.Header().Get(RequestIDHeader), live.Body.String())
	}
	if handler.requestID != spanID.String() {
		t.Fatalf("handler request ID = %q, want %q", handler.requestID, spanID.String())
	}

	invalid := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/memory/search", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	server.ServeHTTP(invalid, request)
	if invalid.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid status = %d: %s", invalid.Code, invalid.Body.String())
	}
	if invalid.Header().Get(RequestIDHeader) != spanID.String() {
		t.Fatalf("invalid request ID = %q, want %q", invalid.Header().Get(RequestIDHeader), spanID.String())
	}
	var envelope errorEnvelope
	if err := json.Unmarshal(invalid.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error.Code != "invalid_request" {
		t.Fatalf("error = %#v", envelope.Error)
	}
}

func TestWrapRejectsUnsafeConfiguration(t *testing.T) {
	t.Parallel()
	if _, err := Wrap(nil, Options{}); err == nil {
		t.Fatal("expected nil handler error")
	}
	if _, err := Wrap(http.NotFoundHandler(), Options{BearerToken: "bad\ntoken"}); err == nil {
		t.Fatal("expected unsafe token error")
	}
}

func assertUnauthorized(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if response.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Fatalf("challenge = %q", response.Header().Get("WWW-Authenticate"))
	}
	var envelope errorEnvelope
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error.Code != "unauthorized" || envelope.Error.Message != "A valid bearer token is required." || envelope.Error.Details != nil {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

type healthHandler struct {
	v1.UnimplementedHandler
	requestID string
}

func (h *healthHandler) GetLiveness(ctx context.Context) (*v1.HealthResponseHeaders, error) {
	h.requestID, _ = RequestID(ctx)
	return &v1.HealthResponseHeaders{Response: v1.HealthResponse{Status: "ok"}}, nil
}

type fixedTracerProvider struct {
	trace.TracerProvider
	spanID trace.SpanID
}

func (p fixedTracerProvider) Tracer(string, ...trace.TracerOption) trace.Tracer {
	return fixedTracer{spanID: p.spanID}
}

type fixedTracer struct {
	trace.Tracer
	spanID trace.SpanID
}

func (t fixedTracer) Start(ctx context.Context, _ string, _ ...trace.SpanStartOption) (context.Context, trace.Span) {
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{1},
		SpanID:  t.spanID,
	})
	ctx = trace.ContextWithSpanContext(ctx, spanContext)
	return ctx, trace.SpanFromContext(ctx)
}
