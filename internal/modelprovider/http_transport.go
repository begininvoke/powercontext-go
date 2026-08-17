package modelprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/thunguo/powercontext-go/inference"
)

const maxProviderResponseBytes int64 = 8 << 20

var defaultProviderHTTPClient = &http.Client{
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// providerHTTPClient is intentionally local to modelprovider. It is the small
// transport surface shared by providers without a stable official Go SDK; it
// never retains response bodies in returned errors.
type providerHTTPClient struct {
	client  *http.Client
	baseURL *url.URL
	headers http.Header
}

func newProviderHTTPClient(client *http.Client, baseURL string, headers http.Header) (providerHTTPClient, error) {
	parsed, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return providerHTTPClient{}, inference.NewConfigurationError("model", "invalid provider base URL")
	}
	if client == nil {
		client = defaultProviderHTTPClient
	}
	return providerHTTPClient{client: client, baseURL: parsed, headers: headers.Clone()}, nil
}

func (c providerHTTPClient) postJSON(ctx context.Context, path string, input, output any, operation string) error {
	if path == "" || !strings.HasPrefix(path, "/") || strings.Contains(path, "?") || strings.Contains(path, "#") {
		return inference.NewConfigurationError("model", "invalid provider endpoint path")
	}
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(input); err != nil {
		return inference.NewConfigurationError("request-rejected", "provider request could not be encoded")
	}

	endpoint := *c.baseURL
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + path
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), &encoded)
	if err != nil {
		return inference.NewConfigurationError("model", "provider request could not be constructed")
	}
	request.Header = c.headers.Clone()
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")

	response, err := c.client.Do(request)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return inference.WrapUnavailableError(operation, err)
	}
	defer response.Body.Close()

	limited := io.LimitReader(response.Body, maxProviderResponseBytes+1)
	body, readErr := io.ReadAll(limited)
	if readErr != nil {
		return inference.WrapUnavailableError(operation, readErr)
	}
	if int64(len(body)) > maxProviderResponseBytes {
		return inference.NewInvalidOutputError(operation, "provider response exceeded the size limit")
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return mapProviderHTTPStatus(response.StatusCode, operation, nil)
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(output); err != nil {
		return inference.NewInvalidOutputError(operation, "provider returned invalid JSON")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return inference.NewInvalidOutputError(operation, "provider returned trailing JSON data")
	}
	return nil
}

func mapProviderHTTPStatus(status int, operation string, cause error) error {
	switch {
	case status == http.StatusRequestTimeout || status == http.StatusGatewayTimeout:
		return context.DeadlineExceeded
	case status == http.StatusConflict || status == http.StatusTooEarly ||
		status == http.StatusTooManyRequests || status >= http.StatusInternalServerError:
		if cause != nil {
			return inference.WrapUnavailableError(operation, cause)
		}
		return inference.NewUnavailableError(operation)
	default:
		detail := fmt.Sprintf("provider returned HTTP status %d", status)
		if cause != nil {
			return inference.WrapConfigurationError("provider-rejected", detail, cause)
		}
		return inference.NewConfigurationError("provider-rejected", detail)
	}
}
