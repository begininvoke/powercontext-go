package work

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"

	"github.com/ob-labs/powercontext-go/artifact"
	"github.com/ob-labs/powercontext-go/artifact/handoff"
	"github.com/ob-labs/powercontext-go/artifact/memory"
	"github.com/ob-labs/powercontext-go/source"
)

type sourceRefJSON struct {
	SourceType string `json:"source_type"`
	SourceID   string `json:"source_id"`
}

type artifactRefJSON struct {
	Family     string `json:"family"`
	ArtifactID string `json:"artifact_id"`
	Revision   int    `json:"revision"`
}

type memoryCitationJSON struct {
	MemoryRef      artifactRefJSON `json:"memory_ref"`
	EntryID        string          `json:"entry_id"`
	EntryVersionID string          `json:"entry_version_id"`
}

type sourceCitationJSON struct {
	Kind      string        `json:"kind"`
	SourceRef sourceRefJSON `json:"source_ref"`
}

type artifactCitationJSON struct {
	Kind        string          `json:"kind"`
	ArtifactRef artifactRefJSON `json:"artifact_ref"`
}

type memoryHandoffCitationJSON struct {
	Kind           string             `json:"kind"`
	MemoryCitation memoryCitationJSON `json:"memory_citation"`
}

type claimJSON struct {
	Text     string            `json:"text"`
	Basis    ClaimBasis        `json:"basis"`
	Evidence []json.RawMessage `json:"evidence"`
}

type contractJSON struct {
	Schema             string      `json:"schema"`
	Trust              string      `json:"trust"`
	Objective          string      `json:"objective"`
	Facts              []claimJSON `json:"facts"`
	InScope            []string    `json:"in_scope"`
	Exclusions         []string    `json:"exclusions"`
	CompletionCriteria []string    `json:"completion_criteria"`
	AuthorizationNotes []string    `json:"authorization_notes"`
	OpenQuestions      []string    `json:"open_questions"`
}

type currentHandoffJSON struct {
	Schema      string              `json:"schema"`
	Trust       string              `json:"trust"`
	Objective   string              `json:"objective"`
	State       []claimJSON         `json:"state"`
	Disposition handoff.Disposition `json:"disposition"`
	NextAction  *claimJSON          `json:"next_action"`
	Omissions   []string            `json:"omissions"`
}

type taskCheckJSON struct {
	Name     string            `json:"name"`
	Status   CheckStatus       `json:"status"`
	Details  *string           `json:"details"`
	Basis    ClaimBasis        `json:"basis"`
	Evidence []json.RawMessage `json:"evidence"`
}

type taskOutcomeJSON struct {
	Schema            string            `json:"schema"`
	Trust             string            `json:"trust"`
	Objective         string            `json:"objective"`
	Status            OutcomeStatus     `json:"status"`
	Summary           string            `json:"summary"`
	HandoffReceiptRef *sourceRefJSON    `json:"handoff_receipt_ref"`
	Observations      []claimJSON       `json:"observations"`
	Checks            []taskCheckJSON   `json:"checks"`
	ProducedArtifacts []artifactRefJSON `json:"produced_artifacts"`
	RemainingWork     []string          `json:"remaining_work"`
}

type receiverChecksJSON struct {
	LiveState     LiveStateCheckStatus `json:"live_state"`
	Capability    ReadinessCheckStatus `json:"capability"`
	Authorization ReadinessCheckStatus `json:"authorization"`
}

type handoffReceiptJSON struct {
	Schema              string              `json:"schema"`
	Trust               string              `json:"trust"`
	Receiver            string              `json:"receiver"`
	Status              ReceiptStatus       `json:"status"`
	Selection           handoff.Selection   `json:"selection"`
	SelectedRevision    *artifactRefJSON    `json:"selected_revision"`
	PreparedDigest      *string             `json:"prepared_digest"`
	ReceiverChecks      *receiverChecksJSON `json:"receiver_checks"`
	EvidenceStatus      EvidenceStatus      `json:"evidence_status"`
	UnavailableEvidence []json.RawMessage   `json:"unavailable_evidence"`
	Message             *string             `json:"message"`
}

type handoffStatementJSON struct {
	Text      string            `json:"text"`
	Citations []json.RawMessage `json:"citations"`
}

type handoffOmissionJSON struct {
	Text     string           `json:"text"`
	Citation *json.RawMessage `json:"citation"`
}

