package handoffreport

import (
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ob-labs/powercontext-go/artifact"
	"github.com/ob-labs/powercontext-go/artifact/handoff"
	"github.com/ob-labs/powercontext-go/internal/work"
)

type Format string

const (
	ReportSchemaVersion        = "powercontext.handoff-report.v1"
	ReportTrust                = "untrusted_history"
	ReportSelectionConsistency = "optimistic_stable"
)

const (
	FormatJSON     Format = "json"
	FormatMarkdown Format = "markdown"
)

type ReportKind string

const (
	ReportHandoff  ReportKind = "handoff"
	ReportPeriodic ReportKind = "periodic"
)

type ActivityCoverageStatus string

const (
	ActivityNotConfigured ActivityCoverageStatus = "not_configured"
	ActivityCaptured      ActivityCoverageStatus = "captured"
	ActivityUnavailable   ActivityCoverageStatus = "unavailable"
)

type WorkStatus string
type ActivityStatus string
type ReportingStatus string
type HandoffActivityRelation string

const (
	WorkContinuable WorkStatus = "continuable"
	WorkBlocked     WorkStatus = "blocked"
	WorkComplete    WorkStatus = "complete"
	WorkNoHandoff   WorkStatus = "no_handoff"

	ActivityNone           ActivityStatus = "no_observed_activity"
	ActivityAfterHandoff   ActivityStatus = "activity_after_handoff"
	ActivityWithoutHandoff ActivityStatus = "activity_without_handoff"
	ActivityCurrentOnly    ActivityStatus = "current_only"
	ActivityUnknown        ActivityStatus = "unknown"

	ReportingReported        ReportingStatus         = "reported"
	ReportingWithOmissions   ReportingStatus         = "reported_with_omissions"
	ReportingEvidenceMissing ReportingStatus         = "evidence_unavailable"
	ReportingNoHandoff       ReportingStatus         = "no_handoff"
	RelationAfterHandoff     HandoffActivityRelation = "activity_after_handoff"
	RelationNoActivityAfter  HandoffActivityRelation = "no_observed_activity_after_handoff"
	RelationUnknown          HandoffActivityRelation = "unknown"
)

type Coverage struct {
	TotalIncludedWorkstreams          int
	CatalogMatchedWorkstreams         int
	SelectedWorkstreams               int
	MissingHandoffWorkstreams         int
	ReportedWithOmissions             int
	UncheckedEvidenceWorkstreams      int
	UnavailableEvidenceWorkstreams    int
	ActivityWithoutHandoffWorkstreams int
	ActivityAfterHandoffWorkstreams   int
	UnknownTimeEvents                 int
	UnassignedActivityCount           int
	UnassignedActivityEvents          int
	ActivityCoverage                  ActivityCoverageStatus
}

type Summary struct {
	ContinuableCount int
	BlockedCount     int
	CompleteCount    int
	NoHandoffCount   int
}

type PeriodComparison struct {
	PreviousStart         time.Time
	PreviousEnd           time.Time
	CurrentActivityCount  int
	PreviousActivityCount int
	ActivityDelta         int
}

type HandoffRevisionSummary struct {
	reference         artifact.Ref
	objectiveExcerpt  string
	disposition       handoff.Disposition
	nextActionExcerpt *string
	stateCount        int
	omissionCount     int
}

