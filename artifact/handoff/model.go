package handoff

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/thunguo/powercontext-go/artifact"
	"github.com/thunguo/powercontext-go/artifact/memory"
	"github.com/thunguo/powercontext-go/source"
)

const (
	Family                = "handoff"
	ContentSchemaVersion  = "powercontext.handoff.v1"
	PreparedSchemaVersion = "powercontext.prepared-handoff.v1"
	ResolutionTrust       = "untrusted_history"
	DefaultMaxBytes       = 8_000
	MaxBytes              = 32_768
	MinMaxBytes           = 512
	MaxCitations          = 32
	MaxOmissions          = 64
	MaxStateStatements    = 64
	MaxTextLength         = 8_192
)

func ValidateContentSchema(value string) error {
	if value != ContentSchemaVersion {
		return fmt.Errorf("unsupported Handoff schema %q", value)
	}
	return nil
}

func ValidatePreparedSchema(value string) error {
	if value != PreparedSchemaVersion {
		return fmt.Errorf("unsupported Prepared Handoff schema %q", value)
	}
	return nil
}

type Audience string

const (
	Human Audience = "human"
	Agent Audience = "agent"
)

type Disposition string

const (
	Continuable Disposition = "continuable"
	Blocked     Disposition = "blocked"
	Complete    Disposition = "complete"
)

type CitationKind string

const (
	SourceCitationKind   CitationKind = "source"
	ArtifactCitationKind CitationKind = "artifact"
	MemoryCitationKind   CitationKind = "memory"
)

type Citation interface {
	Kind() CitationKind
	citationKey() string
}

type SourceCitation struct{ ref source.Ref }

func NewSourceCitation(ref source.Ref) (SourceCitation, error) {
	value := SourceCitation{ref: ref}
	if err := value.Validate(); err != nil {
		return SourceCitation{}, err
	}
	return value, nil
}
func (c SourceCitation) Kind() CitationKind { return SourceCitationKind }
func (c SourceCitation) Ref() source.Ref    { return c.ref }
func (c SourceCitation) Validate() error {
	_, err := source.NewRef(c.ref.Type(), c.ref.ID())
	return err
}
func (c SourceCitation) citationKey() string {
	return "source\x00" + c.ref.Type() + "\x00" + c.ref.ID()
}

type ArtifactCitation struct{ ref artifact.Ref }

func NewArtifactCitation(ref artifact.Ref) (ArtifactCitation, error) {
	value := ArtifactCitation{ref: ref}
	if err := value.Validate(); err != nil {
		return ArtifactCitation{}, err
	}
	return value, nil
}
func (c ArtifactCitation) Kind() CitationKind  { return ArtifactCitationKind }
func (c ArtifactCitation) Ref() artifact.Ref   { return c.ref }
func (c ArtifactCitation) Validate() error     { return c.ref.Validate() }
func (c ArtifactCitation) citationKey() string { return "artifact\x00" + c.ref.String() }

type MemoryCitation struct{ citation memory.Citation }

func NewMemoryCitation(value memory.Citation) (MemoryCitation, error) {
	citation := MemoryCitation{citation: value}
	if err := citation.Validate(); err != nil {
		return MemoryCitation{}, err
	}
	return citation, nil
}
func (c MemoryCitation) Kind() CitationKind        { return MemoryCitationKind }
func (c MemoryCitation) Citation() memory.Citation { return c.citation }
func (c MemoryCitation) Validate() error {
	if err := c.citation.MemoryRef.Validate(); err != nil {
		return err
	}
	if _, err := memory.ValidateIdentifier(c.citation.EntryID); err != nil {
		return err
	}
	_, err := memory.ValidateIdentifier(c.citation.EntryVersionID)
	return err
}
func (c MemoryCitation) citationKey() string {
	return "memory\x00" + c.citation.MemoryRef.String() + "\x00" + c.citation.EntryID + "\x00" + c.citation.EntryVersionID
}

type Prepare struct {
	objective string
	evidence  []Citation
	maxBytes  int
}

func NewPrepare(objective string, evidence []Citation, maxBytes int) (Prepare, error) {
	value := Prepare{objective: objective, evidence: slices.Clone(evidence), maxBytes: maxBytes}
	if err := value.Validate(); err != nil {
		return Prepare{}, err
	}
	return value, nil
}

