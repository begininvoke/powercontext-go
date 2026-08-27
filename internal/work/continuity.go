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
	"encoding/json"
	"fmt"
	"slices"

	"github.com/ob-labs/powercontext-go/artifact"
	"github.com/ob-labs/powercontext-go/source"
)

type EventStatus string

type Event struct {
	position          int64
	kind              Kind
	sourceRef         source.Ref
	recordSchema      string
	status            EventStatus
	summary           *string
	actor             *string
	selectedRevision  *artifact.Ref
	handoffReceiptRef *source.Ref
	receiverChecks    *ReceiverChecks
}

func (e Event) Position() int64                 { return e.position }
func (e Event) Kind() Kind                      { return e.kind }
func (e Event) SourceRef() source.Ref           { return e.sourceRef }
func (e Event) RecordSchema() string            { return e.recordSchema }
func (e Event) Status() EventStatus             { return e.status }
func (e Event) Summary() *string                { return cloneString(e.summary) }
func (e Event) Actor() *string                  { return cloneString(e.actor) }
func (e Event) SelectedRevision() *artifact.Ref { return cloneArtifactRef(e.selectedRevision) }
func (e Event) HandoffReceiptRef() *source.Ref  { return cloneSourceRef(e.handoffReceiptRef) }
func (e Event) ReceiverChecks() *ReceiverChecks { return cloneReceiverChecks(e.receiverChecks) }

type TransferState string

const (
	TransferNotApplicable      TransferState = "not_applicable"
	TransferAwaitingReceipt    TransferState = "awaiting_receipt"
	TransferNeedsClarification TransferState = "needs_clarification"
	TransferDeclined           TransferState = "declined"
	TransferAccepted           TransferState = "accepted"
)

type OutcomeState string

const (
	OutcomeNotExpected OutcomeState = "not_expected"
	OutcomeAwaiting    OutcomeState = "awaiting_outcome"
	OutcomeCovered     OutcomeState = "covered"
)

type Coverage struct {
	contractRecords        int
	handoffRecords         int
	acknowledgementRecords int
	outcomeRecords         int
	transferState          TransferState
	outcomeState           OutcomeState
	activeReceiptRef       *source.Ref
	handoffResultCovered   bool
}

func (c Coverage) ContractRecords() int          { return c.contractRecords }
func (c Coverage) HandoffRecords() int           { return c.handoffRecords }
func (c Coverage) AcknowledgementRecords() int   { return c.acknowledgementRecords }
func (c Coverage) OutcomeRecords() int           { return c.outcomeRecords }
func (c Coverage) TransferState() TransferState  { return c.transferState }
func (c Coverage) OutcomeState() OutcomeState    { return c.outcomeState }
func (c Coverage) ActiveReceiptRef() *source.Ref { return cloneSourceRef(c.activeReceiptRef) }
func (c Coverage) HandoffResultCovered() bool    { return c.handoffResultCovered }

type Continuity struct {
	scopeID            string
	selectedHandoff    *artifact.Ref
	totalEventCount    int
	invalidRecordCount int
	truncated          bool
	events             []Event
	coverage           Coverage
}

func (c Continuity) Schema() string                 { return WorkContinuitySchema }
func (c Continuity) Trust() string                  { return UntrustedHistory }
func (c Continuity) ScopeID() string                { return c.scopeID }
func (c Continuity) SelectedHandoff() *artifact.Ref { return cloneArtifactRef(c.selectedHandoff) }
func (c Continuity) TotalEventCount() int           { return c.totalEventCount }
func (c Continuity) InvalidRecordCount() int        { return c.invalidRecordCount }
func (c Continuity) Truncated() bool                { return c.truncated }
func (c Continuity) Events() []Event                { return slices.Clone(c.events) }
func (c Continuity) Coverage() Coverage             { return c.coverage }

// Validate protects the report-facing continuity projection from malformed
// values. Continuity is assembled by ProjectContinuity, but keeping the
// invariants on the value itself prevents report digests from blessing an
// internally inconsistent projection.
func (c Continuity) Validate() error {
	if err := validateText("continuity.scope_id", c.scopeID, source.MaxIDLength); err != nil {
		return err
	}
	if c.selectedHandoff != nil {
		if err := c.selectedHandoff.Validate(); err != nil {
			return err
		}
	}
	if c.totalEventCount < 0 || c.invalidRecordCount < 0 || c.totalEventCount < len(c.events) {
		return &InvalidError{Field: "continuity", Detail: "contains invalid event counts"}
	}
	if c.truncated != (c.totalEventCount > len(c.events)) {
		return &InvalidError{Field: "continuity.truncated", Detail: "must match omitted continuity events"}
	}
	var previous int64
	for _, event := range c.events {
		if event.position < 1 || event.position <= previous {
			return &InvalidError{Field: "continuity.events", Detail: "positions must be unique and ascending"}
		}
		previous = event.position
		if event.kind.Validate() != nil {
			return &InvalidError{Field: "continuity.events.kind", Detail: "has an unsupported value"}
		}
		if _, err := source.NewRef(event.sourceRef.Type(), event.sourceRef.ID()); err != nil {
			return err
		}
	}
	if c.coverage.handoffResultCovered != (c.coverage.outcomeState == OutcomeCovered) {
		return &InvalidError{Field: "continuity.coverage.handoff_result_covered", Detail: "must match covered outcome state"}
	}
	if c.coverage.transferState != TransferAccepted && c.coverage.outcomeState != OutcomeNotExpected {
		return &InvalidError{Field: "continuity.coverage.outcome_state", Detail: "only an accepted transfer can expect or cover an outcome"}
	}
	if (c.coverage.activeReceiptRef != nil) != (c.coverage.transferState == TransferAccepted) {
		return &InvalidError{Field: "continuity.coverage.active_receipt_ref", Detail: "must identify the accepted transfer"}
	}
	return nil
}