func (v HandoffRevisionSummary) Reference() artifact.Ref          { return v.reference }
func (v HandoffRevisionSummary) ObjectiveExcerpt() string         { return v.objectiveExcerpt }
func (v HandoffRevisionSummary) Disposition() handoff.Disposition { return v.disposition }
func (v HandoffRevisionSummary) NextActionExcerpt() *string {
	return cloneString(v.nextActionExcerpt)
}
func (v HandoffRevisionSummary) StateCount() int    { return v.stateCount }
func (v HandoffRevisionSummary) OmissionCount() int { return v.omissionCount }
func (v HandoffRevisionSummary) Validate() error {
	if err := v.reference.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(v.objectiveExcerpt) == "" || utf8.RuneCountInString(v.objectiveExcerpt) > MaxReportHistoryExcerptLength {
		return fieldError("objective_excerpt", "must be non-empty and within the Handoff history excerpt limit")
	}
	if v.disposition != handoff.Continuable && v.disposition != handoff.Blocked && v.disposition != handoff.Complete {
		return fieldError("disposition", "has an unsupported value")
	}
	if v.nextActionExcerpt != nil && utf8.RuneCountInString(*v.nextActionExcerpt) > MaxReportHistoryExcerptLength {
		return fieldError("next_action_excerpt", "exceeds the Handoff history excerpt limit")
	}
	if v.stateCount < 1 || v.omissionCount < 0 {
		return fieldError("handoff_history", "contains invalid item counts")
	}
	return nil
}

func NewPeriodComparison(previousStart, previousEnd time.Time, current, previous int) (PeriodComparison, error) {
	value := PeriodComparison{
		PreviousStart: previousStart.UTC(), PreviousEnd: previousEnd.UTC(),
		CurrentActivityCount: current, PreviousActivityCount: previous,
		ActivityDelta: current - previous,
	}
	if err := value.Validate(); err != nil {
		return PeriodComparison{}, err
	}
	return value, nil
}

func (v PeriodComparison) Validate() error {
	if !v.PreviousStart.Before(v.PreviousEnd) {
		return fieldError("period", "previous period start must precede its end")
	}
	if v.CurrentActivityCount < 0 || v.PreviousActivityCount < 0 {
		return fieldError("period", "activity counts must be non-negative")
	}
	if v.ActivityDelta != v.CurrentActivityCount-v.PreviousActivityCount {
		return fieldError("activity_delta", "must match current minus previous Activity count")
	}
	return nil
}

type WorkstreamReport struct {
	workstream              WorkstreamDescriptor
	continuity              work.Continuity
	handoffRef              *artifact.Ref
	content                 *handoff.Content
	handoffRevisionCount    int
	handoffHistoryTruncated bool
	handoffHistory          []HandoffRevisionSummary
	evidenceChecks          []handoff.EvidenceCheck
	evidenceChecked         bool
	evidenceUnavailable     bool
	activities              []ActivityEvent
	workStatus              WorkStatus
	reportingStatus         ReportingStatus
	activityStatus          ActivityStatus
	handoffActivityRelation *HandoffActivityRelation
	observedActivityCount   int
}

func (v WorkstreamReport) Workstream() WorkstreamDescriptor { return v.workstream }
func (v WorkstreamReport) Continuity() work.Continuity      { return v.continuity }
func (v WorkstreamReport) HandoffRef() *artifact.Ref        { return cloneArtifactRef(v.handoffRef) }
func (v WorkstreamReport) Content() *handoff.Content {
	if v.content == nil {
		return nil
	}
	copy := *v.content
	return &copy
}
func (v WorkstreamReport) HandoffRevisionCount() int     { return v.handoffRevisionCount }
func (v WorkstreamReport) HandoffHistoryTruncated() bool { return v.handoffHistoryTruncated }
func (v WorkstreamReport) HandoffHistory() []HandoffRevisionSummary {
	return slices.Clone(v.handoffHistory)
}
func (v WorkstreamReport) EvidenceChecks() ([]handoff.EvidenceCheck, bool) {
	if !v.evidenceChecked {
		return nil, false
	}
	return cloneEvidenceChecks(v.evidenceChecks), true
}
func (v WorkstreamReport) EvidenceUnavailable() bool        { return v.evidenceUnavailable }
func (v WorkstreamReport) Activities() []ActivityEvent      { return slices.Clone(v.activities) }
func (v WorkstreamReport) WorkStatus() WorkStatus           { return v.workStatus }
func (v WorkstreamReport) ReportingStatus() ReportingStatus { return v.reportingStatus }
func (v WorkstreamReport) ActivityStatus() ActivityStatus   { return v.activityStatus }
func (v WorkstreamReport) HandoffActivityRelation() *HandoffActivityRelation {
	if v.handoffActivityRelation == nil {
		return nil
	}
	copy := *v.handoffActivityRelation
	return &copy
}
func (v WorkstreamReport) ObservedActivityCount() int { return v.observedActivityCount }

