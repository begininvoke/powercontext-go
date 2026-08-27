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

type Contract struct {
	objective          string
	facts              []Claim
	inScope            []string
	exclusions         []string
	completionCriteria []string
	authorizationNotes []string
	openQuestions      []string
}

func NewContract(
	objective string,
	facts []Claim,
	inScope, exclusions, completionCriteria, authorizationNotes, openQuestions []string,
) (Contract, error) {
	value := Contract{
		objective: objective, facts: slices.Clone(facts), inScope: slices.Clone(inScope),
		exclusions: slices.Clone(exclusions), completionCriteria: slices.Clone(completionCriteria),
		authorizationNotes: slices.Clone(authorizationNotes), openQuestions: slices.Clone(openQuestions),
	}
	if err := value.Validate(); err != nil {
		return Contract{}, err
	}
	return value, nil
}

func (c Contract) Objective() string            { return c.objective }
func (c Contract) Facts() []Claim               { return slices.Clone(c.facts) }
func (c Contract) InScope() []string            { return slices.Clone(c.inScope) }
func (c Contract) Exclusions() []string         { return slices.Clone(c.exclusions) }
func (c Contract) CompletionCriteria() []string { return slices.Clone(c.completionCriteria) }
func (c Contract) AuthorizationNotes() []string { return slices.Clone(c.authorizationNotes) }
func (c Contract) OpenQuestions() []string      { return slices.Clone(c.openQuestions) }
func (c Contract) Schema() string               { return WorkContractSchema }
func (c Contract) Trust() string                { return UntrustedInput }
func (c Contract) Validate() error {
	if err := validateText("contract.objective", c.objective, MaxTextLength); err != nil {
		return err
	}
	if err := validateClaims("contract.facts", c.facts, 0, MaxItems); err != nil {
		return err
	}
	for _, values := range []struct {
		name    string
		values  []string
		minimum int
	}{
		{"contract.in_scope", c.inScope, 1},
		{"contract.exclusions", c.exclusions, 0},
		{"contract.completion_criteria", c.completionCriteria, 1},
		{"contract.authorization_notes", c.authorizationNotes, 0},
		{"contract.open_questions", c.openQuestions, 0},
	} {
		if err := validateTextItems(values.name, values.values, values.minimum, MaxItems); err != nil {
			return err
		}
	}
	return nil
}

type CurrentHandoff struct {
	objective   string
	state       []Claim
	disposition handoff.Disposition
	nextAction  *Claim
	omissions   []string
}

func NewCurrentHandoff(
	objective string,
	state []Claim,
	disposition handoff.Disposition,
	nextAction *Claim,
	omissions []string,
) (CurrentHandoff, error) {
	value := CurrentHandoff{
		objective: objective, state: slices.Clone(state), disposition: disposition,
		nextAction: cloneClaim(nextAction), omissions: slices.Clone(omissions),
	}
	if err := value.Validate(); err != nil {
		return CurrentHandoff{}, err
	}
	return value, nil
}

func (h CurrentHandoff) Schema() string                   { return CurrentWorkHandoffSchema }
func (h CurrentHandoff) Trust() string                    { return UntrustedInput }
func (h CurrentHandoff) Objective() string                { return h.objective }
func (h CurrentHandoff) State() []Claim                   { return slices.Clone(h.state) }
func (h CurrentHandoff) Disposition() handoff.Disposition { return h.disposition }
func (h CurrentHandoff) NextAction() *Claim               { return cloneClaim(h.nextAction) }
func (h CurrentHandoff) Omissions() []string              { return slices.Clone(h.omissions) }
func (h CurrentHandoff) Validate() error {
	if err := validateText("handoff.objective", h.objective, MaxTextLength); err != nil {
		return err
	}
	if err := validateClaims("handoff.state", h.state, 1, MaxItems); err != nil {
		return err
	}
	if h.disposition != handoff.Continuable && h.disposition != handoff.Blocked && h.disposition != handoff.Complete {
		return &InvalidError{Field: "handoff.disposition", Detail: "has an unsupported value"}
	}
	if h.nextAction != nil {
		if err := h.nextAction.Validate(); err != nil {
			return err
		}
	}
	return validateTextItems("handoff.omissions", h.omissions, 0, MaxItems)
}

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

