package inference

import (
	"fmt"
	"time"
)

// ConfigurationError reports a stable, non-transient inference configuration
// failure. A wrapped provider cause is available only through errors.Is/As and
// is never interpolated into the stable Error value.
type ConfigurationError struct {
	code   string
	detail string
	cause  error
}

func NewConfigurationError(code, detail string) *ConfigurationError {
	return &ConfigurationError{code: code, detail: detail}
}

// WrapConfigurationError retains the provider or SDK error for errors.Is/As
// without allowing its potentially sensitive text into the stable Error value.
func WrapConfigurationError(code, detail string, cause error) *ConfigurationError {
	return &ConfigurationError{code: code, detail: detail, cause: cause}
}

func (e *ConfigurationError) Error() string {
	messages := map[string]string{
		"instructions":        "inference instructions must not be empty",
		"schema":              "inference input or output schema is not supported",
		"serialize":           "generation input could not be serialized",
		"model":               "inference model is not configured correctly",
		"embedding-model":     "embedding model is not configured correctly",
		"embedding-batch":     "embedding batch size must be positive",
		"dimension-positive":  "embedding profile dimension must be positive",
		"profile-identifiers": "embedding profile identifiers must not be empty",
		"provider-rejected":   "provider rejected the configured inference request",
		"request-rejected":    "inference provider rejected the configured request",
	}
	if message, ok := messages[e.code]; ok {
		return message
	}
	return fmt.Sprintf("inference is not configured correctly: %s", e.code)
}

func (e *ConfigurationError) Code() string      { return e.code }
func (e *ConfigurationError) Detail() string    { return e.detail }
func (e *ConfigurationError) Unwrap() error     { return e.cause }
func (e *ConfigurationError) inferenceFailure() {}

// UnavailableError is a transient provider or network failure.
type UnavailableError struct {
	operation string
	cause     error
}

func NewUnavailableError(operation string) *UnavailableError {
	return &UnavailableError{operation: operation}
}

// WrapUnavailableError retains the provider or network error for errors.Is/As
// while keeping provider response bodies out of the stable Error value.
func WrapUnavailableError(operation string, cause error) *UnavailableError {
	return &UnavailableError{operation: operation, cause: cause}
}

func (e *UnavailableError) Error() string {
	return fmt.Sprintf("inference is temporarily unavailable for %s", e.operation)
}

func (e *UnavailableError) Operation() string { return e.operation }
func (e *UnavailableError) Unwrap() error     { return e.cause }
func (e *UnavailableError) inferenceFailure() {}

// TimeoutError reports that the PowerContext wall-clock budget was exhausted.
type TimeoutError struct {
	operation string
	timeout   time.Duration
}

func NewTimeoutError(operation string, timeout time.Duration) *TimeoutError {
	return &TimeoutError{operation: operation, timeout: timeout}
}

func (e *TimeoutError) Error() string {
	seconds := float64(e.timeout) / float64(time.Second)
	return fmt.Sprintf("inference timed out for %s after %g seconds", e.operation, seconds)
}

func (e *TimeoutError) Operation() string      { return e.operation }
func (e *TimeoutError) Timeout() time.Duration { return e.timeout }
func (e *TimeoutError) inferenceFailure()      {}

// InvalidOutputError reports a provider result that violated the capability
// contract. Detail is intentionally bounded and must not contain provider
// response bodies, prompts, or user content.
type InvalidOutputError struct {
	operation string
	detail    string
}

func NewInvalidOutputError(operation, detail string) *InvalidOutputError {
	return &InvalidOutputError{operation: operation, detail: detail}
}

func (e *InvalidOutputError) Error() string {
	return fmt.Sprintf("invalid inference output for %s: %s", e.operation, e.detail)
}

func (e *InvalidOutputError) Operation() string { return e.operation }
func (e *InvalidOutputError) Detail() string    { return e.detail }
func (e *InvalidOutputError) inferenceFailure() {}