func (v WorkstreamReport) Validate() error {
	if err := v.workstream.Validate(); err != nil {
		return err
	}
	if err := v.continuity.Validate(); err != nil {
		return err
	}
	if v.continuity.ScopeID() != v.workstream.ScopeID() {
		return fieldError("continuity.scope_id", "must match its Workstream")
	}
	if v.handoffRevisionCount < 0 || len(v.handoffHistory) > MaxReportHandoffHistory {
		return fieldError("handoff_history", "contains invalid Revision counts")
	}
	for _, summary := range v.handoffHistory {
		if err := summary.Validate(); err != nil {
			return err
		}
	}
	if len(v.activities) > MaxReportActivities {
		return fieldError("activities", fmt.Sprintf("must not exceed %d items", MaxReportActivities))
	}
	for _, event := range v.activities {
		if err := event.Validate(); err != nil {
			return err
		}
	}
	if v.observedActivityCount < 0 || v.observedActivityCount != len(v.activities) {
		return fieldError("observed_activity_count", "must match the Workstream activity count")
	}
	if !validWorkStatus(v.workStatus) || !validReportingStatus(v.reportingStatus) ||
		!validActivityStatus(v.activityStatus) || !validActivityRelation(v.handoffActivityRelation) {
		return fieldError("workstream_report", "contains an unsupported status")
	}
	if v.handoffRef == nil {
		if v.content != nil {
			return fieldError("content", "a Workstream without Handoff cannot contain Handoff content")
		}
		if v.evidenceChecked || len(v.evidenceChecks) != 0 {
			return fieldError("evidence_checks", "a Workstream without Handoff cannot contain evidence checks")
		}
		if v.evidenceUnavailable {
			return fieldError("evidence_unavailable", "a Workstream without Handoff cannot have unavailable evidence checks")
		}
		if v.workStatus != WorkNoHandoff || v.reportingStatus != ReportingNoHandoff {
			return fieldError("work_status", "a Workstream without Handoff must report no_handoff state")
		}
		if v.handoffRevisionCount != 0 || len(v.handoffHistory) != 0 || v.handoffHistoryTruncated {
			return fieldError("handoff_history", "a Workstream without Handoff cannot contain Handoff Revision history")
		}
		return nil
	}
	if err := v.handoffRef.Validate(); err != nil || v.handoffRef.Family() != handoff.Family {
		return fieldError("handoff_ref", "must be an exact Handoff reference")
	}
	if v.content == nil {
		return fieldError("content", "an exact Handoff selection must contain Handoff content")
	}
	if err := v.content.Validate(); err != nil {
		return err
	}
	if !v.evidenceChecked && len(v.evidenceChecks) != 0 {
		return fieldError("evidence_checks", "not_checked evidence cannot contain checks")
	}
	if v.evidenceUnavailable && v.evidenceChecked {
		return fieldError("evidence_checks", "an unavailable evidence check must remain not_checked")
	}
	if v.evidenceChecked {
		for _, check := range v.evidenceChecks {
			if err := check.Validate(); err != nil {
				return err
			}
		}
	}
	if v.workStatus != WorkStatus(v.content.Disposition()) {
		return fieldError("work_status", "must match the exact Handoff disposition")
	}
	if v.reportingStatus == ReportingNoHandoff {
		return fieldError("reporting_status", "an exact Handoff selection cannot report missing Handoff state")
	}
	if len(v.handoffHistory) == 0 || v.handoffHistory[len(v.handoffHistory)-1].reference != *v.handoffRef {
		return fieldError("handoff_history", "Handoff reference history must end at the exact selected Handoff")
	}
	if v.handoffRevisionCount < len(v.handoffHistory) || v.handoffHistoryTruncated != (v.handoffRevisionCount > len(v.handoffHistory)) {
		return fieldError("handoff_history", "Revision count and truncation must match the projected history")
	}
	var previous int64
	for _, summary := range v.handoffHistory {
		ref := summary.reference
		if ref.Family() != v.handoffRef.Family() || ref.ID() != v.handoffRef.ID() {
			return fieldError("handoff_history", "must belong to the selected Artifact lifecycle")
		}
		if ref.Revision() <= previous {
			return fieldError("handoff_history", "must be unique and ascending")
		}
		previous = ref.Revision()
	}
	return nil
}