type handoffContentJSON struct {
	Schema      string                 `json:"schema"`
	Objective   string                 `json:"objective"`
	State       []handoffStatementJSON `json:"state"`
	Disposition handoff.Disposition    `json:"disposition"`
	NextAction  *handoffStatementJSON  `json:"next_action"`
	Omissions   []handoffOmissionJSON  `json:"omissions"`
}

type preparedHandoffJSON struct {
	Schema  string             `json:"schema"`
	ScopeID string             `json:"scope_id"`
	Base    *artifactRefJSON   `json:"base"`
	Content handoffContentJSON `json:"content"`
}

// EncodeRecord produces the exact schema-specific JSON captured by the Python
// runtime. pretty controls only the two-space Source representation; digests
// always use the compact representation.
func EncodeRecord(value any, pretty bool) ([]byte, error) {
	var encoded any
	var err error
	switch record := value.(type) {
	case Contract:
		encoded, err = encodeContract(record)
	case CurrentHandoff:
		encoded, err = encodeCurrentHandoff(record)
	case TaskOutcome:
		encoded, err = encodeTaskOutcome(record)
	case HandoffReceipt:
		encoded, err = encodeHandoffReceipt(record)
	default:
		return nil, &InvalidError{Field: "record", Detail: fmt.Sprintf("unsupported type %T", value)}
	}
	if err != nil {
		return nil, err
	}
	return marshalCompatibleJSON(encoded, pretty)
}

// DecodeRecord strictly decodes one Source record selected by its trusted
// metadata kind. Unknown fields and mismatched schema/trust constants make the
// record invalid rather than silently expanding history.
func DecodeRecord(kind Kind, payload []byte) (any, error) {
	switch kind {
	case WorkContractSourceKind:
		var value contractJSON
		if err := decodeStrict(payload, &value); err != nil {
			return nil, err
		}
		return decodeContract(value)
	case HandoffBoundarySourceKind:
		var value currentHandoffJSON
		if err := decodeStrict(payload, &value); err != nil {
			return nil, err
		}
		return decodeCurrentHandoff(value)
	case HandoffReceiptSourceKind:
		var value handoffReceiptJSON
		if err := decodeStrict(payload, &value); err != nil {
			return nil, err
		}
		return decodeHandoffReceipt(value)
	case TaskOutcomeSourceKind:
		var value taskOutcomeJSON
		if err := decodeStrict(payload, &value); err != nil {
			return nil, err
		}
		return decodeTaskOutcome(value)
	default:
		return nil, &InvalidError{Field: "kind", Detail: "unsupported record kind"}
	}
}