func (p Prepare) Objective() string    { return p.objective }
func (p Prepare) Evidence() []Citation { return slices.Clone(p.evidence) }
func (p Prepare) MaxBytes() int        { return p.maxBytes }
func (p Prepare) Validate() error {
	if err := validateText("objective", p.objective); err != nil {
		return err
	}
	if len(p.evidence) < 1 || len(p.evidence) > MaxCitations {
		return fmt.Errorf("Handoff preparation evidence must contain 1..%d citations", MaxCitations)
	}
	if err := validateCitations(p.evidence, true); err != nil {
		return err
	}
	return validateBudget(p.maxBytes)
}

type Activate struct {
	boundarySource source.Ref
	objective      string
	evidence       []Citation
	maxBytes       int
}

func NewActivate(boundary source.Ref, objective string, evidence []Citation, maxBytes int) (Activate, error) {
	value := Activate{boundarySource: boundary, objective: objective, evidence: slices.Clone(evidence), maxBytes: maxBytes}
	if err := value.Validate(); err != nil {
		return Activate{}, err
	}
	return value, nil
}

func (a Activate) BoundarySource() source.Ref { return a.boundarySource }
func (a Activate) Objective() string          { return a.objective }
func (a Activate) Evidence() []Citation       { return slices.Clone(a.evidence) }
func (a Activate) MaxBytes() int              { return a.maxBytes }
func (a Activate) Clone() Activate {
	a.evidence = slices.Clone(a.evidence)
	return a
}
func (a Activate) Validate() error {
	if _, err := source.NewRef(a.boundarySource.Type(), a.boundarySource.ID()); err != nil {
		return err
	}
	if err := validateText("objective", a.objective); err != nil {
		return err
	}
	if len(a.evidence) > MaxCitations {
		return fmt.Errorf("Handoff activation evidence exceeds the citation limit")
	}
	if err := validateBudget(a.maxBytes); err != nil {
		return err
	}
	if err := validateCitations(a.evidence, false); err != nil {
		return err
	}
	actionEvidence := a.ActionEvidence()
	if len(actionEvidence) > MaxCitations {
		return fmt.Errorf("Handoff activation evidence exceeds the citation limit")
	}
	if err := validateCitations(actionEvidence, true); err != nil {
		return fmt.Errorf("Handoff activation evidence must be unique: %w", err)
	}
	return nil
}
func (a Activate) ActionEvidence() []Citation {
	boundary := SourceCitation{ref: a.boundarySource}
	result := []Citation{boundary}
	for _, citation := range a.evidence {
		if validateCitation(citation) != nil || citation.citationKey() != boundary.citationKey() {
			result = append(result, citation)
		}
	}
	return result
}

type Evidence interface {
	Citation() Citation
	evidenceValue()
}

type SourceEvidence struct {
	citation SourceCitation
	source   source.Value
}

func NewSourceEvidence(citation SourceCitation, value source.Value) (SourceEvidence, error) {
	evidence := SourceEvidence{citation: citation, source: value}
	if err := evidence.Validate(); err != nil {
		return SourceEvidence{}, err
	}
	return evidence, nil
}
func (e SourceEvidence) Citation() Citation   { return e.citation }
func (SourceEvidence) evidenceValue()         {}
func (e SourceEvidence) Source() source.Value { return e.source }
func (e SourceEvidence) Validate() error {
	if err := e.citation.Validate(); err != nil {
		return err
	}
	if isNilInterface(e.source) || e.source.SourceName() != e.citation.ref.ID() {
		return fmt.Errorf("resolved Source does not match its Handoff citation")
	}
	return nil
}

type ArtifactEvidence struct {
	citation ArtifactCitation
	value    artifact.Snapshot
}

func NewArtifactEvidence(citation ArtifactCitation, value artifact.Snapshot) (ArtifactEvidence, error) {
	evidence := ArtifactEvidence{citation: citation, value: value}
	if err := evidence.Validate(); err != nil {
		return ArtifactEvidence{}, err
	}
	return evidence, nil
}
func (e ArtifactEvidence) Citation() Citation          { return e.citation }
func (ArtifactEvidence) evidenceValue()                {}
func (e ArtifactEvidence) Artifact() artifact.Snapshot { return e.value }
func (e ArtifactEvidence) Validate() error {
	if err := e.citation.Validate(); err != nil {
		return err
	}
	if isNilInterface(e.value) || e.value.Ref() != e.citation.ref {
		return fmt.Errorf("resolved Artifact does not match its Handoff citation")
	}
	return nil
}

