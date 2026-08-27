// Copyright (c) 2026 OceanBase.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package client

import (
	"bytes"
	"context"
	"io"

	v1 "github.com/ob-labs/powercontext-go/api/v1"
)

const handoffReportPath = "/v1/handoff-reports/get"

// RenderHandoffReport renders one Markdown report without mutating request.
// Call GetHandoffReport directly when a typed JSON projection is required.
func (c *Client) RenderHandoffReport(ctx context.Context, request v1.GetHandoffReportRequest) (string, error) {
	if request.Download.Or(false) {
		return "", &ConfigurationError{Field: "request.download"}
	}
	if format, ok := request.Format.Get(); ok && format != v1.ReportFormatMarkdown {
		return "", &ConfigurationError{Field: "request.format"}
	}
	request.Download = v1.NewOptBool(false)
	request.Format = v1.NewOptReportFormat(v1.ReportFormatMarkdown)

	response, err := c.GetHandoffReport(ctx, &request)
	if err != nil {
		return "", err
	}
	markdown, ok := response.(*v1.GetHandoffReportOKTextMarkdownHeaders)
	if !ok {
		return "", &InvalidResponseError{
			Path: handoffReportPath, RequestID: responseRequestID(response),
		}
	}
	content, err := io.ReadAll(markdown.Response)
	if err != nil {
		return "", &InvalidResponseError{
			Path: handoffReportPath, RequestID: responseRequestID(response), cause: err,
		}
	}
	return string(content), nil
}

// DownloadHandoffReport returns the exact Markdown or canonical JSON bytes
// supplied by the Server. The response is bounded to the public 10 MiB report
// limit and request is copied before download=true is applied.
func (c *Client) DownloadHandoffReport(ctx context.Context, request v1.GetHandoffReportRequest) ([]byte, error) {
	request.Download = v1.NewOptBool(true)
	capture := new(responseBodyCapture)
	response, err := c.GetHandoffReport(context.WithValue(ctx, responseBodyCaptureKey{}, capture), &request)
	if err != nil {
		return nil, err
	}
	if capture.err != nil {
		return nil, &InvalidResponseError{
			Path: handoffReportPath, RequestID: responseRequestID(response), cause: capture.err,
		}
	}
	switch response.(type) {
	case *v1.GetHandoffReportOKTextMarkdownHeaders, *v1.HandoffReportResponseHeaders:
		return bytes.Clone(capture.body), nil
	default:
		return nil, &InvalidResponseError{
			Path: handoffReportPath, RequestID: responseRequestID(response),
		}
	}
}

func responseRequestID(response any) string {
	withRequestID, ok := response.(interface {
		GetXPowerContextRequestID() v1.OptString
	})
	if !ok {
		return ""
	}
	requestID, _ := withRequestID.GetXPowerContextRequestID().Get()
	return requestID
}
