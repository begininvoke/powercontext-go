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