type MemoryEvidence struct {
	citation MemoryCitation
	entry    memory.EntryVersion
}

func NewMemoryEvidence(citation MemoryCitation, entry memory.EntryVersion) (MemoryEvidence, error) {
	evidence := MemoryEvidence{citation: citation, entry: entry.Clone()}
	if err := evidence.Validate(); err != nil {
		return MemoryEvidence{}, err
	}
	return evidence, nil
}
func (e MemoryEvidence) Citation() Citation         { return e.citation }
func (MemoryEvidence) evidenceValue()               {}
func (e MemoryEvidence) Entry() memory.EntryVersion { return e.entry.Clone() }
func (e MemoryEvidence) Validate() error {
	if err := e.citation.Validate(); err != nil {
		return err
	}
	reference := e.citation.citation
	if reference.MemoryRef.ID() != e.entry.MemoryArtifactID || reference.EntryID != e.entry.EntryID ||
		reference.EntryVersionID != e.entry.EntryVersionID {
		return fmt.Errorf("resolved Memory entry does not match its Handoff citation")
	}
	return nil
}

type GenerationRequest struct {
	objective string
	evidence  []Evidence
	maxBytes  int
}

func NewGenerationRequest(objective string, evidence []Evidence, maxBytes int) (GenerationRequest, error) {
	value := GenerationRequest{objective: objective, evidence: slices.Clone(evidence), maxBytes: maxBytes}
	if err := value.Validate(); err != nil {
		return GenerationRequest{}, err
	}
	return value, nil
}
func (r GenerationRequest) Objective() string    { return r.objective }
func (r GenerationRequest) Evidence() []Evidence { return slices.Clone(r.evidence) }
func (r GenerationRequest) MaxBytes() int        { return r.maxBytes }
func (r GenerationRequest) Validate() error {
	if err := validateText("objective", r.objective); err != nil {
		return err
	}
	if len(r.evidence) < 1 || len(r.evidence) > MaxCitations {
		return fmt.Errorf("Handoff generation evidence must contain 1..%d values", MaxCitations)
	}
	for _, evidence := range r.evidence {
		if err := validateEvidence(evidence); err != nil {
			return err
		}
	}
	return validateBudget(r.maxBytes)
}

type Statement struct {
	text      string
	citations []Citation
}

func NewStatement(text string, citations []Citation) (Statement, error) {
	value := Statement{text: text, citations: slices.Clone(citations)}
	if err := value.Validate(); err != nil {
		return Statement{}, err
	}
	return value, nil
}
func (s Statement) Text() string          { return s.text }
func (s Statement) Citations() []Citation { return slices.Clone(s.citations) }
func (s Statement) Validate() error {
	if err := validateText("statement", s.text); err != nil {
		return err
	}
	if len(s.citations) < 1 || len(s.citations) > MaxCitations {
		return fmt.Errorf("Handoff statement must contain 1..%d citations", MaxCitations)
	}
	return validateCitations(s.citations, false)
}

type Omission struct {
	text     string
	citation Citation
}

func NewOmission(text string, citation Citation) (Omission, error) {
	value := Omission{text: text, citation: citation}
	if err := value.Validate(); err != nil {
		return Omission{}, err
	}
	return value, nil
}
func (o Omission) Text() string       { return o.text }
func (o Omission) Citation() Citation { return o.citation }
func (o Omission) Validate() error {
	if err := validateText("omission", o.text); err != nil {
		return err
	}
	if o.citation != nil {
		return validateCitation(o.citation)
	}
	return nil
}

type Content struct {
	objective   string
	state       []Statement
	disposition Disposition
	nextAction  *Statement
	omissions   []Omission
}

func NewContent(objective string, state []Statement, disposition Disposition, nextAction *Statement, omissions []Omission) (Content, error) {
	value := Content{
		objective: objective, state: slices.Clone(state), disposition: disposition,
		nextAction: cloneStatement(nextAction), omissions: slices.Clone(omissions),
	}
	if err := value.Validate(); err != nil {
		return Content{}, err
	}
	return value, nil
}

