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
	if err := Mount(mux, Options{Scopes: []Scope{
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
		`class="server-content" id="dashboard"`, `dashboard.js?v=state-races`,
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
			`data-period-mode="week"`, `data-period-mode="month"`, `id="project-select"`,
			`<section class="report-overview"`, `handoff-report.js?v=state-races`,
		} {
			if !strings.Contains(response.Body.String(), fragment) {
				t.Errorf("handoff page does not contain %q", fragment)
			}
		}
	}
}

func TestMountRejectsMissingScopes(t *testing.T) {
	t.Parallel()
	if err := Mount(http.NewServeMux(), Options{}); err == nil {
		t.Fatal("Mount() unexpectedly accepted no scopes")
	}
}

func request(handler http.Handler, method, target string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(method, target, nil))
	return response
}