type ReceiptStatus string

const (
	ReceiptAccepted           ReceiptStatus = "accepted"
	ReceiptNeedsClarification ReceiptStatus = "needs_clarification"
	ReceiptDeclined           ReceiptStatus = "declined"
)

type LiveStateCheckStatus string

const (
	LiveStateConfirmed  LiveStateCheckStatus = "confirmed"
	LiveStateMismatch   LiveStateCheckStatus = "mismatch"
	LiveStateNotChecked LiveStateCheckStatus = "not_checked"
)

type ReadinessCheckStatus string

const (
	ReadinessConfirmed    ReadinessCheckStatus = "confirmed"
	ReadinessInsufficient ReadinessCheckStatus = "insufficient"
	ReadinessNotChecked   ReadinessCheckStatus = "not_checked"
)

type ReceiverChecks struct {
	liveState     LiveStateCheckStatus
	capability    ReadinessCheckStatus
	authorization ReadinessCheckStatus
}

func NewReceiverChecks(live LiveStateCheckStatus, capability, authorization ReadinessCheckStatus) (ReceiverChecks, error) {
	value := ReceiverChecks{liveState: live, capability: capability, authorization: authorization}
	if err := value.Validate(); err != nil {
		return ReceiverChecks{}, err
	}
	return value, nil
}

func (c ReceiverChecks) LiveState() LiveStateCheckStatus     { return c.liveState }
func (c ReceiverChecks) Capability() ReadinessCheckStatus    { return c.capability }
func (c ReceiverChecks) Authorization() ReadinessCheckStatus { return c.authorization }
func (c ReceiverChecks) AllConfirmed() bool {
	return c.liveState == LiveStateConfirmed && c.capability == ReadinessConfirmed && c.authorization == ReadinessConfirmed
}
func (c ReceiverChecks) Validate() error {
	if c.liveState != LiveStateConfirmed && c.liveState != LiveStateMismatch && c.liveState != LiveStateNotChecked {
		return &InvalidError{Field: "receiver_checks.live_state", Detail: "has an unsupported value"}
	}
	for name, value := range map[string]ReadinessCheckStatus{
		"receiver_checks.capability": c.capability, "receiver_checks.authorization": c.authorization,
	} {
		if value != ReadinessConfirmed && value != ReadinessInsufficient && value != ReadinessNotChecked {
			return &InvalidError{Field: name, Detail: "has an unsupported value"}
		}
	}
	return nil
}

type Acknowledge struct {
	sourceID       string
	receiver       string
	status         ReceiptStatus
	selection      handoff.Selection
	receiverChecks *ReceiverChecks
	prepared       *handoff.Prepared
	revision       *artifact.Ref
	message        *string
}

func NewAcknowledge(
	sourceID, receiver string,
	status ReceiptStatus,
	selection handoff.Selection,
	receiverChecks *ReceiverChecks,
	prepared *handoff.Prepared,
	revision *artifact.Ref,
	message *string,
) (Acknowledge, error) {
	value := Acknowledge{
		sourceID: sourceID, receiver: receiver, status: status, selection: selection,
		receiverChecks: cloneReceiverChecks(receiverChecks), prepared: clonePrepared(prepared),
		revision: cloneArtifactRef(revision), message: cloneString(message),
	}
	if err := value.Validate(); err != nil {
		return Acknowledge{}, err
	}
	return value, nil
}

