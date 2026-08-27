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

func encodeContract(value Contract) (contractJSON, error) {
	if err := value.Validate(); err != nil {
		return contractJSON{}, err
	}
	facts, err := encodeClaims(value.facts)
	if err != nil {
		return contractJSON{}, err
	}
	return contractJSON{
		Schema: WorkContractSchema, Trust: UntrustedInput, Objective: value.objective,
		Facts: facts, InScope: nonNil(value.inScope), Exclusions: nonNil(value.exclusions),
		CompletionCriteria: nonNil(value.completionCriteria), AuthorizationNotes: nonNil(value.authorizationNotes),
		OpenQuestions: nonNil(value.openQuestions),
	}, nil
}

func decodeContract(value contractJSON) (Contract, error) {
	if value.Schema != WorkContractSchema || value.Trust != UntrustedInput {
		return Contract{}, &InvalidError{Field: "contract.schema", Detail: "does not match the Work contract"}
	}
	facts, err := decodeClaims(value.Facts)
	if err != nil {
		return Contract{}, err
	}
	return NewContract(value.Objective, facts, value.InScope, value.Exclusions, value.CompletionCriteria, value.AuthorizationNotes, value.OpenQuestions)
}
