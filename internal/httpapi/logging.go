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