func (a Acknowledge) SourceID() string                { return a.sourceID }
func (a Acknowledge) Receiver() string                { return a.receiver }
func (a Acknowledge) Status() ReceiptStatus           { return a.status }
func (a Acknowledge) Selection() handoff.Selection    { return a.selection }
func (a Acknowledge) ReceiverChecks() *ReceiverChecks { return cloneReceiverChecks(a.receiverChecks) }
func (a Acknowledge) Prepared() *handoff.Prepared     { return clonePrepared(a.prepared) }
func (a Acknowledge) Revision() *artifact.Ref         { return cloneArtifactRef(a.revision) }
func (a Acknowledge) Message() *string                { return cloneString(a.message) }
func (a Acknowledge) Validate() error {
	if err := validateText("acknowledgement.source_id", a.sourceID, source.MaxIDLength); err != nil {
		return err
	}
	if err := validateText("acknowledgement.receiver", a.receiver, source.MaxIDLength); err != nil {
		return err
	}
	if a.status != ReceiptAccepted && a.status != ReceiptNeedsClarification && a.status != ReceiptDeclined {
		return &InvalidError{Field: "acknowledgement.status", Detail: "has an unsupported value"}
	}
	if a.selection == handoff.PreparedSelection {
		if a.prepared == nil || a.revision != nil {
			return &InvalidError{Field: "acknowledgement.selection", Detail: "does not match its exact input"}
		}
		if err := a.prepared.Validate(); err != nil {
			return err
		}
	} else if a.selection == handoff.ExactSelection {
		if a.prepared != nil || a.revision == nil {
			return &InvalidError{Field: "acknowledgement.selection", Detail: "does not match its exact input"}
		}
		if err := a.revision.Validate(); err != nil {
			return err
		}
	} else {
		return &InvalidError{Field: "acknowledgement.selection", Detail: "must be prepared or exact"}
	}
	if a.receiverChecks != nil {
		if err := a.receiverChecks.Validate(); err != nil {
			return err
		}
	}
	if a.status == ReceiptAccepted && (a.receiverChecks == nil || !a.receiverChecks.AllConfirmed()) {
		return &InvalidError{Field: "acknowledgement.receiver_checks", Detail: "accepted Handoff acknowledgement requires all receiver checks"}
	}
	if a.status != ReceiptAccepted && a.message == nil {
		return &InvalidError{Field: "acknowledgement.message", Detail: "non-accepted Handoff acknowledgement requires a message"}
	}
	if a.message != nil {
		return validateText("acknowledgement.message", *a.message, MaxTextLength)
	}
	return nil
}

type EvidenceStatus string

const (
	EvidenceAvailable   EvidenceStatus = "available"
	EvidenceUnavailable EvidenceStatus = "unavailable"
)

type HandoffReceipt struct {
	receiver            string
	status              ReceiptStatus
	selection           handoff.Selection
	selectedRevision    *artifact.Ref
	preparedDigest      *string
	receiverChecks      *ReceiverChecks
	evidenceStatus      EvidenceStatus
	unavailableEvidence []handoff.Citation
	message             *string
}

func NewHandoffReceipt(
	receiver string,
	status ReceiptStatus,
	selection handoff.Selection,
	selectedRevision *artifact.Ref,
	preparedDigest *string,
	receiverChecks *ReceiverChecks,
	evidenceStatus EvidenceStatus,
	unavailableEvidence []handoff.Citation,
	message *string,
) (HandoffReceipt, error) {
	value := HandoffReceipt{
		receiver: receiver, status: status, selection: selection,
		selectedRevision: cloneArtifactRef(selectedRevision), preparedDigest: cloneString(preparedDigest),
		receiverChecks: cloneReceiverChecks(receiverChecks), evidenceStatus: evidenceStatus,
		unavailableEvidence: slices.Clone(unavailableEvidence), message: cloneString(message),
	}
	if err := value.Validate(); err != nil {
		return HandoffReceipt{}, err
	}
	return value, nil
}

