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

import (
	"github.com/ob-labs/powercontext-go/artifact"
	"github.com/ob-labs/powercontext-go/source"
)

func encodeTaskOutcome(value TaskOutcome) (taskOutcomeJSON, error) {
	if err := value.Validate(); err != nil {
		return taskOutcomeJSON{}, err
	}
	observations, err := encodeClaims(value.observations)
	if err != nil {
		return taskOutcomeJSON{}, err
	}
	checks := make([]taskCheckJSON, len(value.checks))
	for index, check := range value.checks {
		evidence, encodeErr := encodeCitations(check.evidence)
		if encodeErr != nil {
			return taskOutcomeJSON{}, encodeErr
		}
		checks[index] = taskCheckJSON{Name: check.name, Status: check.status, Details: cloneString(check.details), Basis: check.basis, Evidence: evidence}
	}
	artifacts := make([]artifactRefJSON, len(value.producedArtifacts))
	for index, ref := range value.producedArtifacts {
		artifacts[index] = encodeArtifactRef(ref)
	}
	var receipt *sourceRefJSON
	if value.handoffReceiptRef != nil {
		encoded := encodeSourceRef(*value.handoffReceiptRef)
		receipt = &encoded
	}
	return taskOutcomeJSON{
		Schema: TaskOutcomeSchema, Trust: UntrustedObservation, Objective: value.objective,
		Status: value.status, Summary: value.summary, HandoffReceiptRef: receipt,
		Observations: observations, Checks: checks, ProducedArtifacts: artifacts,
		RemainingWork: nonNil(value.remainingWork),
	}, nil
}

func decodeTaskOutcome(value taskOutcomeJSON) (TaskOutcome, error) {
	if value.Schema != TaskOutcomeSchema || value.Trust != UntrustedObservation {
		return TaskOutcome{}, &InvalidError{Field: "outcome.schema", Detail: "does not match the Task outcome"}
	}
	observations, err := decodeClaims(value.Observations)
	if err != nil {
		return TaskOutcome{}, err
	}
	checks := make([]TaskCheck, len(value.Checks))
	for index, check := range value.Checks {
		evidence, decodeErr := decodeCitations(check.Evidence)
		if decodeErr != nil {
			return TaskOutcome{}, decodeErr
		}
		checks[index], decodeErr = NewTaskCheck(check.Name, check.Status, check.Details, check.Basis, evidence)
		if decodeErr != nil {
			return TaskOutcome{}, decodeErr
		}
	}
	artifacts := make([]artifact.Ref, len(value.ProducedArtifacts))
	for index, ref := range value.ProducedArtifacts {
		artifacts[index], err = decodeArtifactRef(ref)
		if err != nil {
			return TaskOutcome{}, err
		}
	}
	var receipt *source.Ref
	if value.HandoffReceiptRef != nil {
		decoded, decodeErr := decodeSourceRef(*value.HandoffReceiptRef)
		if decodeErr != nil {
			return TaskOutcome{}, decodeErr
		}
		receipt = &decoded
	}
	return NewTaskOutcome(value.Objective, value.Status, value.Summary, receipt, observations, checks, artifacts, value.RemainingWork)
}
