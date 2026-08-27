package handoffreport

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/ob-labs/powercontext-go/artifact/handoff"
)

func (v Report) MarshalJSON() ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	payload, err := v.object(true, false)
	if err != nil {
		return nil, err
	}
	return marshalUnescaped(payload)
}

// object is the single JSON projection used by wire rendering and canonical
// report digests. The canonical switch changes timestamp spelling only; it
// must not change the selected authority data.
func (v Report) object(includeReportDigest, canonical bool) (map[string]any, error) {
	workstreams := make([]any, len(v.workstreams))
	for index, item := range v.workstreams {
		mapped, err := workstreamReportObject(item, canonical)
		if err != nil {
			return nil, err
		}
		workstreams[index] = mapped
	}
	end := make([]any, len(v.endSelection))
	for index, item := range v.endSelection {
		end[index] = selectionObject(item)
	}
	var baseline any
	if v.baselinePresent {
		values := make([]any, len(v.baselineSelection))
		for index, item := range v.baselineSelection {
			values[index] = selectionObject(item)
		}
		baseline = values
	}
	unassigned := make([]any, len(v.unassignedActivity))
	for index, event := range v.unassignedActivity {
		unassigned[index] = activityEventObject(event, canonical)
	}
	generatedAt := JSONTimestampText(v.generatedAt)
	if canonical {
		generatedAt = UTCText(v.generatedAt)
	}
	payload := map[string]any{
		"schema":                ReportSchemaVersion,
		"trust":                 ReportTrust,
		"locale":                v.locale,
		"format":                v.format,
		"report_kind":           v.reportKind,
		"renderer_version":      v.rendererVersion,
		"generated_at":          generatedAt,
		"selection_consistency": ReportSelectionConsistency,
		"project":               v.project,
		"project_revision":      v.project.Version(),
		"normalized_filters":    v.normalizedFilters,
		"normalized_period":     nil,
		"period_comparison":     nil,
		"baseline_selection":    baseline,
		"end_selection":         end,
		"activity_cursor":       v.activityCursor,
		"activity_selection":    slices.Clone(v.activitySelection),
		"selection_digest":      nullableDigest(v.selectionDigest),
		"report_digest":         nullableDigest(v.reportDigest),
		"coverage":              coverageObject(v.coverage),
		"summary":               summaryObject(v.summary),
		"unassigned_activity":   unassigned,
		"workstreams":           workstreams,
	}
	if v.normalizedPeriod != nil {
		payload["normalized_period"] = v.normalizedPeriod
	}
	if v.periodComparison != nil {
		payload["period_comparison"] = comparisonObject(*v.periodComparison, canonical)
	}
	if !includeReportDigest {
		delete(payload, "report_digest")
	}
	return payload, nil
}

func workstreamReportObject(v WorkstreamReport, canonical bool) (map[string]any, error) {
	var content any
	if v.content != nil {
		encoded, err := handoff.RenderContent(*v.content)
		if err != nil {
			return nil, err
		}
		content, err = DecodeJSONValue(encoded)
		if err != nil {
			return nil, err
		}
	}
	var checks any = "not_checked"
	if v.evidenceChecked {
		values := make([]any, len(v.evidenceChecks))
		for index, check := range v.evidenceChecks {
			mapped, err := evidenceCheckObject(check)
			if err != nil {
				return nil, err
			}
			values[index] = mapped
		}
		checks = values
	}
	activities := make([]any, len(v.activities))
	for index, event := range v.activities {
		activities[index] = activityEventObject(event, canonical)
	}
	var relation any
	if v.handoffActivityRelation != nil {
		relation = *v.handoffActivityRelation
	}
	history := make([]any, len(v.handoffHistory))
	for index, summary := range v.handoffHistory {
		ref := summary.reference
		history[index] = map[string]any{
			"reference":           artifactRefMap(&ref),
			"objective_excerpt":   summary.objectiveExcerpt,
			"disposition":         summary.disposition,
			"next_action_excerpt": summary.nextActionExcerpt,
			"state_count":         summary.stateCount,
			"omission_count":      summary.omissionCount,
		}
	}
	return map[string]any{
		"workstream":                v.workstream,
		"continuity":                v.continuity,
		"handoff_ref":               artifactRefMap(v.handoffRef),
		"content":                   content,
		"handoff_revision_count":    v.handoffRevisionCount,
		"handoff_history_truncated": v.handoffHistoryTruncated,
		"handoff_history":           history,
		"evidence_checks":           checks,
		"evidence_unavailable":      v.evidenceUnavailable,
		"activities":                activities,
		"work_status":               v.workStatus,
		"reporting_status":          v.reportingStatus,
		"activity_status":           v.activityStatus,
		"handoff_activity_relation": relation,
		"observed_activity_count":   v.observedActivityCount,
	}, nil
}