type Report struct {
	locale             Locale
	format             Format
	reportKind         ReportKind
	rendererVersion    string
	generatedAt        time.Time
	project            ProjectDescriptor
	normalizedFilters  map[string]any
	normalizedPeriod   map[string]any
	periodComparison   *PeriodComparison
	baselineSelection  []SelectionEntry
	baselinePresent    bool
	endSelection       []SelectionEntry
	activityCursor     int64
	activitySelection  []string
	selectionDigest    string
	reportDigest       string
	coverage           Coverage
	summary            Summary
	unassignedActivity []ActivityEvent
	workstreams        []WorkstreamReport
}

func (v Report) Locale() Locale                    { return v.locale }
func (v Report) Format() Format                    { return v.format }
func (v Report) ReportKind() ReportKind            { return v.reportKind }
func (v Report) RendererVersion() string           { return v.rendererVersion }
func (v Report) GeneratedAt() time.Time            { return v.generatedAt }
func (v Report) Project() ProjectDescriptor        { return v.project }
func (v Report) ProjectRevision() int              { return v.project.Version() }
func (v Report) NormalizedFilters() map[string]any { return cloneJSONMap(v.normalizedFilters) }
func (v Report) NormalizedPeriod() map[string]any  { return cloneJSONMap(v.normalizedPeriod) }
func (v Report) PeriodComparison() *PeriodComparison {
	if v.periodComparison == nil {
		return nil
	}
	copy := *v.periodComparison
	return &copy
}
func (v Report) BaselineSelection() ([]SelectionEntry, bool) {
	return slices.Clone(v.baselineSelection), v.baselinePresent
}
func (v Report) EndSelection() []SelectionEntry      { return slices.Clone(v.endSelection) }
func (v Report) ActivityCursor() int64               { return v.activityCursor }
func (v Report) ActivitySelection() []string         { return slices.Clone(v.activitySelection) }
func (v Report) SelectionDigest() string             { return v.selectionDigest }
func (v Report) ReportDigest() string                { return v.reportDigest }
func (v Report) Coverage() Coverage                  { return v.coverage }
func (v Report) Summary() Summary                    { return v.summary }
func (v Report) UnassignedActivity() []ActivityEvent { return slices.Clone(v.unassignedActivity) }
func (v Report) Workstreams() []WorkstreamReport     { return slices.Clone(v.workstreams) }

func (v Report) Schema() string               { return ReportSchemaVersion }
func (v Report) Trust() string                { return ReportTrust }
func (v Report) SelectionConsistency() string { return ReportSelectionConsistency }