func ContentDigest(value any) (string, error) {
	payload, err := EncodeRecord(value, false)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func PreparedDigest(value handoff.Prepared) (string, error) {
	payload, err := encodePreparedHandoff(value)
	if err != nil {
		return "", err
	}
	encoded, err := marshalCompatibleJSON(payload, false)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

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

func encodeHandoffReceipt(value HandoffReceipt) (handoffReceiptJSON, error) {
	if err := value.Validate(); err != nil {
		return handoffReceiptJSON{}, err
	}
	var selected *artifactRefJSON
	if value.selectedRevision != nil {
		encoded := encodeArtifactRef(*value.selectedRevision)
		selected = &encoded
	}
	var checks *receiverChecksJSON
	if value.receiverChecks != nil {
		checks = &receiverChecksJSON{
			LiveState: value.receiverChecks.liveState, Capability: value.receiverChecks.capability,
			Authorization: value.receiverChecks.authorization,
		}
	}
	evidence, err := encodeCitations(value.unavailableEvidence)
	if err != nil {
		return handoffReceiptJSON{}, err
	}
	return handoffReceiptJSON{
		Schema: HandoffReceiptSchema, Trust: UntrustedObservation, Receiver: value.receiver,
		Status: value.status, Selection: value.selection, SelectedRevision: selected,
		PreparedDigest: cloneString(value.preparedDigest), ReceiverChecks: checks,
		EvidenceStatus: value.evidenceStatus, UnavailableEvidence: evidence, Message: cloneString(value.message),
	}, nil
}

func decodeHandoffReceipt(value handoffReceiptJSON) (HandoffReceipt, error) {
	if value.Schema != HandoffReceiptSchema || value.Trust != UntrustedObservation {
		return HandoffReceipt{}, &InvalidError{Field: "receipt.schema", Detail: "does not match the Handoff receipt"}
	}
	var selected *artifact.Ref
	if value.SelectedRevision != nil {
		decoded, err := decodeArtifactRef(*value.SelectedRevision)
		if err != nil {
			return HandoffReceipt{}, err
		}
		selected = &decoded
	}
	var checks *ReceiverChecks
	if value.ReceiverChecks != nil {
		decoded, err := NewReceiverChecks(value.ReceiverChecks.LiveState, value.ReceiverChecks.Capability, value.ReceiverChecks.Authorization)
		if err != nil {
			return HandoffReceipt{}, err
		}
		checks = &decoded
	}
	evidence, err := decodeCitations(value.UnavailableEvidence)
	if err != nil {
		return HandoffReceipt{}, err
	}
	return NewHandoffReceipt(
		value.Receiver, value.Status, value.Selection, selected, value.PreparedDigest,
		checks, value.EvidenceStatus, evidence, value.Message,
	)
}

func encodeClaims(values []Claim) ([]claimJSON, error) {
	result := make([]claimJSON, len(values))
	for index, value := range values {
		encoded, err := encodeClaim(value)
		if err != nil {
			return nil, err
		}
		result[index] = encoded
	}
	return result, nil
}

func encodeClaim(value Claim) (claimJSON, error) {
	if err := value.Validate(); err != nil {
		return claimJSON{}, err
	}
	evidence, err := encodeCitations(value.evidence)
	if err != nil {
		return claimJSON{}, err
	}
	return claimJSON{Text: value.text, Basis: value.basis, Evidence: evidence}, nil
}

func decodeClaims(values []claimJSON) ([]Claim, error) {
	result := make([]Claim, len(values))
	for index, value := range values {
		decoded, err := decodeClaim(value)
		if err != nil {
			return nil, err
		}
		result[index] = decoded
	}
	return result, nil
}

func decodeClaim(value claimJSON) (Claim, error) {
	evidence, err := decodeCitations(value.Evidence)
	if err != nil {
		return Claim{}, err
	}
	return NewClaim(value.Text, value.Basis, evidence)
}

func encodeCitations(values []handoff.Citation) ([]json.RawMessage, error) {
	result := make([]json.RawMessage, len(values))
	for index, value := range values {
		encoded, err := encodeCitation(value)
		if err != nil {
			return nil, err
		}
		result[index] = encoded
	}
	return result, nil
}

func encodeCitation(value handoff.Citation) (json.RawMessage, error) {
	var payload any
	switch citation := value.(type) {
	case handoff.SourceCitation:
		payload = sourceCitationJSON{Kind: string(handoff.SourceCitationKind), SourceRef: encodeSourceRef(citation.Ref())}
	case handoff.ArtifactCitation:
		payload = artifactCitationJSON{Kind: string(handoff.ArtifactCitationKind), ArtifactRef: encodeArtifactRef(citation.Ref())}
	case handoff.MemoryCitation:
		memoryValue := citation.Citation()
		payload = memoryHandoffCitationJSON{
			Kind: string(handoff.MemoryCitationKind),
			MemoryCitation: memoryCitationJSON{
				MemoryRef: encodeArtifactRef(memoryValue.MemoryRef), EntryID: memoryValue.EntryID,
				EntryVersionID: memoryValue.EntryVersionID,
			},
		}
	default:
		return nil, &InvalidError{Field: "citation", Detail: fmt.Sprintf("unsupported type %T", value)}
	}
	return marshalCompatibleJSON(payload, false)
}

func decodeCitations(values []json.RawMessage) ([]handoff.Citation, error) {
	result := make([]handoff.Citation, len(values))
	for index, value := range values {
		decoded, err := decodeCitation(value)
		if err != nil {
			return nil, err
		}
		result[index] = decoded
	}
	return result, nil
}

func decodeCitation(payload []byte) (handoff.Citation, error) {
	var discriminator struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(payload, &discriminator); err != nil {
		return nil, err
	}
	switch handoff.CitationKind(discriminator.Kind) {
	case handoff.SourceCitationKind:
		var value sourceCitationJSON
		if err := decodeStrict(payload, &value); err != nil {
			return nil, err
		}
		ref, err := decodeSourceRef(value.SourceRef)
		if err != nil {
			return nil, err
		}
		return handoff.NewSourceCitation(ref)
	case handoff.ArtifactCitationKind:
		var value artifactCitationJSON
		if err := decodeStrict(payload, &value); err != nil {
			return nil, err
		}
		ref, err := decodeArtifactRef(value.ArtifactRef)
		if err != nil {
			return nil, err
		}
		return handoff.NewArtifactCitation(ref)
	case handoff.MemoryCitationKind:
		var value memoryHandoffCitationJSON
		if err := decodeStrict(payload, &value); err != nil {
			return nil, err
		}
		ref, err := decodeArtifactRef(value.MemoryCitation.MemoryRef)
		if err != nil {
			return nil, err
		}
		return handoff.NewMemoryCitation(memory.Citation{
			MemoryRef: ref, EntryID: value.MemoryCitation.EntryID, EntryVersionID: value.MemoryCitation.EntryVersionID,
		})
	default:
		return nil, &InvalidError{Field: "citation.kind", Detail: "has an unsupported value"}
	}
}

func encodePreparedHandoff(value handoff.Prepared) (preparedHandoffJSON, error) {
	if err := value.Validate(); err != nil {
		return preparedHandoffJSON{}, err
	}
	content, err := encodeHandoffContent(value.Content())
	if err != nil {
		return preparedHandoffJSON{}, err
	}
	var base *artifactRefJSON
	if value.Base() != nil {
		encoded := encodeArtifactRef(*value.Base())
		base = &encoded
	}
	return preparedHandoffJSON{
		Schema: handoff.PreparedSchemaVersion, ScopeID: value.ScopeID(), Base: base, Content: content,
	}, nil
}

func encodeHandoffContent(value handoff.Content) (handoffContentJSON, error) {
	if err := value.Validate(); err != nil {
		return handoffContentJSON{}, err
	}
	state := make([]handoffStatementJSON, len(value.State()))
	for index, statement := range value.State() {
		encoded, err := encodeHandoffStatement(statement)
		if err != nil {
			return handoffContentJSON{}, err
		}
		state[index] = encoded
	}
	var next *handoffStatementJSON
	if value.NextAction() != nil {
		encoded, err := encodeHandoffStatement(*value.NextAction())
		if err != nil {
			return handoffContentJSON{}, err
		}
		next = &encoded
	}
	omissions := make([]handoffOmissionJSON, len(value.Omissions()))
	for index, omission := range value.Omissions() {
		var citation *json.RawMessage
		if omission.Citation() != nil {
			encoded, err := encodeCitation(omission.Citation())
			if err != nil {
				return handoffContentJSON{}, err
			}
			citation = &encoded
		}
		omissions[index] = handoffOmissionJSON{Text: omission.Text(), Citation: citation}
	}
	return handoffContentJSON{
		Schema: handoff.ContentSchemaVersion, Objective: value.Objective(), State: state,
		Disposition: value.Disposition(), NextAction: next, Omissions: omissions,
	}, nil
}

func encodeHandoffStatement(value handoff.Statement) (handoffStatementJSON, error) {
	citations, err := encodeCitations(value.Citations())
	if err != nil {
		return handoffStatementJSON{}, err
	}
	return handoffStatementJSON{Text: value.Text(), Citations: citations}, nil
}

func encodeSourceRef(value source.Ref) sourceRefJSON {
	return sourceRefJSON{SourceType: value.Type(), SourceID: value.ID()}
}

func decodeSourceRef(value sourceRefJSON) (source.Ref, error) {
	return source.NewRef(value.SourceType, value.SourceID)
}

func encodeArtifactRef(value artifact.Ref) artifactRefJSON {
	return artifactRefJSON{Family: value.Family(), ArtifactID: value.ID(), Revision: int(value.Revision())}
}

func decodeArtifactRef(value artifactRefJSON) (artifact.Ref, error) {
	return artifact.NewRef(value.Family, value.ArtifactID, int64(value.Revision))
}

func decodeStrict(payload []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		if err == nil {
			return fmt.Errorf("unexpected trailing JSON value")
		}
		return err
	}
	return nil
}

func marshalCompatibleJSON(value any, pretty bool) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if pretty {
		encoder.SetIndent("", "  ")
	}
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	encoded := bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'})
	// encoding/json deliberately escapes these two code points for JavaScript.
	// Pydantic emits UTF-8, and Source idempotency compares exact bytes.
	encoded = bytes.ReplaceAll(encoded, []byte(`\u2028`), []byte("\u2028"))
	encoded = bytes.ReplaceAll(encoded, []byte(`\u2029`), []byte("\u2029"))
	return encoded, nil
}

func nonNil[T any](values []T) []T {
	result := make([]T, len(values))
	copy(result, values)
	return result
}
