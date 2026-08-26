package webui

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"html/template"
	"io/fs"
	"net/http"
	"slices"
	"strconv"
)

const pageCSP = "default-src 'none'; style-src 'self'; script-src 'self' 'unsafe-inline'; connect-src 'self'; img-src 'self' data:; base-uri 'none'; frame-ancestors 'none'"

//go:embed templates/*.html templates/*.tmpl static/*
var assets embed.FS

// Scope is one explicit Server scope exposed by the personal Dashboard.
// It is deliberately configuration-owned; the Dashboard never enumerates
// scope identifiers from persistence.
type Scope struct {
	ScopeID     string `json:"scope_id"`
	DisplayName string `json:"display_name"`
}

type Options struct {
	Scopes               []Scope
	HandoffReportEnabled bool
}

type pages struct {
	dashboard *template.Template
	handoff   *template.Template
	scopes    []Scope
	options   Options
	static    http.Handler
}

type pageData struct {
	Title                string
	ActivePage           string
	StatusTitleKey       string
	StatusTitle          string
	HandoffReportEnabled bool
}

// Mount registers only the frozen Dashboard surface on mux. The caller keeps
// ownership of authentication, request IDs, API fallback, and listener state.
func Mount(mux *http.ServeMux, options Options) error {
	if mux == nil {
		return errors.New("webui: mux must not be nil")
	}
	if len(options.Scopes) == 0 {
		return errors.New("webui: Dashboard requires at least one scope")
	}
	dashboard, err := parsePage("dashboard.html")
	if err != nil {
		return err
	}
	var handoff *template.Template
	if options.HandoffReportEnabled {
		handoff, err = parsePage("handoff_report.html")
		if err != nil {
			return err
		}
	}
	staticFS, err := fs.Sub(assets, "static")
	if err != nil {
		return err
	}
	owner := &pages{
		dashboard: dashboard,
		handoff:   handoff,
		scopes:    slices.Clone(options.Scopes),
		options:   options,
		static:    http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))),
	}
	mux.HandleFunc("GET /{$}", owner.dashboardPage)
	mux.HandleFunc("GET /dashboard/scopes", owner.dashboardScopes)
	mux.Handle("GET /static/", owner.static)
	if handoff != nil {
		mux.HandleFunc("GET /handoff-reports", owner.handoffPage)
	}
	return nil
}

func parsePage(name string) (*template.Template, error) {
	return template.New(name).ParseFS(assets, "templates/components.tmpl", "templates/"+name)
}

func (p *pages) dashboardPage(writer http.ResponseWriter, _ *http.Request) {
	p.writePage(writer, p.dashboard, pageData{
		Title: "PowerContext Dashboard", ActivePage: "dashboard",
		StatusTitleKey: "dashboardTitle", StatusTitle: "Dashboard",
		HandoffReportEnabled: p.options.HandoffReportEnabled,
	})
}

func (p *pages) handoffPage(writer http.ResponseWriter, _ *http.Request) {
	p.writePage(writer, p.handoff, pageData{
		Title: "PowerContext Handoff Report", ActivePage: "handoff_report",
		StatusTitleKey: "handoffReportTitle", StatusTitle: "Handoff Report",
		HandoffReportEnabled: true,
	})
}

func (*pages) writePage(writer http.ResponseWriter, page *template.Template, data pageData) {
	var rendered bytes.Buffer
	if page == nil || page.ExecuteTemplate(&rendered, "page", data) != nil {
		http.Error(writer, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Security-Policy", pageCSP)
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("Content-Length", strconv.Itoa(rendered.Len()))
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(rendered.Bytes())
}

func (p *pages) dashboardScopes(writer http.ResponseWriter, _ *http.Request) {
	payload, err := json.Marshal(p.scopes)
	if err != nil {
		http.Error(writer, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Content-Length", strconv.Itoa(len(payload)))
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(payload)
}