func (v Report) Validate() error {
	if v.locale != LocaleChinese && v.locale != LocaleEnglish {
		return fieldError("locale", "has an unsupported value")
	}
	if v.format != FormatJSON && v.format != FormatMarkdown {
		return fieldError("format", "has an unsupported value")
	}
	if v.reportKind != ReportHandoff && v.reportKind != ReportPeriodic {
		return fieldError("report_kind", "has an unsupported value")
	}
	if v.generatedAt.IsZero() {
		return fieldError("generated_at", "must be set")
	}
	_, generatedOffset := v.generatedAt.Zone()
	if generatedOffset != 0 {
		return fieldError("generated_at", "must be UTC")
	}
	if err := v.project.Validate(); err != nil {
		return err
	}
	if v.normalizedFilters == nil {
		return fieldError("normalized_filters", "must be an object")
	}
	if _, err := normalizeCanonical(v.normalizedFilters); err != nil {
		return err
	}
	if _, err := normalizeCanonical(v.normalizedPeriod); err != nil {
		return err
	}
	if v.reportKind == ReportPeriodic && v.normalizedPeriod == nil {
		return fieldError("normalized_period", "a periodic report must contain a normalized period")
	}
	if v.reportKind == ReportHandoff && (v.normalizedPeriod != nil || v.periodComparison != nil) {
		return fieldError("normalized_period", "a point-in-time Handoff report cannot contain period values")
	}
	if v.periodComparison != nil {
		if v.reportKind != ReportPeriodic {
			return fieldError("period_comparison", "is only valid for a periodic report")
		}
		if err := v.periodComparison.Validate(); err != nil {
			return err
		}
	}
	if len(v.baselineSelection) > MaxReportWorkstreams || len(v.endSelection) > MaxReportWorkstreams ||
		len(v.workstreams) > MaxReportWorkstreams {
		return fieldError("workstreams", fmt.Sprintf("must not exceed %d items", MaxReportWorkstreams))
	}
	for _, entry := range v.baselineSelection {
		if err := entry.Validate(); err != nil {
			return err
		}
	}
	if len(v.endSelection) != len(v.workstreams) {
		return fieldError("end_selection", "must exactly match Workstream reports")
	}
	selectionScopes := make(map[string]struct{}, len(v.endSelection))
	reportScopes := make(map[string]struct{}, len(v.workstreams))
	for index, item := range v.workstreams {
		if err := item.Validate(); err != nil {
			return err
		}
		entry := v.endSelection[index]
		if err := entry.Validate(); err != nil {
			return err
		}
		if _, exists := selectionScopes[entry.ScopeID()]; exists {
			return fieldError("end_selection", "Handoff Report selection scopes must be unique")
		}
		selectionScopes[entry.ScopeID()] = struct{}{}
		if _, exists := reportScopes[item.workstream.ScopeID()]; exists {
			return fieldError("workstreams", "Handoff Report Workstream scopes must be unique")
		}
		reportScopes[item.workstream.ScopeID()] = struct{}{}
		if entry.ScopeID() != item.workstream.ScopeID() {
			return fieldError("end_selection", "Workstream reports must exactly match selection scope order")
		}
		if item.workstream.ProjectID() != v.project.ProjectID() {
			return fieldError("workstreams", "every Workstream report must belong to the Report Project")
		}
		if entry.WorkstreamRevision() != item.workstream.Version() {
			return fieldError("end_selection", "selection Workstream revision must match the projected descriptor")
		}
		left, right := entry.HandoffRef(), item.HandoffRef()
		if (left == nil) != (right == nil) || (left != nil && *left != *right) {
			return fieldError("end_selection", "selection Handoff reference must match the Workstream report")
		}
	}
	if err := validateReportActivity(v); err != nil {
		return err
	}
	if err := validateReportCoverage(v); err != nil {
		return err
	}
	if err := validateReportSummary(v); err != nil {
		return err
	}
	if !validDigest(v.selectionDigest) || !validDigest(v.reportDigest) {
		return fieldError("digest", "must use sha256:<64 lowercase hexadecimal characters>")
	}
	return nil
}