type continuityEventJSON struct {
	Position          int64               `json:"position"`
	Kind              Kind                `json:"kind"`
	SourceRef         sourceRefJSON       `json:"source_ref"`
	RecordSchema      string              `json:"record_schema"`
	Status            EventStatus         `json:"status"`
	Summary           *string             `json:"summary"`
	Actor             *string             `json:"actor"`
	SelectedRevision  *artifactRefJSON    `json:"selected_revision"`
	HandoffReceiptRef *sourceRefJSON      `json:"handoff_receipt_ref"`
	ReceiverChecks    *receiverChecksJSON `json:"receiver_checks"`
}

type continuityCoverageJSON struct {
	ContractRecords        int            `json:"contract_records"`
	HandoffRecords         int            `json:"handoff_records"`
	AcknowledgementRecords int            `json:"acknowledgement_records"`
	OutcomeRecords         int            `json:"outcome_records"`
	TransferState          TransferState  `json:"transfer_state"`
	OutcomeState           OutcomeState   `json:"outcome_state"`
	ActiveReceiptRef       *sourceRefJSON `json:"active_receipt_ref"`
	HandoffResultCovered   bool           `json:"handoff_result_covered"`
}

type continuityJSON struct {
	Schema             string                 `json:"schema"`
	Trust              string                 `json:"trust"`
	ScopeID            string                 `json:"scope_id"`
	SelectedHandoff    *artifactRefJSON       `json:"selected_handoff"`
	TotalEventCount    int                    `json:"total_event_count"`
	InvalidRecordCount int                    `json:"invalid_record_count"`
	Truncated          bool                   `json:"truncated"`
	Events             []continuityEventJSON  `json:"events"`
	Coverage           continuityCoverageJSON `json:"coverage"`
}

// MarshalJSON preserves the Python WorkContinuity wire shape, including
// explicit nulls. It is also the value consumed by Handoff Report JCS digests.
func (c Continuity) MarshalJSON() ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	events := make([]continuityEventJSON, len(c.events))
	for index, event := range c.events {
		var selected *artifactRefJSON
		if event.selectedRevision != nil {
			value := encodeArtifactRef(*event.selectedRevision)
			selected = &value
		}
		var receipt *sourceRefJSON
		if event.handoffReceiptRef != nil {
			value := encodeSourceRef(*event.handoffReceiptRef)
			receipt = &value
		}
		var checks *receiverChecksJSON
		if event.receiverChecks != nil {
			checks = &receiverChecksJSON{
				LiveState: event.receiverChecks.LiveState(), Capability: event.receiverChecks.Capability(),
				Authorization: event.receiverChecks.Authorization(),
			}
		}
		events[index] = continuityEventJSON{
			Position: event.position, Kind: event.kind, SourceRef: encodeSourceRef(event.sourceRef),
			RecordSchema: event.recordSchema, Status: event.status, Summary: cloneString(event.summary),
			Actor: cloneString(event.actor), SelectedRevision: selected, HandoffReceiptRef: receipt,
			ReceiverChecks: checks,
		}
	}
	var selected *artifactRefJSON
	if c.selectedHandoff != nil {
		value := encodeArtifactRef(*c.selectedHandoff)
		selected = &value
	}
	var active *sourceRefJSON
	if c.coverage.activeReceiptRef != nil {
		value := encodeSourceRef(*c.coverage.activeReceiptRef)
		active = &value
	}
	payload := continuityJSON{
		Schema: WorkContinuitySchema, Trust: UntrustedHistory, ScopeID: c.scopeID,
		SelectedHandoff: selected, TotalEventCount: c.totalEventCount,
		InvalidRecordCount: c.invalidRecordCount, Truncated: c.truncated, Events: events,
		Coverage: continuityCoverageJSON{
			ContractRecords: c.coverage.contractRecords, HandoffRecords: c.coverage.handoffRecords,
			AcknowledgementRecords: c.coverage.acknowledgementRecords, OutcomeRecords: c.coverage.outcomeRecords,
			TransferState: c.coverage.transferState, OutcomeState: c.coverage.outcomeState,
			ActiveReceiptRef: active, HandoffResultCovered: c.coverage.handoffResultCovered,
		},
	}
	return marshalCompatibleJSON(payload, false)
}

