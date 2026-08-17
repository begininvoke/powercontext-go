package conformance_test

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/thunguo/powercontext-go/artifact"
	"github.com/thunguo/powercontext-go/artifact/experience"
	"github.com/thunguo/powercontext-go/artifact/handoff"
	"github.com/thunguo/powercontext-go/artifact/memory"
	"github.com/thunguo/powercontext-go/artifact/skill"
	"github.com/thunguo/powercontext-go/contextpack"
	"github.com/thunguo/powercontext-go/handoffreport"
	"github.com/thunguo/powercontext-go/inference"
	"github.com/thunguo/powercontext-go/internal/endpoint"
	"github.com/thunguo/powercontext-go/internal/httpapi"
	"github.com/thunguo/powercontext-go/internal/mcpapi"
	"github.com/thunguo/powercontext-go/internal/scheduler"
	"github.com/thunguo/powercontext-go/review"
	"github.com/thunguo/powercontext-go/runtime"
	"github.com/thunguo/powercontext-go/server"
	"github.com/thunguo/powercontext-go/source"
	"github.com/thunguo/powercontext-go/trigger"
)

type frozenDomainContract struct {
	Schema          string                 `json:"schema"`
	OracleCommit    string                 `json:"oracle_commit"`
	Constants       map[string]any         `json:"constants"`
	ErrorMappings   map[string]frozenError `json:"error_mappings"`
	MemoryCanonical frozenMemoryCanonical  `json:"memory_canonical"`
}

const oracleCommit = "9e23c336492c8bba16c6f26083298b6f484a91b0"