func validateReportActivity(report Report) error {
	if report.activityCursor < 0 {
		return fieldError("activity_cursor", "must be non-negative")
	}
	if len(report.activitySelection) > MaxReportActivities || len(report.unassignedActivity) > MaxReportActivities {
		return fieldError("activities", fmt.Sprintf("must not exceed %d items", MaxReportActivities))
	}
	knownScopes := make(map[string]struct{}, len(report.workstreams))
	activityIDs := make([]string, 0, len(report.activitySelection))
	seenIDs := make(map[string]struct{}, len(report.activitySelection))
	for _, item := range report.workstreams {
		scopeID := item.workstream.ScopeID()
		knownScopes[scopeID] = struct{}{}
		for _, event := range item.activities {
			if event.ProjectID() != report.project.ProjectID() {
				return fieldError("activities", "every assigned Activity Event must belong to the Report Project")
			}
			eventScope := event.ScopeID()
			if eventScope == nil || *eventScope != scopeID {
				return fieldError("activities", "assigned Activity Event scope must match its Workstream report")
			}
			if _, exists := seenIDs[event.EventID()]; exists {
				return fieldError("activities", "Activity Event ids must be unique within a Handoff Report")
			}
			seenIDs[event.EventID()] = struct{}{}
			activityIDs = append(activityIDs, event.EventID())
		}
	}
	for _, event := range report.unassignedActivity {
		if err := event.Validate(); err != nil {
			return err
		}
		if event.ProjectID() != report.project.ProjectID() {
			return fieldError("unassigned_activity", "every unassigned Activity Event must belong to the Report Project")
		}
		if scope := event.ScopeID(); scope != nil {
			if _, selected := knownScopes[*scope]; selected {
				return fieldError("unassigned_activity", "Activity Event for a selected scope cannot be unassigned")
			}
		}
		if _, exists := seenIDs[event.EventID()]; exists {
			return fieldError("activities", "Activity Event ids must be unique within a Handoff Report")
		}
		seenIDs[event.EventID()] = struct{}{}
		activityIDs = append(activityIDs, event.EventID())
	}
	if !slices.Equal(report.activitySelection, activityIDs) {
		return fieldError("activity_selection", "must match projected Activity Event order")
	}
	return nil
}

func validateReportCoverage(report Report) error {
	coverage := report.coverage
	counts := []int{
		coverage.TotalIncludedWorkstreams,
		coverage.CatalogMatchedWorkstreams,
		coverage.SelectedWorkstreams,
		coverage.MissingHandoffWorkstreams,
		coverage.ReportedWithOmissions,
		coverage.UncheckedEvidenceWorkstreams,
		coverage.UnavailableEvidenceWorkstreams,
		coverage.ActivityWithoutHandoffWorkstreams,
		coverage.ActivityAfterHandoffWorkstreams,
		coverage.UnknownTimeEvents,
		coverage.UnassignedActivityCount,
		coverage.UnassignedActivityEvents,
	}
	for _, count := range counts {
		if count < 0 {
			return fieldError("coverage", "counts must be non-negative")
		}
	}
	if coverage.ActivityCoverage != ActivityNotConfigured && coverage.ActivityCoverage != ActivityCaptured &&
		coverage.ActivityCoverage != ActivityUnavailable {
		return fieldError("coverage.activity_coverage", "has an unsupported value")
	}
	if coverage.SelectedWorkstreams != len(report.workstreams) {
		return fieldError("coverage.selected_workstreams", "must match Workstream report count")
	}
	if coverage.TotalIncludedWorkstreams < len(report.workstreams) {
		return fieldError("coverage.total_included_workstreams", "cannot be smaller than the selected report")
	}
	if coverage.CatalogMatchedWorkstreams > coverage.TotalIncludedWorkstreams {
		return fieldError("coverage.catalog_matched_workstreams", "cannot exceed total_included_workstreams")
	}
	expected := Coverage{
		MissingHandoffWorkstreams:         0,
		ReportedWithOmissions:             0,
		UncheckedEvidenceWorkstreams:      0,
		UnavailableEvidenceWorkstreams:    0,
		ActivityWithoutHandoffWorkstreams: 0,
		ActivityAfterHandoffWorkstreams:   0,
		UnknownTimeEvents:                 0,
		UnassignedActivityCount:           len(report.unassignedActivity),
		UnassignedActivityEvents:          len(report.unassignedActivity),
	}
	for _, item := range report.workstreams {
		if item.handoffRef == nil {
			expected.MissingHandoffWorkstreams++
		}
		if item.reportingStatus == ReportingWithOmissions {
			expected.ReportedWithOmissions++
		}
		if item.handoffRef != nil && !item.evidenceChecked {
			expected.UncheckedEvidenceWorkstreams++
		}
		unavailable := item.evidenceUnavailable
		if item.evidenceChecked {
			for _, check := range item.evidenceChecks {
				if check.Status() == handoff.EvidenceUnavailable {
					unavailable = true
					break
				}
			}
		}
		if unavailable {
			expected.UnavailableEvidenceWorkstreams++
		}
		if item.activityStatus == ActivityWithoutHandoff {
			expected.ActivityWithoutHandoffWorkstreams++
		}
		if item.handoffActivityRelation != nil && *item.handoffActivityRelation == RelationAfterHandoff {
			expected.ActivityAfterHandoffWorkstreams++
		}
		for _, event := range item.activities {
			if event.EffectivePeriodTime() == nil {
				expected.UnknownTimeEvents++
			}
		}
	}
	for _, event := range report.unassignedActivity {
		if event.EffectivePeriodTime() == nil {
			expected.UnknownTimeEvents++
		}
	}
	actualValues := []int{
		coverage.MissingHandoffWorkstreams,
		coverage.ReportedWithOmissions,
		coverage.UncheckedEvidenceWorkstreams,
		coverage.UnavailableEvidenceWorkstreams,
		coverage.ActivityWithoutHandoffWorkstreams,
		coverage.ActivityAfterHandoffWorkstreams,
		coverage.UnknownTimeEvents,
		coverage.UnassignedActivityCount,
		coverage.UnassignedActivityEvents,
	}
	expectedValues := []int{
		expected.MissingHandoffWorkstreams,
		expected.ReportedWithOmissions,
		expected.UncheckedEvidenceWorkstreams,
		expected.UnavailableEvidenceWorkstreams,
		expected.ActivityWithoutHandoffWorkstreams,
		expected.ActivityAfterHandoffWorkstreams,
		expected.UnknownTimeEvents,
		expected.UnassignedActivityCount,
		expected.UnassignedActivityEvents,
	}
	if !slices.Equal(actualValues, expectedValues) {
		return fieldError("coverage", "must match the canonical report projection")
	}
	return nil
}

