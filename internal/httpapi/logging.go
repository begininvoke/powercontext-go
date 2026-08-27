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

package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"time"

	serverlogging "github.com/ob-labs/powercontext-go/internal/observability/logging"
)

type ApplicationError struct {
	StatusCode int
	Code       string
}

type ApplicationErrorClassifier func(error) ApplicationError

// LogApplicationFailures records decoded application failures and
// cancellations. Successful operations are already represented by metrics and
// spans and intentionally remain silent, matching the frozen Python behavior.
func LogApplicationFailures(logger *slog.Logger, classify ApplicationErrorClassifier) Middleware {
	return func(request Request, next Next) (Response, error) {
		if request.OperationID == "get_liveness" || request.OperationID == "get_readiness" {
			return next(request)
		}
		started := time.Now()
		response, err := next(request)
		if errorsIsCancelled(request.Context, err) {
			serverlogging.LogApplicationCompletion(request.Context, logger, serverlogging.ApplicationObservation{
				Operation: request.OperationID, Outcome: "cancelled", Duration: time.Since(started),
			})
			return response, err
		}
		if err != nil {
			mapped := ApplicationError{StatusCode: 500, Code: "internal_error"}
			if classify != nil {
				mapped = classify(err)
			}
			serverlogging.LogApplicationCompletion(request.Context, logger, serverlogging.ApplicationObservation{
				Operation: request.OperationID, Outcome: "failure", Duration: time.Since(started),
				StatusCode: mapped.StatusCode, ErrorCode: mapped.Code,
			})
		}
		return response, err
	}
}

func errorsIsCancelled(ctx context.Context, err error) bool {
	return context.Cause(ctx) != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