type frozenError struct {
	Status  int            `json:"status"`
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

type frozenCanonicalValue struct {
	Bytes string `json:"bytes"`
	Hash  string `json:"hash"`
}

type frozenMemoryCanonical struct {
	Normalization struct {
		Text   string `json:"text"`
		Kind   string `json:"kind"`
		Reason string `json:"reason"`
	} `json:"normalization"`
	Entry     frozenCanonicalValue `json:"entry"`
	Memory    frozenCanonicalValue `json:"memory"`
	Embedding struct {
		Hash               string    `json:"hash"`
		OverflowStableUnit []float64 `json:"overflow_stable_unit"`
	} `json:"embedding"`
}

func TestGoDomainConstantsMatchFrozenPythonOracle(t *testing.T) {
	contract := readDomainContract(t)
	want := goDomainConstants()
	if len(contract.Constants) != len(want) {
		t.Fatalf("constant count = %d, want %d", len(want), len(contract.Constants))
	}
	for name, pythonValue := range contract.Constants {
		goValue, ok := want[name]
		if !ok {
			t.Errorf("Python constant %s has no Go mapping", name)
			continue
		}
		if !equalContractScalar(goValue, pythonValue) {
			t.Errorf("%s = %v, want Python value %v", name, goValue, pythonValue)
		}
	}
}

func TestGoMemoryCanonicalBytesMatchFrozenPythonOracle(t *testing.T) {
	contract := readDomainContract(t).MemoryCanonical
	const separators = "\u001c\u001d\u001e\u001f"

	normalizedText, err := memory.NormalizeText(separators + " durable " + separators)
	if err != nil {
		t.Fatal(err)
	}
	normalizedKind, err := memory.NormalizeKind(separators + " integration-kind " + separators)
	if err != nil {
		t.Fatal(err)
	}
	reasonInput := separators + " user requested " + separators
	normalizedReason, err := memory.NormalizeReason(&reasonInput)
	if err != nil {
		t.Fatal(err)
	}
	if normalizedReason == nil || normalizedText != contract.Normalization.Text ||
		normalizedKind != contract.Normalization.Kind || *normalizedReason != contract.Normalization.Reason {
		t.Fatalf("Go normalization differs from frozen Python: text=%q kind=%q reason=%v", normalizedText, normalizedKind, normalizedReason)
	}

	sourceA, err := source.NewRef("content", "a")
	if err != nil {
		t.Fatal(err)
	}
	sourceB, err := source.NewRef("content", "b")
	if err != nil {
		t.Fatal(err)
	}
	parent, err := artifact.NewRef("experience", "z", 2)
	if err != nil {
		t.Fatal(err)
	}
	entryBytes, err := memory.EntryContentBytes(
		separators+" fact "+separators,
		separators+"Cafe\u0301  "+separators,
		[]source.Ref{sourceB, sourceA, sourceA},
		[]artifact.Ref{parent},
	)
	if err != nil {
		t.Fatal(err)
	}
	entryHash, err := memory.EntryContentHash(
		separators+" fact "+separators,
		separators+"Cafe\u0301  "+separators,
		[]source.Ref{sourceB, sourceA, sourceA},
		[]artifact.Ref{parent},
	)
	if err != nil {
		t.Fatal(err)
	}
	if string(entryBytes) != contract.Entry.Bytes || entryHash != contract.Entry.Hash {
		t.Fatalf("Go entry authority differs from Python:\nbytes=%s\nhash=%s", entryBytes, entryHash)
	}

	manifestEntry, err := memory.NewManifestEntry("entry-a", "version-a1", entryHash, memory.Active)
	if err != nil {
		t.Fatal(err)
	}
	versionID := "version-a1"
	memoryReason := separators + " introduced " + separators
	change, err := memory.NewChange(memory.Add, "entry-a", nil, &versionID, &memoryReason)
	if err != nil {
		t.Fatal(err)
	}
	content := memory.NewContent(memory.NewManifest([]memory.ManifestEntry{manifestEntry}), []memory.Change{change})
	memoryBytes, err := memory.ContentBytes(content)
	if err != nil {
		t.Fatal(err)
	}
	memoryHash, err := memory.ContentHash(content)
	if err != nil {
		t.Fatal(err)
	}
	if string(memoryBytes) != contract.Memory.Bytes || memoryHash != contract.Memory.Hash {
		t.Fatalf("Go Memory authority differs from Python:\nbytes=%s\nhash=%s", memoryBytes, memoryHash)
	}

	profile := memory.EmbeddingProfile{
		ProfileID: " profile-a ", Model: " model-a ", Dimension: 3,
		Distance: "l2", Normalization: "unit",
	}
	embeddingHash, err := memory.EmbeddingContentHash(profile, entryHash)
	if err != nil {
		t.Fatal(err)
	}
	unit, err := memory.NormalizeEmbedding([]float64{1e308, 1e308}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if embeddingHash != contract.Embedding.Hash || len(unit) != len(contract.Embedding.OverflowStableUnit) {
		t.Fatalf("Go embedding authority differs from Python: hash=%s unit=%v", embeddingHash, unit)
	}
	for index := range unit {
		if math.Abs(unit[index]-contract.Embedding.OverflowStableUnit[index]) > 1e-15 {
			t.Fatalf("Go embedding unit[%d] = %.17g, want Python %.17g", index, unit[index], contract.Embedding.OverflowStableUnit[index])
		}
	}
}

func equalContractScalar(left, right any) bool {
	leftValue, rightValue := reflect.ValueOf(left), reflect.ValueOf(right)
	leftNumber, leftOK := contractNumber(leftValue)
	rightNumber, rightOK := contractNumber(rightValue)
	if leftOK || rightOK {
		return leftOK && rightOK && leftNumber == rightNumber
	}
	return reflect.DeepEqual(left, right)
}

func contractNumber(value reflect.Value) (float64, bool) {
	switch value.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(value.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(value.Uint()), true
	case reflect.Float32, reflect.Float64:
		return value.Convert(reflect.TypeFor[float64]()).Float(), true
	default:
		return 0, false
	}
}

func TestGoErrorTaxonomyMatchesFrozenPythonOracle(t *testing.T) {
	contract := readDomainContract(t)
	cases := goErrorCases(t)
	if len(contract.ErrorMappings) != len(cases) {
		t.Fatalf("error mapping count = %d, want %d", len(cases), len(contract.ErrorMappings))
	}
	for name, pythonValue := range contract.ErrorMappings {
		err, ok := cases[name]
		if !ok {
			t.Errorf("Python error case %s has no Go mapping", name)
			continue
		}
		mapped := endpoint.MapError(err)
		got := frozenError{
			Status: mapped.StatusCode, Code: mapped.Code, Message: mapped.Message,
			Details: normalizeDetails(t, mapped.Details),
		}
		if !reflect.DeepEqual(got, pythonValue) {
			t.Errorf("%s = %#v, want frozen Python mapping %#v", name, got, pythonValue)
		}
	}
}

func readDomainContract(t *testing.T) frozenDomainContract {
	t.Helper()
	contents, err := os.ReadFile("testdata/python-v0.0.1/domain-contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var result frozenDomainContract
	if err := json.Unmarshal(contents, &result); err != nil {
		t.Fatal(err)
	}
	if result.Schema != "powercontext.python-v0.0.1.domain-contract.v1" || result.OracleCommit != oracleCommit {
		t.Fatalf("unexpected frozen contract identity: %#v", result)
	}
	return result
}

func goDomainConstants() map[string]any {
	return map[string]any{
		"powercontext.builtin.artifacts.experience.incubation.EXPERIENCE_INCUBATION_CURSOR_NAME":      experience.IncubationCursorName,
		"powercontext.builtin.artifacts.experience.incubation.EXPERIENCE_INCUBATION_REASON":           experience.IncubationReason,
		"powercontext.builtin.artifacts.experience.incubation.EXPERIENCE_INCUBATION_WINDOW_LIMIT":     experience.IncubationWindowLimit,
		"powercontext.builtin.artifacts.experience.incubation.MAX_EXPERIENCE_CANDIDATE_EVIDENCE":      experience.MaxCandidateEvidence,
		"powercontext.builtin.artifacts.experience.incubation.MAX_EXPERIENCE_INCUBATION_SOURCES":      experience.MaxIncubationSources,
		"powercontext.builtin.artifacts.experience.incubation.MAX_EXPERIENCE_INCUBATION_SOURCE_CHARS": experience.MaxIncubationSourceChars,
		"powercontext.builtin.artifacts.experience.incubation.TASK_OUTCOME_SOURCE_KIND":               experience.TaskOutcomeSourceKind,
		"powercontext.builtin.artifacts.experience.models.MAX_EXPERIENCE_FIELD_LENGTH":                experience.MaxFieldLength,
		"powercontext.builtin.artifacts.generation.MAX_GENERATION_EVIDENCE":                           artifact.MaxGenerationEvidence,
		"powercontext.builtin.artifacts.generation.MAX_GENERATION_EVIDENCE_CHARS":                     artifact.MaxGenerationEvidenceChars,
		"powercontext.builtin.artifacts.handoff.models.DEFAULT_HANDOFF_MAX_BYTES":                     handoff.DefaultMaxBytes,
		"powercontext.builtin.artifacts.handoff.models.MAX_HANDOFF_BYTES":                             handoff.MaxBytes,
		"powercontext.builtin.artifacts.handoff.models.MAX_HANDOFF_CITATIONS":                         handoff.MaxCitations,
		"powercontext.builtin.artifacts.handoff.models.MAX_HANDOFF_OMISSIONS":                         handoff.MaxOmissions,
		"powercontext.builtin.artifacts.handoff.models.MAX_HANDOFF_STATE_STATEMENTS":                  handoff.MaxStateStatements,
		"powercontext.builtin.artifacts.handoff.models.MAX_HANDOFF_TEXT_LENGTH":                       handoff.MaxTextLength,
		"powercontext.builtin.artifacts.handoff.models.MIN_HANDOFF_MAX_BYTES":                         handoff.MinMaxBytes,
		"powercontext.builtin.artifacts.skill.external.MAX_EXTERNAL_SKILL_FILES":                      skill.MaxExternalFiles,
		"powercontext.builtin.artifacts.skill.external.MAX_EXTERNAL_SKILL_MANIFEST_BYTES":             skill.MaxExternalManifestBytes,
		"powercontext.builtin.artifacts.skill.external.MAX_EXTERNAL_SKILL_PACKAGE_BYTES":              skill.MaxExternalPackageBytes,
		"powercontext.builtin.artifacts.skill.models.MAX_SKILL_DESCRIPTION_LENGTH":                    skill.MaxDescriptionLength,
		"powercontext.builtin.artifacts.skill.models.MAX_SKILL_INSTRUCTIONS_LENGTH":                   skill.MaxInstructionsLength,
		"powercontext.builtin.artifacts.skill.models.MAX_SKILL_NAME_LENGTH":                           skill.MaxNameLength,
		"powercontext.builtin.artifacts.skill.models.MAX_SKILL_VALIDATION_ITEMS":                      skill.MaxValidationItems,
		"powercontext.builtin.artifacts.skill.models.MAX_SKILL_VALIDATION_ITEM_LENGTH":                skill.MaxValidationItemLength,
		"powercontext.builtin.handoff_report.catalog_store.DEFAULT_CATALOG_PAGE_SIZE":                 handoffreport.DefaultCatalogPageSize,
		"powercontext.builtin.handoff_report.catalog_store.MAX_CATALOG_PAGE_SIZE":                     handoffreport.MaxCatalogPageSize,
		"powercontext.builtin.handoff_report.models.MAX_PROJECT_KEY_LENGTH":                           handoffreport.MaxProjectKeyLength,
		"powercontext.builtin.handoff_report.models.MAX_REPORT_AGENT_LABEL_LENGTH":                    handoffreport.MaxReportAgentLabelLength,
		"powercontext.builtin.handoff_report.models.MAX_REPORT_DESCRIPTION_LENGTH":                    handoffreport.MaxReportDescriptionLength,
		"powercontext.builtin.handoff_report.models.MAX_REPORT_EVIDENCE_REFS":                         handoffreport.MaxReportEvidenceRefs,
		"powercontext.builtin.handoff_report.models.MAX_REPORT_EXTERNAL_ID_LENGTH":                    handoffreport.MaxReportExternalIDLength,
		"powercontext.builtin.handoff_report.models.MAX_REPORT_EXTERNAL_REFS":                         handoffreport.MaxReportExternalRefs,
		"powercontext.builtin.handoff_report.models.MAX_REPORT_ID_LENGTH":                             handoffreport.MaxReportIDLength,
		"powercontext.builtin.handoff_report.models.MAX_REPORT_LABELS":                                handoffreport.MaxReportLabels,
		"powercontext.builtin.handoff_report.models.MAX_REPORT_LABEL_LENGTH":                          handoffreport.MaxReportLabelLength,
		"powercontext.builtin.handoff_report.models.MAX_REPORT_NORMALIZED_REMOTE_LENGTH":              handoffreport.MaxReportNormalizedRemoteLength,
		"powercontext.builtin.handoff_report.models.MAX_REPORT_PROVIDER_LENGTH":                       handoffreport.MaxReportProviderLength,
		"powercontext.builtin.handoff_report.models.MAX_REPORT_REPOSITORY_ID_LENGTH":                  handoffreport.MaxReportRepositoryIDLength,
		"powercontext.builtin.handoff_report.models.MAX_REPORT_SOURCE_SUMMARY_LENGTH":                 handoffreport.MaxReportSourceSummaryLength,
		"powercontext.builtin.handoff_report.models.MAX_REPORT_SUBPATH_LENGTH":                        handoffreport.MaxReportSubpathLength,
		"powercontext.builtin.handoff_report.models.MAX_REPORT_TITLE_LENGTH":                          handoffreport.MaxReportTitleLength,
		"powercontext.builtin.handoff_report.models.MAX_REPORT_URL_LENGTH":                            handoffreport.MaxReportURLLength,
		"powercontext.builtin.handoff_report.models.MAX_WORKSPACE_INSTANCE_ID_LENGTH":                 handoffreport.MaxWorkspaceInstanceIDLength,
		"powercontext.builtin.handoff_report.models.MAX_WORKSTREAM_KEY_LENGTH":                        handoffreport.MaxWorkstreamKeyLength,
		"powercontext.builtin.handoff_report.report.MAX_REPORT_ACTIVITIES":                            handoffreport.MaxReportActivities,
		"powercontext.builtin.handoff_report.report.MAX_REPORT_WORKSTREAMS":                           handoffreport.MaxReportWorkstreams,
		"powercontext.builtin.handoff_report.selection.DEFAULT_HANDOFF_SELECTION_ATTEMPTS":            handoffreport.DefaultSelectionAttempts,
		"powercontext.builtin.handoff_report.selection.MAX_HANDOFF_SELECTION_ATTEMPTS":                handoffreport.MaxSelectionAttempts,
		"powercontext.builtin.review.models.DEFAULT_CANDIDATE_PAGE_SIZE":                              review.DefaultPageSize,
		"powercontext.builtin.review.models.MAX_CANDIDATE_EVIDENCE":                                   review.MaxEvidence,
		"powercontext.builtin.review.models.MAX_CANDIDATE_PAGE_SIZE":                                  review.MaxPageSize,
		"powercontext.builtin.review.models.MAX_CANDIDATE_REASON_LENGTH":                              review.MaxReasonLength,
		"powercontext.builtin.runtime.models.PREPARED_CONTEXT_SCHEMA":                                 contextpack.SchemaV1,
		"powercontext.builtin.runtime.readiness.READINESS_PROBE_CACHE_SECONDS":                        runtime.DefaultReadinessCacheTTL.Seconds(),
		"powercontext.builtin.runtime.readiness.READINESS_PROBE_TIMEOUT_SECONDS":                      runtime.DefaultReadinessProbeTimeout.Seconds(),
		"powercontext.builtin.runtime.readiness.READINESS_PROBE_TRANSIENT_CACHE_SECONDS":              runtime.TransientReadinessCacheTTL.Seconds(),
		"powercontext.builtin.runtime.scheduler.EXPERIENCE_INCUBATION_JOB_ID":                         scheduler.ExperienceIncubationJobID,
		"powercontext.builtin.runtime.scheduler.SCHEDULER_TABLE":                                      scheduler.TableName,
		"powercontext.builtin.runtime.scheduler.SOURCE_WINDOW_JOB_ID":                                 scheduler.SourceWindowJobID,
		"powercontext.builtin.sources.content.CONTENT_SOURCE_NAME":                                    source.ContentType,
		"powercontext.builtin.sources.external_skill.EXTERNAL_SKILL_SNAPSHOT_SOURCE_NAME":             skill.ExternalSnapshotSourceType,
		"powercontext.builtin.triggers.handoff.HANDOFF_BOUNDARY_TRIGGER_NAME":                         trigger.HandoffBoundaryName,
		"powercontext.builtin.triggers.source_window.SOURCE_WINDOW_TRIGGER_NAME":                      trigger.SourceWindowName,
		"powercontext.limits.MAX_ARTIFACT_FAMILY_LENGTH":                                              artifact.MaxFamilyLength,
		"powercontext.limits.MAX_ARTIFACT_ID_LENGTH":                                                  artifact.MaxIDLength,
		"powercontext.limits.MAX_BINDING_NAME_LENGTH":                                                 source.MaxBindingNameLength,
		"powercontext.limits.MAX_EXTERNAL_SKILL_DESCRIPTION_LENGTH":                                   skill.MaxDescriptionLength,
		"powercontext.limits.MAX_EXTERNAL_SKILL_HOST_ID_LENGTH":                                       skill.MaxExternalHostIDLength,
		"powercontext.limits.MAX_EXTERNAL_SKILL_LOCATOR_LENGTH":                                       skill.MaxExternalLocatorLength,
		"powercontext.limits.MAX_EXTERNAL_SKILL_NAME_LENGTH":                                          skill.MaxNameLength,
		"powercontext.limits.MAX_SCOPE_ID_LENGTH":                                                     handoffreport.MaxScopeIDLength,
		"powercontext.limits.MAX_SOURCE_ID_LENGTH":                                                    source.MaxIDLength,
		"powercontext.limits.MAX_SOURCE_TYPE_LENGTH":                                                  source.MaxTypeLength,
		"powercontext.server.app.MAX_HANDOFF_REPORT_BYTES":                                            handoffreport.MaxReportBytes,
		"powercontext.server.app.REPORT_DIGEST_HEADER":                                                httpapi.ReportDigestHeader,
		"powercontext.server.app.REPORT_SELECTION_DIGEST_HEADER":                                      httpapi.SelectionDigestHeader,
		"powercontext.server.app.REQUEST_ID_HEADER":                                                   httpapi.RequestIDHeader,
		"powercontext.server.mcp.MCP_PATH":                                                            server.DefaultMCPPath,
		"powercontext.server.mcp.MCP_SERVER_NAME":                                                     mcpapi.ServerName,
	}
}

func goErrorCases(t *testing.T) map[string]error {
	t.Helper()
	target, err := artifact.NewRef("experience", "exp-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	current, err := artifact.NewRef("experience", "exp-1", 2)
	if err != nil {
		t.Fatal(err)
	}
	expectedVersion, currentVersion, estimatedBytes := 2, 4, 10_485_761
	return map[string]error{
		"runtime_not_ready":              &endpoint.RuntimeNotReadyError{},
		"external_registry_unavailable":  &skill.ExternalRegistryUnavailableError{},
		"external_not_found":             &skill.ExternalNotFoundError{ExternalSkillID: "skill-1"},
		"external_snapshot_unavailable":  &skill.ExternalSnapshotUnavailableError{ExternalSkillID: "skill-1"},
		"generation_unavailable":         &review.GenerationCapabilityUnavailableError{Family: "skill"},
		"candidate_not_found":            &review.CandidateNotFoundError{CandidateID: "candidate-1"},
		"candidate_conflict":             &review.CandidateConflictError{CandidateID: "candidate-1", ExpectedVersion: 2, CurrentVersion: 4},
		"artifact_target_conflict":       &review.ArtifactTargetConflictError{Target: target, Current: current},
		"candidate_terminal":             &review.CandidateTerminalError{CandidateID: "candidate-1", Status: review.Approved},
		"invalid_candidate":              &review.InvalidCandidateError{Field: "proposal", Detail: "invalid"},
		"project_not_found":              &handoffreport.ProjectNotFoundError{ProjectID: "project-1"},
		"workstream_not_found":           &handoffreport.WorkstreamNotFoundError{ScopeID: "scope-1"},
		"workspace_not_found":            &handoffreport.WorkspaceBindingNotFoundError{WorkspaceInstanceID: "workspace-1"},
		"project_conflict":               &handoffreport.ProjectConflictError{ProjectID: "project-1", ExpectedVersion: &expectedVersion, CurrentVersion: &currentVersion},
		"workstream_conflict":            &handoffreport.WorkstreamConflictError{ScopeID: "scope-1", ExpectedVersion: &expectedVersion, CurrentVersion: &currentVersion},
		"scope_already_grouped":          &handoffreport.ScopeAlreadyGroupedError{ScopeID: "scope-1", ProjectID: "project-1"},
		"workspace_conflict":             &handoffreport.WorkspaceBindingConflictError{WorkspaceInstanceID: "workspace-1", ExpectedVersion: &expectedVersion, CurrentVersion: &currentVersion},
		"activity_conflict":              &handoffreport.ActivityEventConflictError{Source: handoffreport.ActivityCodingSession, SourceEventID: "session-1"},
		"report_busy":                    &handoffreport.BusyError{Attempts: 3},
		"report_too_large":               &handoffreport.TooLargeError{SelectedWorkstreams: 2, SelectedActivities: 3, EstimatedBytes: &estimatedBytes},
		"report_inconsistent":            &handoffreport.InconsistentError{ScopeID: "scope-1"},
		"report_invalid_catalog":         &handoffreport.CatalogArgumentError{Field: "period", Detail: "invalid"},
		"report_invalid_activity":        &handoffreport.InvalidActivityEventError{Field: "source", Detail: "invalid"},
		"report_invalid_repository":      &handoffreport.InvalidActivityRepositoryArgumentError{Field: "limit", Detail: "invalid"},
		"report_unavailable":             &handoffreport.EvidenceCheckUnavailableError{},
		"artifact_not_found":             &artifact.NotFoundError{Ref: target},
		"memory_not_found":               &memory.EntryNotFoundError{EntryID: "entry-1"},
		"source_conflict":                &source.ConflictError{Field: "identity", Value: "source-1"},
		"revision_conflict":              &artifact.RevisionConflictError{Requested: target, Current: current},
		"memory_inactive":                &memory.EntryInactiveError{EntryID: "entry-1"},
		"capability_not_supported":       &memory.CapabilityNotSupportedError{Capability: "vector"},
		"invalid_memory_candidate":       &memory.InvalidCandidateError{Code: "canonical"},
		"invalid_memory_evidence":        &memory.InvalidEvidenceError{Code: "source-outside"},
		"invalid_memory_citation":        &memory.InvalidCitationError{Code: "hash-mismatch"},
		"handoff_scope_mismatch":         &handoff.ScopeMismatchError{Expected: "scope-1", Actual: "scope-2"},
		"invalid_handoff_reference":      &handoff.InvalidReferenceError{},
		"invalid_runtime_request":        &runtime.InvalidScopeError{Detail: "since-revision"},
		"inference_timeout":              inference.NewTimeoutError("generation", time.Second),
		"inference_unavailable":          inference.NewUnavailableError("generation"),
		"handoff_evidence_not_found":     &handoff.EvidenceUnavailableError{},
		"handoff_generation_unavailable": &handoff.GenerationUnavailableError{},
		"invalid_handoff_generation":     &handoff.InvalidGenerationError{Code: "budget"},
		"unknown":                        errors.New("secret backend detail"),
	}
}

func normalizeDetails(t *testing.T, value map[string]any) map[string]any {
	t.Helper()
	if value == nil {
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
