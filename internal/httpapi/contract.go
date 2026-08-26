package httpapi

import (
	"fmt"

	v1 "github.com/ob-labs/powercontext-go/api/v1"
)

// ValidatePowerContextContract supplements ogen's OpenAPI 3.0 validation with
// the few cross-field rules that the schema format cannot encode. Requests are
// rejected before application telemetry and endpoint dispatch; invalid
// responses are treated as server failures.
func ValidatePowerContextContract(request Request, next Next) (Response, error) {
	if err := v1.ValidatePowerContextContract(request.Body); err != nil {
		return Response{}, &requestContractError{cause: err}
	}
	response, err := next(request)
	if err != nil {
		return response, err
	}
	if err := v1.ValidatePowerContextContract(response.Type); err != nil {
		return Response{}, fmt.Errorf("HTTP response violates the PowerContext contract: %w", err)
	}
	return response, nil
}

type requestContractError struct{ cause error }

func (*requestContractError) Error() string { return "HTTP request violates the PowerContext contract" }

func (e *requestContractError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}