func (r HandoffReceipt) Schema() string                  { return HandoffReceiptSchema }
func (r HandoffReceipt) Trust() string                   { return UntrustedObservation }
func (r HandoffReceipt) Receiver() string                { return r.receiver }
func (r HandoffReceipt) Status() ReceiptStatus           { return r.status }
func (r HandoffReceipt) Selection() handoff.Selection    { return r.selection }
func (r HandoffReceipt) SelectedRevision() *artifact.Ref { return cloneArtifactRef(r.selectedRevision) }
func (r HandoffReceipt) PreparedDigest() *string         { return cloneString(r.preparedDigest) }
func (r HandoffReceipt) ReceiverChecks() *ReceiverChecks {
	return cloneReceiverChecks(r.receiverChecks)
}
func (r HandoffReceipt) EvidenceStatus() EvidenceStatus { return r.evidenceStatus }
func (r HandoffReceipt) UnavailableEvidence() []handoff.Citation {
	return slices.Clone(r.unavailableEvidence)
}
func (r HandoffReceipt) Message() *string { return cloneString(r.message) }
func (r HandoffReceipt) Validate() error {
	if err := validateText("receipt.receiver", r.receiver, source.MaxIDLength); err != nil {
		return err
	}
	if r.status != ReceiptAccepted && r.status != ReceiptNeedsClarification && r.status != ReceiptDeclined {
		return &InvalidError{Field: "receipt.status", Detail: "has an unsupported value"}
	}
	if r.selection == handoff.PreparedSelection {
		if r.selectedRevision != nil || r.preparedDigest == nil {
			return &InvalidError{Field: "receipt.selection", Detail: "must preserve its exact resolved target"}
		}
	} else if r.selection == handoff.ExactSelection {
		if r.selectedRevision == nil || r.preparedDigest != nil {
			return &InvalidError{Field: "receipt.selection", Detail: "must preserve its exact resolved target"}
		}
		if err := r.selectedRevision.Validate(); err != nil {
			return err
		}
	} else {
		return &InvalidError{Field: "receipt.selection", Detail: "must be prepared or exact"}
	}
	if r.preparedDigest != nil {
		if err := validateText("receipt.prepared_digest", *r.preparedDigest, 128); err != nil {
			return err
		}
	}
	if r.receiverChecks != nil {
		if err := r.receiverChecks.Validate(); err != nil {
			return err
		}
	}
	if len(r.unavailableEvidence) > MaxReceiptEvidence {
		return &InvalidError{Field: "receipt.unavailable_evidence", Detail: "contains too many items"}
	}
	if err := validateCitations("receipt.unavailable_evidence", r.unavailableEvidence); err != nil {
		return err
	}
	if r.evidenceStatus == EvidenceAvailable && len(r.unavailableEvidence) != 0 {
		return &InvalidError{Field: "receipt.unavailable_evidence", Detail: "available receipt cannot contain unavailable evidence"}
	}
	if r.evidenceStatus == EvidenceUnavailable && len(r.unavailableEvidence) == 0 {
		return &InvalidError{Field: "receipt.unavailable_evidence", Detail: "unavailable receipt must identify unavailable evidence"}
	}
	if r.evidenceStatus != EvidenceAvailable && r.evidenceStatus != EvidenceUnavailable {
		return &InvalidError{Field: "receipt.evidence_status", Detail: "has an unsupported value"}
	}
	if r.status == ReceiptAccepted && r.evidenceStatus == EvidenceUnavailable {
		return &InvalidError{Field: "receipt.status", Detail: "a Handoff with unavailable evidence cannot be accepted"}
	}
	if r.status == ReceiptAccepted && r.receiverChecks != nil && !r.receiverChecks.AllConfirmed() {
		return &InvalidError{Field: "receipt.receiver_checks", Detail: "accepted Handoff receipt requires all recorded receiver checks"}
	}
	if r.message != nil {
		return validateText("receipt.message", *r.message, MaxTextLength)
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
