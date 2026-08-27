package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDashboardRoutesAndHeaders(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	if err := Mount(mux, Options{DashboardEnabled: true, AuthenticationRequired: true, Scopes: []Scope{
		{ScopeID: "person:psiace", DisplayName: "PsiACE"},
		{ScopeID: "project:powercontext", DisplayName: "PowerContext"},
	}}); err != nil {
		t.Fatal(err)
	}

	home := request(mux, http.MethodGet, "/")
	if home.Code != http.StatusOK {
		t.Fatalf("home = %d %s", home.Code, home.Body.String())
	}
	if home.Header().Get("Cache-Control") != "no-store" || home.Header().Get("Content-Security-Policy") != pageCSP {
		t.Fatalf("page headers = %#v", home.Header())
	}
	for _, fragment := range []string{
		`data-server-session="missing"`, `id="auth-shell"`, `id="page-status" hidden`,
		`class="server-content" id="dashboard"`, `dashboard.js?v=default-startup-locale-v1`,
		`href="/skills"`, `href="/reviews"`,
	} {
		if !strings.Contains(home.Body.String(), fragment) {
			t.Errorf("home does not contain %q", fragment)
		}
	}
	if response := request(mux, http.MethodGet, "/dashboard"); response.Code != http.StatusNotFound {
		t.Fatalf("legacy dashboard alias = %d", response.Code)
	}

	scopesResponse := request(mux, http.MethodGet, "/dashboard/scopes")
	if scopesResponse.Code != http.StatusOK || scopesResponse.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("scopes = %d %#v", scopesResponse.Code, scopesResponse.Header())
	}
	var scopes []Scope
	if err := json.Unmarshal(scopesResponse.Body.Bytes(), &scopes); err != nil {
		t.Fatal(err)
	}
	if len(scopes) != 2 || scopes[0].ScopeID != "person:psiace" || scopes[1].DisplayName != "PowerContext" {
		t.Fatalf("scopes = %#v", scopes)
	}

	asset := request(mux, http.MethodGet, "/static/auth.js")
	if asset.Code != http.StatusOK || !strings.Contains(asset.Body.String(), "powercontext.server.token") {
		t.Fatalf("asset = %d %s", asset.Code, asset.Body.String())
	}
	if response := request(mux, http.MethodPost, "/"); response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST / = %d", response.Code)
	}
}

func TestHandoffReportPageFeatureGate(t *testing.T) {
	t.Parallel()

	for _, enabled := range []bool{false, true} {
		mux := http.NewServeMux()
		if err := Mount(mux, Options{
			DashboardEnabled:     true,
			Scopes:               []Scope{{ScopeID: "project:powercontext", DisplayName: "PowerContext"}},
			HandoffReportEnabled: enabled,
		}); err != nil {
			t.Fatal(err)
		}
		response := request(mux, http.MethodGet, "/handoff-reports")
		if !enabled {
			if response.Code != http.StatusNotFound {
				t.Fatalf("disabled page = %d", response.Code)
			}
			continue
		}
		if response.Code != http.StatusOK {
			t.Fatalf("enabled page = %d %s", response.Code, response.Body.String())
		}
		for _, fragment := range []string{
			`class="server-content" id="handoff-report"`, `data-period-mode="day"`,
			`data-period-mode="week"`, `data-period-mode="month"`, `id="project-search"`,
			`<section class="report-overview"`, `handoff-report.js?v=scope-report-v1`,
		} {
			if !strings.Contains(response.Body.String(), fragment) {
				t.Errorf("handoff page does not contain %q", fragment)
			}
		}
	}
}

func TestMountAcceptsEnabledDashboardWithoutScopes(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	if err := Mount(mux, Options{DashboardEnabled: true}); err != nil {
		t.Fatal(err)
	}
	response := request(mux, http.MethodGet, "/dashboard/scopes")
	if response.Code != http.StatusOK || response.Body.String() != "[]" {
		t.Fatalf("empty scopes = %d %s", response.Code, response.Body.String())
	}
}

func TestMountSupportsReportOnlyWebUI(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	if err := Mount(mux, Options{HandoffReportEnabled: true}); err != nil {
		t.Fatal(err)
	}
	if response := request(mux, http.MethodGet, "/"); response.Code != http.StatusNotFound {
		t.Fatalf("report-only root = %d", response.Code)
	}
	if response := request(mux, http.MethodGet, "/handoff-reports"); response.Code != http.StatusOK {
		t.Fatalf("report-only page = %d %s", response.Code, response.Body.String())
	}
}

func TestDisabledDashboardLeavesProductPagesAbsentAndHealthAvailable(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	if err := Mount(mux, Options{}); err == nil {
		t.Fatal("disabled web UI unexpectedly mounted")
	}
	mux.HandleFunc("GET /health/live", func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
	})
	for _, path := range []string{"/", "/skills", "/reviews"} {
		if response := request(mux, http.MethodGet, path); response.Code != http.StatusNotFound {
			t.Fatalf("GET %s = %d, want 404", path, response.Code)
		}
	}
	if response := request(mux, http.MethodGet, "/health/live"); response.Code != http.StatusOK {
		t.Fatalf("health = %d", response.Code)
	}
}

func TestHandoffReportPageContainsDataFreePreviewTemplate(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	if err := Mount(mux, Options{HandoffReportEnabled: true}); err != nil {
		t.Fatal(err)
	}
	response := request(mux, http.MethodGet, "/handoff-reports")
	if response.Code != http.StatusOK {
		t.Fatalf("page = %d %s", response.Code, response.Body.String())
	}
	page := response.Body.String()
	_, afterPreview, found := strings.Cut(page, `id="handoff-report-preview"`)
	if !found {
		t.Fatal("preview shell is absent")
	}
	preview, _, found := strings.Cut(afterPreview, `id="handoff-report"`)
	if !found {
		t.Fatal("live report shell is absent after preview")
	}
	for _, fragment := range []string{
		`aria-describedby="preview-notice"`, `hidden`, `id="preview-retry"`,
		`role="status" aria-live="polite"`, `data-preview-placeholder`,
	} {
		if !strings.Contains(preview, fragment) {
			t.Errorf("preview does not contain %q", fragment)
		}
	}
	for _, forbidden := range []string{">0<", "<input", "<select", `id="download-report"`} {
		if strings.Contains(preview, forbidden) {
			t.Errorf("preview contains live or interactive value %q", forbidden)
		}
	}
	for remainder := preview; ; {
		_, after, ok := strings.Cut(remainder, "data-preview-placeholder")
		if !ok {
			break
		}
		closeTag := strings.IndexByte(after, '>')
		if closeTag < 0 {
			t.Fatalf("preview placeholder tag is incomplete: %q", after)
		}
		openTag := strings.IndexByte(after[closeTag+1:], '<')
		if openTag < 0 || after[closeTag+1:closeTag+1+openTag] != "—" {
			t.Fatalf("preview placeholder is not the data-free em dash: %q", after)
		}
		remainder = after[closeTag+1+openTag:]
	}
}

func request(handler http.Handler, method, target string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(method, target, nil))
	return response
}