func validateReportSummary(report Report) error {
	var expected Summary
	for _, item := range report.workstreams {
		switch item.workStatus {
		case WorkContinuable:
			expected.ContinuableCount++
		case WorkBlocked:
			expected.BlockedCount++
		case WorkComplete:
			expected.CompleteCount++
		case WorkNoHandoff:
			expected.NoHandoffCount++
		}
	}
	if report.summary != expected {
		return fieldError("summary", "must match the canonical report projection")
	}
	return nil
}

func validWorkStatus(value WorkStatus) bool {
	return value == WorkContinuable || value == WorkBlocked || value == WorkComplete || value == WorkNoHandoff
}

func validReportingStatus(value ReportingStatus) bool {
	return value == ReportingReported || value == ReportingWithOmissions ||
		value == ReportingEvidenceMissing || value == ReportingNoHandoff
}

func validActivityStatus(value ActivityStatus) bool {
	return value == ActivityNone || value == ActivityAfterHandoff || value == ActivityWithoutHandoff ||
		value == ActivityCurrentOnly || value == ActivityUnknown
}

func validActivityRelation(value *HandoffActivityRelation) bool {
	return value == nil || *value == RelationAfterHandoff || *value == RelationNoActivityAfter || *value == RelationUnknown
}

func validDigest(value string) bool {
	if value == "" {
		return true
	}
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	digest := strings.TrimPrefix(value, "sha256:")
	if strings.ToLower(digest) != digest {
		return false
	}
	_, err := hex.DecodeString(digest)
	return err == nil
}

func cloneEvidenceChecks(values []handoff.EvidenceCheck) []handoff.EvidenceCheck {
	return slices.Clone(values)
}
