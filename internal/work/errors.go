package work

import "fmt"

// InvalidError identifies a domain validation failure without exposing the
// submitted value. Transport adapters map it to the public invalid_request
// envelope.
type InvalidError struct {
	Field  string
	Detail string
}

func (e *InvalidError) Error() string {
	if e.Detail == "" {
		return "invalid Work " + e.Field
	}
	return fmt.Sprintf("invalid Work %s: %s", e.Field, e.Detail)
}

// InvalidRequestError identifies a validly decoded request whose selected
// history cannot satisfy the Work continuity trust boundary.
type InvalidRequestError struct{ Code string }

func (e *InvalidRequestError) Error() string { return "invalid Work request: " + e.Code }
