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
	"reflect"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ob-labs/powercontext-go/artifact"
	"github.com/ob-labs/powercontext-go/artifact/handoff"
	"github.com/ob-labs/powercontext-go/source"
)

const (
	MaxTextLength             = 8_192
	MaxItems                  = 64
	MaxEvidence               = 32
	MaxClaimEvidence          = handoff.MaxCitations - 1
	MaxReceiptEvidence        = (handoff.MaxStateStatements + 1) * handoff.MaxCitations
	MaxContinuityEvents       = 64
	WorkContractSourceKind    = Kind("work-contract")
	HandoffBoundarySourceKind = Kind("handoff-boundary")
	HandoffReceiptSourceKind  = Kind("handoff-receipt")
	TaskOutcomeSourceKind     = Kind("task-outcome")
	WorkContractSchema        = "powercontext.work-contract.v1"
	CurrentWorkHandoffSchema  = "powercontext.current-work-handoff.v1"
	HandoffReceiptSchema      = "powercontext.handoff-receipt.v1"
	TaskOutcomeSchema         = "powercontext.task-outcome.v1"
	WorkContinuitySchema      = "powercontext.work-continuity.v1"
	UntrustedInput            = "untrusted_input"
	UntrustedObservation      = "untrusted_observation"
	UntrustedHistory          = "untrusted_history"
)

type Kind string

func (k Kind) Validate() error {
	switch k {
	case WorkContractSourceKind, HandoffBoundarySourceKind, HandoffReceiptSourceKind, TaskOutcomeSourceKind:
		return nil
	default:
		return &InvalidError{Field: "kind", Detail: fmt.Sprintf("unsupported value %q", k)}
	}
}

type ClaimBasis string

const (
	Declared ClaimBasis = "declared"
	Verified ClaimBasis = "verified"
)

type Claim struct {
	text     string
	basis    ClaimBasis
	evidence []handoff.Citation
}

func NewClaim(text string, basis ClaimBasis, evidence []handoff.Citation) (Claim, error) {
	value := Claim{text: text, basis: basis, evidence: slices.Clone(evidence)}
	if err := value.Validate(); err != nil {
		return Claim{}, err
	}
	return value, nil
}

func (c Claim) Text() string                 { return c.text }
func (c Claim) Basis() ClaimBasis            { return c.basis }
func (c Claim) Evidence() []handoff.Citation { return slices.Clone(c.evidence) }
func (c Claim) Validate() error {
	if err := validateText("claim", c.text, MaxTextLength); err != nil {
		return err
	}
	if c.basis != Declared && c.basis != Verified {
		return &InvalidError{Field: "claim.basis", Detail: "must be declared or verified"}
	}
	if len(c.evidence) > MaxClaimEvidence {
		return &InvalidError{Field: "claim.evidence", Detail: fmt.Sprintf("must contain at most %d items", MaxClaimEvidence)}
	}
	if err := validateCitations("claim.evidence", c.evidence); err != nil {
		return err
	}
	if c.basis == Verified && len(c.evidence) == 0 {
		return &InvalidError{Field: "claim.evidence", Detail: "verified Work claims require exact evidence"}
	}
	if c.basis == Declared && len(c.evidence) != 0 {
		return &InvalidError{Field: "claim.evidence", Detail: "declared Work claims cannot present evidence as verified"}
	}
	return nil
}

type CreateContract struct {
	SourceID string
	Contract Contract
}

type HandoffCurrent struct {
	SourceID string
	Handoff  CurrentHandoff
}

type RecordOutcome struct {
	SourceID string
	Outcome  TaskOutcome
}

type SourceReceipt struct {
	Kind          Kind
	SourceRef     source.Ref
	Position      int64
	ContentDigest string
}

type PreparedHandoff struct {
	Boundary SourceReceipt
	Handoff  handoff.Prepared
}

type Acknowledgement struct {
	Resolution handoff.Resolution
	Receipt    SourceReceipt
}

func validateClaims(field string, values []Claim, minimum, maximum int) error {
	if len(values) < minimum || len(values) > maximum {
		return &InvalidError{Field: field, Detail: fmt.Sprintf("must contain %d..%d items", minimum, maximum)}
	}
	for _, value := range values {
		if err := value.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func validateTextItems(field string, values []string, minimum, maximum int) error {
	if len(values) < minimum || len(values) > maximum {
		return &InvalidError{Field: field, Detail: fmt.Sprintf("must contain %d..%d items", minimum, maximum)}
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if err := validateText(field, value, MaxTextLength); err != nil {
			return err
		}
		if _, exists := seen[value]; exists {
			return &InvalidError{Field: field, Detail: "items must be unique"}
		}
		seen[value] = struct{}{}
	}
	return nil
}

func validateText(field, value string, maximum int) error {
	trimmed := strings.TrimFunc(value, func(character rune) bool {
		return unicode.IsSpace(character) || character >= '\u001c' && character <= '\u001f'
	})
	if trimmed == "" || trimmed != value {
		return &InvalidError{Field: field, Detail: "must be non-empty and trimmed"}
	}
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > maximum {
		return &InvalidError{Field: field, Detail: fmt.Sprintf("must not exceed %d characters", maximum)}
	}
	return nil
}

func validateCitations(field string, values []handoff.Citation) error {
	for _, citation := range values {
		if citation == nil || (reflect.ValueOf(citation).Kind() == reflect.Pointer && reflect.ValueOf(citation).IsNil()) {
			return &InvalidError{Field: field, Detail: "contains an invalid citation"}
		}
		var err error
		switch value := citation.(type) {
		case handoff.SourceCitation:
			err = value.Validate()
		case handoff.ArtifactCitation:
			err = value.Validate()
		case handoff.MemoryCitation:
			err = value.Validate()
		default:
			return &InvalidError{Field: field, Detail: fmt.Sprintf("contains unsupported citation type %T", citation)}
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func cloneClaim(value *Claim) *Claim {
	if value == nil {
		return nil
	}
	copy := *value
	copy.evidence = slices.Clone(value.evidence)
	return &copy
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneArtifactRef(value *artifact.Ref) *artifact.Ref {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneSourceRef(value *source.Ref) *source.Ref {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneReceiverChecks(value *ReceiverChecks) *ReceiverChecks {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func clonePrepared(value *handoff.Prepared) *handoff.Prepared {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
