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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	v1 "github.com/ob-labs/powercontext-go/api/v1"
)

const requestIDHeader = "X-PowerContext-Request-ID"

type operationDescriptor struct {
	path          string
	successStatus int
}

func validateOperationRequest(path string, request any) error {
	var err error
	switch request := request.(type) {
	case interface{ Validate() error }:
		err = request.Validate()
	case *v1.GetStatsParams:
		err = validateGetStatsParams(request)
	default:
		err = errors.New("generated request has no validator")
	}
	if err != nil {
		return &RequestValidationError{Path: path, cause: err}
	}
	if err := v1.ValidatePowerContextContract(request); err != nil {
		return &RequestValidationError{Path: path, cause: err}
	}
	return nil
}

func validateGetStatsParams(params *v1.GetStatsParams) error {
	if params == nil {
		return errors.New("request is nil")
	}
	if strings.TrimSpace(params.ScopeID) == "" || utf8.RuneCountInString(params.ScopeID) > 256 {
		return errors.New("scope_id is invalid")
	}
	if period, ok := params.Period.Get(); ok &&
		period != v1.StatsPeriodToday && period != v1.StatsPeriod7d && period != v1.StatsPeriod30d {
		return errors.New("period is invalid")
	}
	return nil
}

type responseCapture struct {
	statusCode    int
	successStatus int
	requestID     string
	body          []byte
	bodyErr       error
}

type responseCaptureKey struct{}

const (
	maxCapturedErrorBytes    = 1 * 1024 * 1024
	maxCapturedResponseBytes = 10 * 1024 * 1024
)

type responseBodyCapture struct {
	body []byte
	err  error
}

type responseBodyCaptureKey struct{}

func captureResponse(ctx context.Context, response *http.Response) {
	if ctx == nil || response == nil {
		return
	}
	capture, _ := ctx.Value(responseCaptureKey{}).(*responseCapture)
	if capture != nil {
		capture.statusCode = response.StatusCode
		capture.requestID = response.Header.Get(requestIDHeader)
	}
	captureResponseBody(ctx, response, capture)
}

// captureResponseBody is deliberately opt-in. Handoff Report downloads need
// the exact canonical bytes returned by the Server, while ordinary generated
// operations should retain ogen's streaming/decoding behavior without a
// second in-memory copy.
func captureResponseBody(ctx context.Context, response *http.Response, metadata *responseCapture) {
	explicit, _ := ctx.Value(responseBodyCaptureKey{}).(*responseBodyCapture)
	captureError := metadata != nil && response.StatusCode != metadata.successStatus
	if (!captureError && explicit == nil) || response.Body == nil {
		return
	}
	limit := maxCapturedErrorBytes
	if explicit != nil {
		limit = maxCapturedResponseBytes
	}
	original := response.Body
	data, readErr := io.ReadAll(io.LimitReader(original, int64(limit)+1))
	closeErr := original.Close()
	if readErr == nil {
		readErr = closeErr
	}
	if len(data) > limit {
		data = data[:limit]
		readErr = fmt.Errorf("response exceeds %d bytes", limit)
	}
	if captureError {
		metadata.body = bytes.Clone(data)
		metadata.bodyErr = readErr
	}
	if explicit != nil {
		explicit.body = bytes.Clone(data)
		explicit.err = readErr
	}
	response.Body = io.NopCloser(bytes.NewReader(data))
}

func invokeOperation[Response any](
	ctx context.Context,
	operation operationDescriptor,
	invoke func(context.Context) (Response, error),
) (Response, error) {
	var zero Response
	capture := &responseCapture{successStatus: operation.successStatus}
	value, err := invoke(context.WithValue(ctx, responseCaptureKey{}, capture))
	if err == nil {
		if serverError, ok := AsServerError(value); ok {
			return zero, serverError
		}
		if capture.statusCode != 0 && capture.statusCode != operation.successStatus {
			return zero, capturedServerError(capture)
		}
		if validationErr := v1.ValidatePowerContextContract(value); validationErr != nil {
			return zero, &InvalidResponseError{
				Path: operation.path, RequestID: capture.requestID, cause: validationErr,
			}
		}
		return value, nil
	}

	// Caller cancellation remains a normal context error. Client-side timeouts
	// and network failures are classified below because the caller Context is
	// still live in those cases.
	if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
		return zero, ctx.Err()
	}
	if capture.statusCode != 0 {
		if capture.statusCode == operation.successStatus {
			return zero, &InvalidResponseError{
				Path: operation.path, RequestID: capture.requestID, cause: err,
			}
		}
		return zero, capturedServerError(capture)
	}
	var transport *TransportError
	if errors.As(err, &transport) {
		return zero, transport
	}
	var urlError *url.Error
	if errors.As(err, &urlError) {
		return zero, &TransportError{Path: operation.path, cause: err}
	}
	return zero, err
}

func capturedServerError(capture *responseCapture) *ServerError {
	result := &ServerError{StatusCode: capture.statusCode, RequestID: capture.requestID}
	if capture.bodyErr != nil || len(capture.body) == 0 {
		return result
	}
	var wire v1.ErrorResponse
	if err := json.Unmarshal(capture.body, &wire); err != nil {
		return result
	}
	result.Code = wire.Error.Code
	result.Message = wire.Error.Message
	result.Details = decodeDetails(wire.Error.Details)
	return result
}