func evidenceCheckObject(v handoff.EvidenceCheck) (map[string]any, error) {
	unavailableEvidence := v.UnavailableEvidence()
	unavailable := make([]any, len(unavailableEvidence))
	for index, citation := range unavailableEvidence {
		mapped, err := citationObject(citation)
		if err != nil {
			return nil, err
		}
		unavailable[index] = mapped
	}
	var stateIndex any
	if index := v.StateIndex(); index != nil {
		stateIndex = *index
	}
	return map[string]any{
		"claim": v.Claim(), "state_index": stateIndex, "status": v.Status(),
		"unavailable_evidence": unavailable,
	}, nil
}

func citationObject(value handoff.Citation) (map[string]any, error) {
	switch citation := value.(type) {
	case handoff.SourceCitation:
		ref := citation.Ref()
		return map[string]any{"kind": "source", "source_ref": map[string]any{"source_type": ref.Type(), "source_id": ref.ID()}}, nil
	case handoff.ArtifactCitation:
		ref := citation.Ref()
		return map[string]any{"kind": "artifact", "artifact_ref": map[string]any{"family": ref.Family(), "artifact_id": ref.ID(), "revision": ref.Revision()}}, nil
	case handoff.MemoryCitation:
		entry := citation.Citation()
		return map[string]any{"kind": "memory", "memory_citation": map[string]any{"memory_ref": map[string]any{"family": entry.MemoryRef.Family(), "artifact_id": entry.MemoryRef.ID(), "revision": entry.MemoryRef.Revision()}, "entry_id": entry.EntryID, "entry_version_id": entry.EntryVersionID}}, nil
	default:
		return nil, fmt.Errorf("unsupported Handoff citation %T", value)
	}
}

func selectionObject(v SelectionEntry) map[string]any {
	return map[string]any{"scope_id": v.scopeID, "workstream_revision": v.workstreamRevision, "status": v.status, "handoff_ref": artifactRefMap(v.handoffRef)}
}

func coverageObject(v Coverage) map[string]any {
	return map[string]any{
		"total_included_workstreams":           v.TotalIncludedWorkstreams,
		"catalog_matched_workstreams":          v.CatalogMatchedWorkstreams,
		"selected_workstreams":                 v.SelectedWorkstreams,
		"missing_handoff_workstreams":          v.MissingHandoffWorkstreams,
		"reported_with_omissions":              v.ReportedWithOmissions,
		"unchecked_evidence_workstreams":       v.UncheckedEvidenceWorkstreams,
		"unavailable_evidence_workstreams":     v.UnavailableEvidenceWorkstreams,
		"activity_without_handoff_workstreams": v.ActivityWithoutHandoffWorkstreams,
		"activity_after_handoff_workstreams":   v.ActivityAfterHandoffWorkstreams,
		"unknown_time_events":                  v.UnknownTimeEvents,
		"unassigned_activity_count":            v.UnassignedActivityCount,
		"unassigned_activity_events":           v.UnassignedActivityEvents,
		"activity_coverage":                    v.ActivityCoverage,
	}
}

func summaryObject(v Summary) map[string]any {
	return map[string]any{"continuable_count": v.ContinuableCount, "blocked_count": v.BlockedCount, "complete_count": v.CompleteCount, "no_handoff_count": v.NoHandoffCount}
}

func comparisonObject(v PeriodComparison, canonical bool) map[string]any {
	start, end := JSONTimestampText(v.PreviousStart), JSONTimestampText(v.PreviousEnd)
	if canonical {
		start, end = UTCText(v.PreviousStart), UTCText(v.PreviousEnd)
	}
	return map[string]any{"previous_start": start, "previous_end": end, "current_activity_count": v.CurrentActivityCount, "previous_activity_count": v.PreviousActivityCount, "activity_delta": v.ActivityDelta, "handoff_boundary_coverage": "unavailable"}
}

func JSONValue(value any) (any, error) {
	encoded, err := marshalUnescaped(value)
	if err != nil {
		return nil, err
	}
	return DecodeJSONValue(encoded)
}

func DecodeJSONValue(encoded []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

func marshalUnescaped(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'}), nil
}

func nullableDigest(value string) any {
	if value == "" {
		return nil
	}
	return value
}
