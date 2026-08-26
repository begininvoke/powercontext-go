package handoff

import "fmt"

type EvidenceUnavailableError struct{ Citation Citation }

func (e *EvidenceUnavailableError) Error() string { return "Handoff evidence is unavailable" }

type GenerationUnavailableError struct{}

func (*GenerationUnavailableError) Error() string { return "Handoff generation is not configured" }

type InvalidGenerationError struct{ Code string }

func (e *InvalidGenerationError) Error() string {
	switch e.Code {
	case "output":
		return "Handoff generation returned an invalid output type"
	case "objective":
		return "generated Handoff changed the caller-owned objective"
	case "evidence":
		return "generated Handoff cited evidence outside the preparation action"
	case "budget":
		return "generated Handoff exceeds the preparation byte budget"
	default:
		return "invalid Handoff generation: " + e.Code
	}
}

type ScopeMismatchError struct{ Expected, Actual string }

func (e *ScopeMismatchError) Error() string {
	return fmt.Sprintf("Prepared Handoff belongs to scope %q, expected %q", e.Actual, e.Expected)
}

type InvalidReferenceError struct{}

func (*InvalidReferenceError) Error() string {
	return "reference does not address the current Handoff lifecycle"
}
