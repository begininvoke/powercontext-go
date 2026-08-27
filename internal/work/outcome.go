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
	"fmt"
	"slices"

	"github.com/ob-labs/powercontext-go/artifact"
	"github.com/ob-labs/powercontext-go/artifact/handoff"
	"github.com/ob-labs/powercontext-go/source"
)

type CheckStatus string

const (
	CheckPassed      CheckStatus = "passed"
	CheckFailed      CheckStatus = "failed"
	CheckSkipped     CheckStatus = "skipped"
	CheckTimedOut    CheckStatus = "timed_out"
	CheckUnavailable CheckStatus = "unavailable"
	CheckCancelled   CheckStatus = "cancelled"
	CheckUnknown     CheckStatus = "unknown"
)

type TaskCheck struct {
	name     string
	status   CheckStatus
	details  *string
	basis    ClaimBasis
	evidence []handoff.Citation
}

func NewTaskCheck(name string, status CheckStatus, details *string, basis ClaimBasis, evidence []handoff.Citation) (TaskCheck, error) {
	value := TaskCheck{name: name, status: status, details: cloneString(details), basis: basis, evidence: slices.Clone(evidence)}
	if err := value.Validate(); err != nil {
		return TaskCheck{}, err
	}
	return value, nil
}

func (c TaskCheck) Name() string                 { return c.name }
func (c TaskCheck) Status() CheckStatus          { return c.status }
func (c TaskCheck) Details() *string             { return cloneString(c.details) }
func (c TaskCheck) Basis() ClaimBasis            { return c.basis }
func (c TaskCheck) Evidence() []handoff.Citation { return slices.Clone(c.evidence) }
func (c TaskCheck) Validate() error {
	if err := validateText("check.name", c.name, MaxTextLength); err != nil {
		return err
	}
	switch c.status {
	case CheckPassed, CheckFailed, CheckSkipped, CheckTimedOut, CheckUnavailable, CheckCancelled, CheckUnknown:
	default:
		return &InvalidError{Field: "check.status", Detail: "has an unsupported value"}
	}
	if c.details != nil {
		if err := validateText("check.details", *c.details, MaxTextLength); err != nil {
			return err
		}
	}
	if c.basis != Declared && c.basis != Verified {
		return &InvalidError{Field: "check.basis", Detail: "must be declared or verified"}
	}
	if len(c.evidence) > MaxEvidence {
		return &InvalidError{Field: "check.evidence", Detail: fmt.Sprintf("must contain at most %d items", MaxEvidence)}
	}
	if err := validateCitations("check.evidence", c.evidence); err != nil {
		return err
	}
	if c.basis == Verified && len(c.evidence) == 0 {
		return &InvalidError{Field: "check.evidence", Detail: "verified Task checks require exact evidence"}
	}
	if c.basis == Declared && len(c.evidence) != 0 {
		return &InvalidError{Field: "check.evidence", Detail: "declared Task checks cannot present evidence as verified"}
	}
	return nil
}

type OutcomeStatus string

const (
	OutcomeSucceeded OutcomeStatus = "succeeded"
	OutcomePartial   OutcomeStatus = "partial"
	OutcomeBlocked   OutcomeStatus = "blocked"
	OutcomeFailed    OutcomeStatus = "failed"
	OutcomeCancelled OutcomeStatus = "cancelled"
	OutcomeUnknown   OutcomeStatus = "unknown"
)

type TaskOutcome struct {
	objective         string
	status            OutcomeStatus
	summary           string
	handoffReceiptRef *source.Ref
	observations      []Claim
	checks            []TaskCheck
	producedArtifacts []artifact.Ref
	remainingWork     []string
}

func NewTaskOutcome(
	objective string,
	status OutcomeStatus,
	summary string,
	handoffReceiptRef *source.Ref,
	observations []Claim,
	checks []TaskCheck,
	producedArtifacts []artifact.Ref,
	remainingWork []string,
) (TaskOutcome, error) {
	value := TaskOutcome{
		objective: objective, status: status, summary: summary, handoffReceiptRef: cloneSourceRef(handoffReceiptRef),
		observations: slices.Clone(observations), checks: slices.Clone(checks),
		producedArtifacts: slices.Clone(producedArtifacts), remainingWork: slices.Clone(remainingWork),
	}
	if err := value.Validate(); err != nil {
		return TaskOutcome{}, err
	}
	return value, nil
}

func (o TaskOutcome) Schema() string                    { return TaskOutcomeSchema }
func (o TaskOutcome) Trust() string                     { return UntrustedObservation }
func (o TaskOutcome) Objective() string                 { return o.objective }
func (o TaskOutcome) Status() OutcomeStatus             { return o.status }
func (o TaskOutcome) Summary() string                   { return o.summary }
func (o TaskOutcome) HandoffReceiptRef() *source.Ref    { return cloneSourceRef(o.handoffReceiptRef) }
func (o TaskOutcome) Observations() []Claim             { return slices.Clone(o.observations) }
func (o TaskOutcome) Checks() []TaskCheck               { return slices.Clone(o.checks) }
func (o TaskOutcome) ProducedArtifacts() []artifact.Ref { return slices.Clone(o.producedArtifacts) }
func (o TaskOutcome) RemainingWork() []string           { return slices.Clone(o.remainingWork) }
func (o TaskOutcome) Validate() error {
	if err := validateText("outcome.objective", o.objective, MaxTextLength); err != nil {
		return err
	}
	if err := validateText("outcome.summary", o.summary, MaxTextLength); err != nil {
		return err
	}
	switch o.status {
	case OutcomeSucceeded, OutcomePartial, OutcomeBlocked, OutcomeFailed, OutcomeCancelled, OutcomeUnknown:
	default:
		return &InvalidError{Field: "outcome.status", Detail: "has an unsupported value"}
	}
	if o.handoffReceiptRef != nil {
		if _, err := source.NewRef(o.handoffReceiptRef.Type(), o.handoffReceiptRef.ID()); err != nil {
			return err
		}
	}
	if err := validateClaims("outcome.observations", o.observations, 1, MaxItems); err != nil {
		return err
	}
	if len(o.checks) > MaxItems {
		return &InvalidError{Field: "outcome.checks", Detail: fmt.Sprintf("must contain at most %d items", MaxItems)}
	}
	for _, check := range o.checks {
		if err := check.Validate(); err != nil {
			return err
		}
	}
	if len(o.producedArtifacts) > MaxEvidence {
		return &InvalidError{Field: "outcome.produced_artifacts", Detail: fmt.Sprintf("must contain at most %d items", MaxEvidence)}
	}
	for _, ref := range o.producedArtifacts {
		if err := ref.Validate(); err != nil {
			return err
		}
	}
	return validateTextItems("outcome.remaining_work", o.remainingWork, 0, MaxItems)
}
