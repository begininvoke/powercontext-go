package handoffreport

import (
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"github.com/ob-labs/powercontext-go/artifact/handoff"
)

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