var _ json.Marshaler = Continuity{}

// ProjectContinuity deterministically projects only valid high-level Work
// records. Ordinary Sources are ignored and malformed Work Sources are counted
// without making report generation fail.
func ProjectContinuity(scopeID string, entries []source.JournalEntry, selectedHandoff *artifact.Ref) (Continuity, error) {
	if err := validateText("continuity.scope_id", scopeID, source.MaxIDLength); err != nil {
		return Continuity{}, err
	}
	if selectedHandoff != nil {
		if err := selectedHandoff.Validate(); err != nil {
			return Continuity{}, err
		}
	}
	events := make([]Event, 0)
	invalid := 0
	for _, entry := range entries {
		content, ok := entry.Value().(source.ContentSource)
		if !ok {
			continue
		}
		kindValue, ok := content.Metadata()["kind"].(string)
		if !ok {
			continue
		}
		kind := Kind(kindValue)
		if kind.Validate() != nil {
			continue
		}
		record, err := DecodeRecord(kind, []byte(content.Content()))
		if err != nil {
			invalid++
			continue
		}
		event, err := eventFromRecord(entry, kind, record)
		if err != nil {
			return Continuity{}, err
		}
		events = append(events, event)
	}
	coverage := projectCoverage(events, selectedHandoff)
	projected := events
	if len(projected) > MaxContinuityEvents {
		projected = projected[len(projected)-MaxContinuityEvents:]
	}
	return Continuity{
		scopeID: scopeID, selectedHandoff: cloneArtifactRef(selectedHandoff), totalEventCount: len(events),
		invalidRecordCount: invalid, truncated: len(events) > len(projected), events: slices.Clone(projected), coverage: coverage,
	}, nil
}

func eventFromRecord(entry source.JournalEntry, kind Kind, record any) (Event, error) {
	event := Event{position: entry.Position(), kind: kind, sourceRef: entry.Ref()}
	switch value := record.(type) {
	case Contract:
		event.recordSchema = WorkContractSchema
		event.status = "delegated"
		event.summary = stringPointer(value.Objective())
	case CurrentHandoff:
		event.recordSchema = CurrentWorkHandoffSchema
		event.status = EventStatus(value.Disposition())
		event.summary = stringPointer(value.Objective())
	case HandoffReceipt:
		event.recordSchema = HandoffReceiptSchema
		event.status = EventStatus(value.Status())
		event.summary = value.Message()
		event.actor = stringPointer(value.Receiver())
		event.selectedRevision = value.SelectedRevision()
		event.receiverChecks = value.ReceiverChecks()
	case TaskOutcome:
		event.recordSchema = TaskOutcomeSchema
		event.status = EventStatus(value.Status())
		event.summary = stringPointer(value.Summary())
		event.handoffReceiptRef = value.HandoffReceiptRef()
	default:
		return Event{}, fmt.Errorf("unsupported Work continuity record %T", record)
	}
	return event, nil
}

func projectCoverage(events []Event, selectedHandoff *artifact.Ref) Coverage {
	coverage := Coverage{transferState: TransferNotApplicable, outcomeState: OutcomeNotExpected}
	var matching []Event
	for _, event := range events {
		switch event.kind {
		case WorkContractSourceKind:
			coverage.contractRecords++
		case HandoffBoundarySourceKind:
			coverage.handoffRecords++
		case HandoffReceiptSourceKind:
			coverage.acknowledgementRecords++
			if selectedHandoff != nil && event.selectedRevision != nil && *event.selectedRevision == *selectedHandoff {
				matching = append(matching, event)
			}
		case TaskOutcomeSourceKind:
			coverage.outcomeRecords++
		}
	}
	if selectedHandoff == nil {
		return coverage
	}
	if len(matching) == 0 {
		coverage.transferState = TransferAwaitingReceipt
		return coverage
	}
	latest := matching[0]
	for _, event := range matching[1:] {
		if event.position > latest.position {
			latest = event
		}
	}
	switch latest.status {
	case EventStatus(ReceiptNeedsClarification):
		coverage.transferState = TransferNeedsClarification
	case EventStatus(ReceiptDeclined):
		coverage.transferState = TransferDeclined
	case EventStatus(ReceiptAccepted):
		coverage.transferState = TransferAccepted
		coverage.outcomeState = OutcomeAwaiting
		coverage.activeReceiptRef = cloneSourceRef(&latest.sourceRef)
		for _, event := range events {
			if event.kind == TaskOutcomeSourceKind && event.position > latest.position &&
				event.handoffReceiptRef != nil && *event.handoffReceiptRef == latest.sourceRef {
				coverage.outcomeState = OutcomeCovered
				coverage.handoffResultCovered = true
				break
			}
		}
	}
	return coverage
}

func stringPointer(value string) *string { return &value }