func (c Content) Schema() string           { return ContentSchemaVersion }
func (c Content) Objective() string        { return c.objective }
func (c Content) State() []Statement       { return slices.Clone(c.state) }
func (c Content) Disposition() Disposition { return c.disposition }
func (c Content) NextAction() *Statement   { return cloneStatement(c.nextAction) }
func (c Content) Omissions() []Omission    { return slices.Clone(c.omissions) }
func (c Content) Equal(other Content) bool { return reflect.DeepEqual(c, other) }
func (c Content) Validate() error {
	if err := validateText("objective", c.objective); err != nil {
		return err
	}
	if len(c.state) < 1 || len(c.state) > MaxStateStatements {
		return fmt.Errorf("Handoff state must contain 1..%d statements", MaxStateStatements)
	}
	for _, statement := range c.state {
		if err := statement.Validate(); err != nil {
			return err
		}
	}
	if c.disposition != Continuable && c.disposition != Blocked && c.disposition != Complete {
		return fmt.Errorf("invalid Handoff disposition %q", c.disposition)
	}
	if c.nextAction != nil {
		if err := c.nextAction.Validate(); err != nil {
			return err
		}
	}
	if len(c.omissions) > MaxOmissions {
		return fmt.Errorf("Handoff omissions exceed %d items", MaxOmissions)
	}
	for _, omission := range c.omissions {
		if err := omission.Validate(); err != nil {
			return err
		}
	}
	return nil
}

type Draft struct{ content Content }

func NewDraft(objective string, state []Statement, disposition Disposition, nextAction *Statement, omissions []Omission) (Draft, error) {
	content, err := NewContent(objective, state, disposition, nextAction, omissions)
	if err != nil {
		return Draft{}, err
	}
	return Draft{content: content}, nil
}
func (d Draft) AsContent() Content { return cloneContent(d.content) }
func (d Draft) Objective() string  { return d.content.objective }
func (d Draft) Validate() error    { return d.content.Validate() }

type ActivationStatus string

const (
	ActivationGenerated ActivationStatus = "generated"
	ActivationIgnored   ActivationStatus = "ignored"
)

type Activation struct {
	status           ActivationStatus
	boundarySource   source.Ref
	previousPosition int64
	currentPosition  int64
	draft            *Draft
}

func NewActivation(
	status ActivationStatus,
	boundarySource source.Ref,
	previousPosition int64,
	currentPosition int64,
	draft *Draft,
) (Activation, error) {
	value := Activation{
		status: status, boundarySource: boundarySource,
		previousPosition: previousPosition, currentPosition: currentPosition,
		draft: cloneDraft(draft),
	}
	if err := value.Validate(); err != nil {
		return Activation{}, err
	}
	return value, nil
}

func (a Activation) Status() ActivationStatus   { return a.status }
func (a Activation) BoundarySource() source.Ref { return a.boundarySource }
func (a Activation) PreviousPosition() int64    { return a.previousPosition }
func (a Activation) CurrentPosition() int64     { return a.currentPosition }
func (a Activation) Draft() *Draft              { return cloneDraft(a.draft) }
func (a Activation) Validate() error {
	if _, err := source.NewRef(a.boundarySource.Type(), a.boundarySource.ID()); err != nil {
		return err
	}
	if a.previousPosition < 0 || a.currentPosition < 0 {
		return fmt.Errorf("Handoff activation positions must not be negative")
	}
	if a.currentPosition < a.previousPosition {
		return fmt.Errorf("Handoff activation position cannot move backwards")
	}
	switch a.status {
	case ActivationGenerated:
		if a.draft == nil || a.currentPosition <= a.previousPosition {
			return fmt.Errorf("generated Handoff activation must advance with a Draft")
		}
		return a.draft.Validate()
	case ActivationIgnored:
		if a.draft != nil || a.currentPosition != a.previousPosition {
			return fmt.Errorf("ignored Handoff activation cannot change state or contain a Draft")
		}
		return nil
	default:
		return fmt.Errorf("invalid Handoff activation status %q", a.status)
	}
}

type Handoff = artifact.Artifact[Content]
type ArtifactDraft = artifact.Draft[Content]

