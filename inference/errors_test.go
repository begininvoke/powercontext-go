package inference

import (
	"errors"
	"strings"
	"testing"
)

func TestWrappedInferenceErrorsPreserveCauseWithoutLeakingItsText(t *testing.T) {
	provider := errors.New("secret provider response")
	for _, err := range []error{
		WrapConfigurationError("provider-rejected", "", provider),
		WrapUnavailableError("generate", provider),
	} {
		if !errors.Is(err, provider) {
			t.Fatalf("error %T did not retain its cause", err)
		}
		if strings.Contains(err.Error(), provider.Error()) || strings.Contains(err.Error(), "secret") {
			t.Fatalf("error %T leaked its cause: %q", err, err)
		}
	}
}
