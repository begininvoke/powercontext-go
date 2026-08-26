package server

import (
	"errors"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	v1 "github.com/ob-labs/powercontext-go/api/v1"
	"github.com/ob-labs/powercontext-go/internal/endpoint"
	"github.com/ob-labs/powercontext-go/internal/httpapi"
	"github.com/ob-labs/powercontext-go/internal/mcpapi"
	serverlogging "github.com/ob-labs/powercontext-go/internal/observability/logging"
	servermetrics "github.com/ob-labs/powercontext-go/internal/observability/metrics"
	requesttrace "github.com/ob-labs/powercontext-go/internal/observability/tracing"
	"github.com/ob-labs/powercontext-go/internal/webui"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

type HTTPOptions struct {
	BearerToken         string
	HandoffReportRoutes bool
	TracerProvider      trace.TracerProvider
	MeterProvider       metric.MeterProvider
	Metrics             *servermetrics.Server
	Logger              *slog.Logger
	AccessLog           bool
	MCP                 MCPOptions
	Dashboard           *webui.Options
}

// MCPOptions controls the optional MCP Streamable HTTP route. Path defaults to
// /mcp. A disabled MCP surface is not mounted and therefore returns 404.
type MCPOptions struct {
	Enabled        bool
	Path           string
	Version        string
	Stateless      bool
	JSONResponse   bool
	SessionTimeout time.Duration
	Logger         *slog.Logger
}

// NewHTTPHandler assembles the generated OpenAPI server and the transport-wide
// policy middleware. Lifecycle and listener ownership remain with cmd/server.
func NewHTTPHandler(handler v1.Handler, options HTTPOptions) (http.Handler, error) {
	if handler == nil {
		return nil, errors.New("server: endpoint handler must not be nil")
	}
	security, err := httpapi.NewSecurity(options.BearerToken)
	if err != nil {
		return nil, err
	}
	var applicationLogger, accessLogger *slog.Logger
	if options.Logger != nil {
		applicationLogger = serverlogging.Named(options.Logger, "powercontext.server.app")
		accessLogger = serverlogging.Named(options.Logger, "powercontext.server.access")
	}
	middlewares := []v1.Middleware{httpapi.BindSpanRequestID, httpapi.ValidatePowerContextContract}
	middlewares = append(middlewares, httpapi.TraceApplication(options.TracerProvider))
	if applicationLogger != nil {
		middlewares = append(middlewares, httpapi.LogApplicationFailures(applicationLogger, func(err error) httpapi.ApplicationError {
			mapped := endpoint.MapError(err)
			return httpapi.ApplicationError{StatusCode: mapped.StatusCode, Code: mapped.Code}
		}))
	}
	if options.Metrics != nil {
		middlewares = append(middlewares, options.Metrics.HTTPMiddleware)
	}
	serverOptions := []v1.ServerOption{
		v1.WithTracerProvider(httpapi.TracerProvider(options.TracerProvider)),
		v1.WithMiddleware(middlewares...),
		v1.WithErrorHandler(httpapi.ErrorHandler(func(err error) (int, httpapi.Error, bool) {
			mapped := endpoint.MapError(err)
			return mapped.StatusCode, httpapi.Error{
				Code: mapped.Code, Message: mapped.Message, Details: mapped.Details,
			}, true
		})),
	}
	if options.MeterProvider != nil {
		serverOptions = append(serverOptions, v1.WithMeterProvider(options.MeterProvider))
	}
	generated, err := v1.NewServer(handler, security, serverOptions...)
	if err != nil {
		return nil, err
	}
	mcpPath := ""
	if options.MCP.Enabled {
		mcpPath, err = normalizeMCPPath(options.MCP.Path)
		if err != nil {
			return nil, err
		}
	}
	var application http.Handler = generated
	var mux *http.ServeMux
	if options.MCP.Enabled || options.Metrics != nil || options.Dashboard != nil {
		mux = http.NewServeMux()
		mux.Handle("/", generated)
		application = mux
	}
	if options.Dashboard != nil {
		if err := webui.Mount(mux, *options.Dashboard); err != nil {
			return nil, err
		}
	}
	if options.Metrics != nil {
		mux.Handle("/metrics", options.Metrics.Handler())
	}
	if options.MCP.Enabled {
		receiving := []mcp.Middleware{requesttrace.MCPMiddleware(options.TracerProvider)}
		if options.AccessLog && accessLogger != nil {
			receiving = append(receiving, mcpapi.AccessLogMiddleware(accessLogger))
		}
		mcpServer, mcpErr := mcpapi.NewServer(handler, mcpapi.Options{
			Version:              options.MCP.Version,
			HandoffReportEnabled: options.HandoffReportRoutes,
			ApplicationObserver:  options.Metrics,
			ApplicationLogger:    applicationLogger,
			TracerProvider:       options.TracerProvider,
			ReceivingMiddleware:  receiving,
		})
		if mcpErr != nil {
			return nil, mcpErr
		}
		if options.Metrics != nil {
			mcpServer.AddReceivingMiddleware(options.Metrics.MCPMiddleware())
		}
		mcpHandler := mcpapi.NewHTTPHandler(mcpServer, mcpapi.HTTPOptions{
			Stateless: options.MCP.Stateless, JSONResponse: options.MCP.JSONResponse,
			SessionTimeout: options.MCP.SessionTimeout, Logger: options.MCP.Logger,
		})
		mux.Handle(mcpPath+"/", http.StripPrefix(mcpPath, mcpHandler))
		mux.Handle(mcpPath, http.RedirectHandler(mcpPath+"/", http.StatusTemporaryRedirect))
	}
	var access *httpapi.AccessLogOptions
	if options.AccessLog && accessLogger != nil {
		access = &httpapi.AccessLogOptions{
			Logger: accessLogger,
			ResolveOperation: func(request *http.Request) string {
				if !options.HandoffReportRoutes && strings.HasPrefix(request.URL.Path, "/v1/handoff-reports/") {
					return "unmatched"
				}
				route, found := generated.FindPath(request.Method, request.URL)
				if !found {
					return "unmatched"
				}
				return route.OperationID()
			},
			Skip: func(request *http.Request) bool {
				for _, prefix := range []string{"/health/live", "/health/ready", "/metrics"} {
					if strings.HasPrefix(request.URL.Path, prefix) {
						return true
					}
				}
				return mcpPath != "" && strings.HasPrefix(request.URL.Path, mcpPath)
			},
		}
	}
	return httpapi.Wrap(application, httpapi.Options{
		BearerToken: options.BearerToken, HandoffReportRoutes: options.HandoffReportRoutes,
		Access: access,
	})
}

func normalizeMCPPath(value string) (string, error) {
	if value == "" {
		return DefaultMCPPath, nil
	}
	if !strings.HasPrefix(value, "/") || strings.ContainsAny(value, "?#") {
		return "", errors.New("server: MCP path must be an absolute URL path")
	}
	cleaned := path.Clean(value)
	if cleaned == "/" || cleaned == "." {
		return "", errors.New("server: MCP path must not replace the server root")
	}
	return cleaned, nil
}