func NewArtifactDraft(content Content, sources []source.Ref, artifacts []artifact.Ref) (ArtifactDraft, error) {
	if err := content.Validate(); err != nil {
		return ArtifactDraft{}, err
	}
	return artifact.NewDraft(Family, cloneContent(content), sources, artifacts)
}

type Prepared struct {
	scopeID string
	base    *artifact.Ref
	content Content
}

func NewPrepared(scopeID string, base *artifact.Ref, content Content) (Prepared, error) {
	value := Prepared{scopeID: scopeID, base: cloneArtifactRef(base), content: cloneContent(content)}
	if err := value.Validate(); err != nil {
		return Prepared{}, err
	}
	return value, nil
}
func (p Prepared) Schema() string      { return PreparedSchemaVersion }
func (p Prepared) ScopeID() string     { return p.scopeID }
func (p Prepared) Base() *artifact.Ref { return cloneArtifactRef(p.base) }
func (p Prepared) Content() Content    { return cloneContent(p.content) }
func (p Prepared) Validate() error {
	if strings.TrimSpace(p.scopeID) == "" {
		return fmt.Errorf("scope_id must contain non-whitespace content")
	}
	if p.base != nil {
		if err := p.base.Validate(); err != nil {
			return err
		}
	}
	return p.content.Validate()
}

type EvidenceStatus string

const (
	EvidenceAvailable   EvidenceStatus = "available"
	EvidenceUnavailable EvidenceStatus = "unavailable"
)

type Claim string

const (
	StateClaim      Claim = "state"
	NextActionClaim Claim = "next_action"
)

type EvidenceCheck struct {
	claim               Claim
	stateIndex          *int
	status              EvidenceStatus
	unavailableEvidence []Citation
}

func NewEvidenceCheck(
	claim Claim,
	stateIndex *int,
	status EvidenceStatus,
	unavailableEvidence []Citation,
) (EvidenceCheck, error) {
	value := EvidenceCheck{
		claim: claim, stateIndex: cloneInt(stateIndex), status: status,
		unavailableEvidence: slices.Clone(unavailableEvidence),
	}
	if err := value.Validate(); err != nil {
		return EvidenceCheck{}, err
	}
	return value, nil
}

func (c EvidenceCheck) Claim() Claim                    { return c.claim }
func (c EvidenceCheck) StateIndex() *int                { return cloneInt(c.stateIndex) }
func (c EvidenceCheck) Status() EvidenceStatus          { return c.status }
func (c EvidenceCheck) UnavailableEvidence() []Citation { return slices.Clone(c.unavailableEvidence) }
func (c EvidenceCheck) Validate() error {
	if c.claim != StateClaim && c.claim != NextActionClaim {
		return fmt.Errorf("invalid Handoff evidence claim %q", c.claim)
	}
	if c.claim == StateClaim && c.stateIndex == nil {
		return fmt.Errorf("state evidence check requires a state index")
	}
	if c.claim == NextActionClaim && c.stateIndex != nil {
		return fmt.Errorf("next-action evidence check cannot contain a state index")
	}
	if c.stateIndex != nil && *c.stateIndex < 0 {
		return fmt.Errorf("Handoff evidence state index must not be negative")
	}
	if c.status != EvidenceAvailable && c.status != EvidenceUnavailable {
		return fmt.Errorf("invalid Handoff evidence status %q", c.status)
	}
	if len(c.unavailableEvidence) > MaxCitations {
		return fmt.Errorf("Handoff unavailable evidence exceeds %d citations", MaxCitations)
	}
	if err := validateCitations(c.unavailableEvidence, false); err != nil {
		return err
	}
	if c.status == EvidenceAvailable && len(c.unavailableEvidence) != 0 {
		return fmt.Errorf("available evidence check cannot identify unavailable evidence")
	}
	if c.status == EvidenceUnavailable && len(c.unavailableEvidence) == 0 {
		return fmt.Errorf("unavailable evidence check must identify unavailable evidence")
	}
	return nil
}

type Selection string

const (
	PreparedSelection Selection = "prepared"
	ExactSelection    Selection = "exact"
	LatestSelection   Selection = "latest"
)

type ResolutionStatus string

const (
	EmptyResolution    ResolutionStatus = "empty"
	ResolvedResolution ResolutionStatus = "resolved"
)

