package tracing

import (
	"testing"
)

func TestDisabledTracingKeepsValidNonRecordingContext(t *testing.T) {
	server, err := Configure(t.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close(t.Context()) })
	_, span := server.Provider().Tracer("test").Start(t.Context(), "test")
	defer span.End()
	if !span.SpanContext().IsValid() || span.IsRecording() {
		t.Fatalf("span context = %#v, recording = %t", span.SpanContext(), span.IsRecording())
	}
}

func TestEnabledTracingHasCompiledOTLPHTTPExporter(t *testing.T) {
	// Python needs an optional runtime extra before enabled tracing can be
	// configured. The standard Go artifact instead compiles the exporter in;
	// constructing it must succeed without loading any optional component.
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:4318")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	server, err := Configure(t.Context(), true)
	if err != nil {
		t.Fatal(err)
	}
	if server.provider == nil {
		t.Fatal("enabled tracing did not construct a provider")
	}
	if err := server.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestOperationNameConversionIsDeterministic(t *testing.T) {
	for input, want := range map[string]string{
		"GetCapabilities":             "get_capabilities",
		"GetHandoffReportWorkspace":   "get_handoff_report_workspace",
		"ListHandoffReportActivities": "list_handoff_report_activities",
	} {
		if got := camelToSnake(input); got != want {
			t.Fatalf("camelToSnake(%q) = %q, want %q", input, got, want)
		}
	}
}
