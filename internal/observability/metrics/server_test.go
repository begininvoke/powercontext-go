package metrics

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ogen-go/ogen/middleware"
)

func TestMetricsFailureDoesNotChangeApplicationBehavior(t *testing.T) {
	// A zero-value Server deliberately makes every Prometheus collector panic.
	// The observability boundary must isolate those failures from application
	// behavior just as it would isolate a faulty collector at runtime.
	broken := &Server{}
	want := &struct{ value string }{value: "application response"}
	called := false

	response, err := broken.HTTPMiddleware(middleware.Request{
		Context: context.Background(), OperationID: "get_capabilities",
	}, func(request middleware.Request) (middleware.Response, error) {
		called = true
		if request.OperationID != "get_capabilities" {
			t.Fatalf("operation ID = %q", request.OperationID)
		}
		return middleware.Response{Type: want}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !called || response.Type != want {
		t.Fatalf("application result = %#v, called = %t", response.Type, called)
	}

	// Readiness observation uses the same failure-isolation boundary.
	broken.SetReady(true)
	broken.SetRuntimeScopes(3, 1)
}

func TestRuntimeScopeMetricsHaveOnlyBoundedStateLabels(t *testing.T) {
	t.Parallel()
	server, err := New()
	if err != nil {
		t.Fatal(err)
	}
	server.SetRuntimeScopes(3, 1)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest("GET", "/metrics", nil))
	text := response.Body.String()
	for _, want := range []string{
		`powercontext_server_runtime_scopes{state="active"} 1`,
		`powercontext_server_runtime_scopes{state="cached"} 3`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("metrics do not contain %q:\n%s", want, text)
		}
	}
}