type Resolution struct {
	status           ResolutionStatus
	scopeID          string
	content          *Content
	selection        *Selection
	selectedRevision *artifact.Ref
	currentRevision  *artifact.Ref
	evidenceChecks   []EvidenceCheck
}

func Empty(scopeID string) Resolution {
	value, err := NewResolution(EmptyResolution, scopeID, nil, nil, nil, nil, nil)
	if err != nil {
		panic(err)
	}
	return value
}

func NewResolved(
	scopeID string,
	content Content,
	selection Selection,
	selectedRevision *artifact.Ref,
	currentRevision *artifact.Ref,
	evidenceChecks []EvidenceCheck,
) (Resolution, error) {
	return NewResolution(
		ResolvedResolution,
		scopeID,
		&content,
		&selection,
		selectedRevision,
		currentRevision,
		evidenceChecks,
	)
}

func NewResolution(
	status ResolutionStatus,
	scopeID string,
	content *Content,
	selection *Selection,
	selectedRevision *artifact.Ref,
	currentRevision *artifact.Ref,
	evidenceChecks []EvidenceCheck,
) (Resolution, error) {
	value := Resolution{
		status: status, scopeID: scopeID, content: cloneContentPointer(content),
		selection: cloneSelection(selection), selectedRevision: cloneArtifactRef(selectedRevision),
		currentRevision: cloneArtifactRef(currentRevision), evidenceChecks: cloneEvidenceChecks(evidenceChecks),
	}
	if err := value.Validate(); err != nil {
		return Resolution{}, err
	}
	return value, nil
}

func (r Resolution) Trust() string                   { return ResolutionTrust }
func (r Resolution) Status() ResolutionStatus        { return r.status }
func (r Resolution) ScopeID() string                 { return r.scopeID }
func (r Resolution) Content() *Content               { return cloneContentPointer(r.content) }
func (r Resolution) Selection() *Selection           { return cloneSelection(r.selection) }
func (r Resolution) SelectedRevision() *artifact.Ref { return cloneArtifactRef(r.selectedRevision) }
func (r Resolution) CurrentRevision() *artifact.Ref  { return cloneArtifactRef(r.currentRevision) }
func (r Resolution) EvidenceChecks() []EvidenceCheck { return cloneEvidenceChecks(r.evidenceChecks) }

func (r Resolution) Validate() error {
	if r.status != EmptyResolution && r.status != ResolvedResolution {
		return fmt.Errorf("invalid Handoff resolution status %q", r.status)
	}
	if r.status == EmptyResolution {
		if r.content != nil || r.selection != nil || r.selectedRevision != nil ||
			r.currentRevision != nil || len(r.evidenceChecks) != 0 {
			return fmt.Errorf("empty resolution cannot contain Handoff state")
		}
		return nil
	}
	if r.content == nil || r.selection == nil {
		return fmt.Errorf("resolved Handoff must contain content and selection")
	}
	if err := r.content.Validate(); err != nil {
		return err
	}
	if err := validateResolutionSelection(*r.selection, r.selectedRevision, r.currentRevision); err != nil {
		return err
	}
	return validateResolutionEvidence(*r.content, r.evidenceChecks)
}

func validateText(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s must contain non-whitespace content", field)
	}
	if utf8.RuneCountInString(value) > MaxTextLength {
		return fmt.Errorf("%s must not exceed %d characters", field, MaxTextLength)
	}
	return nil
}

func validateBudget(value int) error {
	if value < MinMaxBytes || value > MaxBytes {
		return fmt.Errorf("Handoff max_bytes must be between %d and %d", MinMaxBytes, MaxBytes)
	}
	return nil
}

func validateCitations(values []Citation, unique bool) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if err := validateCitation(value); err != nil {
			return err
		}
		if unique {
			key := value.citationKey()
			if _, exists := seen[key]; exists {
				return fmt.Errorf("Handoff citations must be unique")
			}
			seen[key] = struct{}{}
		}
	}
	return nil
}

func validateCitation(value Citation) error {
	if value == nil {
		return fmt.Errorf("Handoff citation must not be nil")
	}
	switch citation := value.(type) {
	case SourceCitation:
		return citation.Validate()
	case ArtifactCitation:
		return citation.Validate()
	case MemoryCitation:
		return citation.Validate()
	default:
		return fmt.Errorf("unsupported Handoff citation %T", value)
	}
}

