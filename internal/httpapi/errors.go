package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/ogen-go/ogen/ogenerrors"
	v1 "github.com/thunguo/powercontext-go/api/v1"
)

// ErrorMapper maps application/domain errors at the transport boundary. The
// bool is false when the mapper does not recognize an error.
type ErrorMapper func(error) (statusCode int, detail Error, ok bool)

// ErrorHandler returns the error adapter used by the generated server. Decode
// failures are deliberately normalized to FastAPI's 422 contract and raw error
// strings are never returned to clients.
func ErrorHandler(mapper ErrorMapper) v1.ErrorHandler {
	return func(_ context.Context, w http.ResponseWriter, _ *http.Request, err error) {
		if statusCode, detail, ok := mapTransportError(err); ok {
			writeError(w, statusCode, detail)
			return
		}
		if mapper != nil {
			if statusCode, detail, ok := mapper(err); ok {
				writeError(w, statusCode, detail)
				return
			}
		}
		writeError(w, http.StatusInternalServerError, Error{
			Code:    "internal_error",
			Message: "The Server failed.",
		})
	}
}

func mapTransportError(err error) (int, Error, bool) {
	var security *ogenerrors.SecurityError
	if errors.As(err, &security) {
		return http.StatusUnauthorized, Error{
			Code:    "unauthorized",
			Message: "A valid bearer token is required.",
		}, true
	}

	var request *ogenerrors.DecodeRequestError
	var params *ogenerrors.DecodeParamsError
	var semantic *requestContractError
	if errors.As(err, &request) || errors.As(err, &params) || errors.As(err, &semantic) {
		return http.StatusUnprocessableEntity, Error{
			Code:    "invalid_request",
			Message: "The request violates the API contract.",
		}, true
	}
	return 0, Error{}, false
}
