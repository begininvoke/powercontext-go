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

package work

func encodeCurrentHandoff(value CurrentHandoff) (currentHandoffJSON, error) {
	if err := value.Validate(); err != nil {
		return currentHandoffJSON{}, err
	}
	state, err := encodeClaims(value.state)
	if err != nil {
		return currentHandoffJSON{}, err
	}
	var next *claimJSON
	if value.nextAction != nil {
		encoded, encodeErr := encodeClaim(*value.nextAction)
		if encodeErr != nil {
			return currentHandoffJSON{}, encodeErr
		}
		next = &encoded
	}
	return currentHandoffJSON{
		Schema: CurrentWorkHandoffSchema, Trust: UntrustedInput, Objective: value.objective,
		State: state, Disposition: value.disposition, NextAction: next, Omissions: nonNil(value.omissions),
	}, nil
}

func decodeCurrentHandoff(value currentHandoffJSON) (CurrentHandoff, error) {
	if value.Schema != CurrentWorkHandoffSchema || value.Trust != UntrustedInput {
		return CurrentHandoff{}, &InvalidError{Field: "handoff.schema", Detail: "does not match the current Work Handoff"}
	}
	state, err := decodeClaims(value.State)
	if err != nil {
		return CurrentHandoff{}, err
	}
	var next *Claim
	if value.NextAction != nil {
		decoded, decodeErr := decodeClaim(*value.NextAction)
		if decodeErr != nil {
			return CurrentHandoff{}, decodeErr
		}
		next = &decoded
	}
	return NewCurrentHandoff(value.Objective, state, value.Disposition, next, value.Omissions)
}