func validateEvidence(value Evidence) error {
	if value == nil {
		return fmt.Errorf("Handoff evidence must not be nil")
	}
	switch evidence := value.(type) {
	case SourceEvidence:
		return evidence.Validate()
	case ArtifactEvidence:
		return evidence.Validate()
	case MemoryEvidence:
		return evidence.Validate()
	default:
		return fmt.Errorf("unsupported Handoff evidence %T", value)
	}
}

func validateResolutionSelection(selection Selection, selected, current *artifact.Ref) error {
	if selection != PreparedSelection && selection != ExactSelection && selection != LatestSelection {
		return fmt.Errorf("invalid Handoff resolution selection %q", selection)
	}
	for _, ref := range []*artifact.Ref{selected, current} {
		if ref != nil {
			if err := ref.Validate(); err != nil {
				return err
			}
		}
	}
	if selection == PreparedSelection && selected != nil {
		return fmt.Errorf("prepared selection cannot identify a committed Revision")
	}
	if selection != PreparedSelection && selected == nil {
		return fmt.Errorf("committed selection must identify its exact Revision")
	}
	if selection != PreparedSelection && current == nil {
		return fmt.Errorf("committed selection must identify the current Revision")
	}
	if selection == LatestSelection && *selected != *current {
		return fmt.Errorf("latest selection must select the current Revision")
	}
	if selected != nil && current != nil &&
		(selected.Family() != current.Family() || selected.ID() != current.ID()) {
		return fmt.Errorf("selected and current Revisions must share one Artifact identity")
	}
	return nil
}

func validateResolutionEvidence(content Content, checks []EvidenceCheck) error {
	expected := len(content.state)
	if content.nextAction != nil {
		expected++
	}
	if len(checks) != expected {
		return fmt.Errorf("evidence checks must match Handoff statements in order")
	}
	for index, check := range checks {
		if err := check.Validate(); err != nil {
			return err
		}
		var statement Statement
		if index < len(content.state) {
			if check.claim != StateClaim || check.stateIndex == nil || *check.stateIndex != index {
				return fmt.Errorf("evidence checks must match Handoff statements in order")
			}
			statement = content.state[index]
		} else {
			if check.claim != NextActionClaim || check.stateIndex != nil {
				return fmt.Errorf("evidence checks must match Handoff statements in order")
			}
			statement = *content.nextAction
		}
		for _, unavailable := range check.unavailableEvidence {
			if !statementHasCitation(statement, unavailable) {
				return fmt.Errorf("unavailable evidence must belong to the checked statement")
			}
		}
	}
	return nil
}

func statementHasCitation(statement Statement, citation Citation) bool {
	key := citation.citationKey()
	for _, candidate := range statement.citations {
		if candidate.citationKey() == key {
			return true
		}
	}
	return false
}

func isNilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func cloneStatement(value *Statement) *Statement {
	if value == nil {
		return nil
	}
	cloned := Statement{text: value.text, citations: slices.Clone(value.citations)}
	return &cloned
}

func cloneContent(value Content) Content {
	state := make([]Statement, len(value.state))
	for index := range value.state {
		state[index] = *cloneStatement(&value.state[index])
	}
	return Content{
		objective: value.objective, state: state, disposition: value.disposition,
		nextAction: cloneStatement(value.nextAction), omissions: slices.Clone(value.omissions),
	}
}

func cloneDraft(value *Draft) *Draft {
	if value == nil {
		return nil
	}
	cloned := Draft{content: cloneContent(value.content)}
	return &cloned
}

func cloneContentPointer(value *Content) *Content {
	if value == nil {
		return nil
	}
	cloned := cloneContent(*value)
	return &cloned
}

func cloneSelection(value *Selection) *Selection {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneEvidenceChecks(values []EvidenceCheck) []EvidenceCheck {
	result := make([]EvidenceCheck, len(values))
	for index, value := range values {
		result[index] = EvidenceCheck{
			claim: value.claim, stateIndex: cloneInt(value.stateIndex), status: value.status,
			unavailableEvidence: slices.Clone(value.unavailableEvidence),
		}
	}
	return result
}

func cloneArtifactRef(value *artifact.Ref) *artifact.Ref {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
