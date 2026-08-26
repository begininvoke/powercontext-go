package handoffreport

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ob-labs/powercontext-go/artifact"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const (
	ProjectSchemaVersion            = "powercontext.project.v1"
	WorkstreamSchemaVersion         = "powercontext.workstream.v1"
	ActivitySchemaVersion           = "powercontext.handoff-report-activity.v1"
	ActivityTrust                   = "untrusted_observation"
	MaxReportIDLength               = 256
	MaxProjectKeyLength             = 64
	MaxWorkstreamKeyLength          = 64
	MaxReportTitleLength            = 256
	MaxReportDescriptionLength      = 2_000
	MaxReportLabelLength            = 128
	MaxReportProviderLength         = 64
	MaxReportActivitySourceLength   = 64
	MaxReportAgentLabelLength       = 128
	MaxReportSourceSummaryLength    = 2_000
	MaxReportExternalIDLength       = 256
	MaxReportURLLength              = 2_048
	MaxReportRepositoryIDLength     = 256
	MaxReportNormalizedRemoteLength = 2_048
	MaxReportSubpathLength          = 1_024
	MaxWorkspaceInstanceIDLength    = 256
	MaxScopeIDLength                = 256
	MaxReportExternalRefs           = 32
	MaxReportLabels                 = 32
	MaxReportEvidenceRefs           = 32
	DefaultCatalogPageSize          = 50
	MaxCatalogPageSize              = 100
	MaxReportWorkstreams            = 100
	MaxReportActivities             = 5_000
	DefaultSelectionAttempts        = 3
	MaxSelectionAttempts            = 5
	MaxReportBytes                  = 10 * 1024 * 1024
)

type Locale string

const (
	LocaleChinese Locale = "zh-CN"
	LocaleEnglish Locale = "en"
)

type CatalogState string

const (
	CatalogIncluded CatalogState = "included"
	CatalogArchived CatalogState = "archived"
)

type WorkstreamKind string

const (
	WorkstreamFeature    WorkstreamKind = "feature"
	WorkstreamBug        WorkstreamKind = "bug"
	WorkstreamRefactor   WorkstreamKind = "refactor"
	WorkstreamOperations WorkstreamKind = "operations"
	WorkstreamResearch   WorkstreamKind = "research"
	WorkstreamOther      WorkstreamKind = "other"
)

type ExternalReferenceKind string

const (
	ExternalIssue       ExternalReferenceKind = "issue"
	ExternalTask        ExternalReferenceKind = "task"
	ExternalPullRequest ExternalReferenceKind = "pull_request"
	ExternalBranch      ExternalReferenceKind = "branch"
	ExternalFeature     ExternalReferenceKind = "feature"
	ExternalRelease     ExternalReferenceKind = "release"
	ExternalProgram     ExternalReferenceKind = "program"
	ExternalOther       ExternalReferenceKind = "other"
)

type ActivitySource string

const (
	ActivityHandoffObservation ActivitySource = "handoff_observation"
	ActivityGitCommit          ActivitySource = "git_commit"
	ActivityGitWorktree        ActivitySource = "git_worktree"
	ActivityCodingSession      ActivitySource = "coding_session"
	ActivityOther              ActivitySource = "other"
)

type TimeBasis string

const (
	TimeSourceReported TimeBasis = "source_reported"
	TimeHostObserved   TimeBasis = "host_observed"
	TimeFirstSeen      TimeBasis = "first_seen"
	TimeCurrentOnly    TimeBasis = "current_only"
	TimeUnknown        TimeBasis = "unknown"
)

type RepositoryProvider string

const (
	RepositoryGitHub RepositoryProvider = "github"
	RepositoryGitLab RepositoryProvider = "gitlab"
	RepositoryLocal  RepositoryProvider = "local"
	RepositoryOther  RepositoryProvider = "other"
)

type WorkspaceBindingState string

const (
	WorkspaceConfirmed WorkspaceBindingState = "confirmed"
	WorkspaceDetached  WorkspaceBindingState = "detached"
)

type SelectionStatus string

const (
	SelectionSelected  SelectionStatus = "selected"
	SelectionNoHandoff SelectionStatus = "no_handoff"
)

type ExternalReference struct {
	kind       ExternalReferenceKind
	provider   string
	externalID string
	url        *string
}

func NewExternalReference(kind ExternalReferenceKind, provider, externalID string, target *string) (ExternalReference, error) {
	value := ExternalReference{kind: kind, provider: provider, externalID: externalID, url: cloneString(target)}
	if err := value.Validate(); err != nil {
		return ExternalReference{}, err
	}
	return value, nil
}
func (v ExternalReference) Validate() error {
	if !validExternalKind(v.kind) {
		return fieldError("kind", "has an unsupported value")
	}
	if err := requireText("provider", v.provider, MaxReportProviderLength); err != nil {
		return err
	}
	if err := requireText("external_id", v.externalID, MaxReportExternalIDLength); err != nil {
		return err
	}
	if err := requireOptionalText("url", v.url, MaxReportURLLength); err != nil {
		return err
	}
	return nil
}
func (v ExternalReference) Kind() ExternalReferenceKind { return v.kind }
func (v ExternalReference) Provider() string            { return v.provider }
func (v ExternalReference) ExternalID() string          { return v.externalID }
func (v ExternalReference) URL() *string                { return cloneString(v.url) }
func (v ExternalReference) key() string {
	return string(v.kind) + "\x00" + v.provider + "\x00" + v.externalID + "\x00" + optionalKey(v.url)
}
func (v ExternalReference) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"kind": v.kind, "provider": v.provider, "external_id": v.externalID, "url": v.url})
}
func (v *ExternalReference) UnmarshalJSON(data []byte) error {
	var dto struct {
		Kind       ExternalReferenceKind `json:"kind"`
		Provider   string                `json:"provider"`
		ExternalID string                `json:"external_id"`
		URL        *string               `json:"url"`
	}
	if err := decodeStrict(data, &dto); err != nil {
		return err
	}
	value, err := NewExternalReference(dto.Kind, dto.Provider, dto.ExternalID, dto.URL)
	if err == nil {
		*v = value
	}
	return err
}

type RepositoryRef struct {
	provider                                RepositoryProvider
	repositoryID, normalizedRemote, subpath *string
}

func NewRepositoryRef(provider RepositoryProvider, repositoryID, normalizedRemote, subpath *string) (RepositoryRef, error) {
	if !validRepositoryProvider(provider) {
		return RepositoryRef{}, fieldError("provider", "has an unsupported value")
	}
	if err := requireOptionalText("repository_id", repositoryID, MaxReportRepositoryIDLength); err != nil {
		return RepositoryRef{}, err
	}
	if err := requireOptionalText("normalized_remote", normalizedRemote, MaxReportNormalizedRemoteLength); err != nil {
		return RepositoryRef{}, err
	}
	if err := requireOptionalText("subpath", subpath, MaxReportSubpathLength); err != nil {
		return RepositoryRef{}, err
	}
	if repositoryID == nil && normalizedRemote == nil && subpath == nil {
		return RepositoryRef{}, fieldError("repository_ref", "must contain a repository id, remote, or subpath")
	}
	if normalizedRemote != nil && strings.ContainsAny(*normalizedRemote, "@?#") {
		return RepositoryRef{}, fieldError("normalized_remote", "must not contain credentials or query fragments")
	}
	remote, err := normalizeRepositoryRemote(normalizedRemote)
	if err != nil {
		return RepositoryRef{}, err
	}
	cleanSubpath, err := normalizeRepositorySubpath(subpath)
	if err != nil {
		return RepositoryRef{}, err
	}
	return RepositoryRef{provider: provider, repositoryID: cloneString(repositoryID), normalizedRemote: remote, subpath: cleanSubpath}, nil
}
func (v RepositoryRef) Provider() RepositoryProvider { return v.provider }
func (v RepositoryRef) RepositoryID() *string        { return cloneString(v.repositoryID) }
func (v RepositoryRef) NormalizedRemote() *string    { return cloneString(v.normalizedRemote) }
func (v RepositoryRef) Subpath() *string             { return cloneString(v.subpath) }
func (v RepositoryRef) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"provider": v.provider, "repository_id": v.repositoryID, "normalized_remote": v.normalizedRemote, "subpath": v.subpath})
}
func (v *RepositoryRef) UnmarshalJSON(data []byte) error {
	var dto struct {
		Provider         RepositoryProvider `json:"provider"`
		RepositoryID     *string            `json:"repository_id"`
		NormalizedRemote *string            `json:"normalized_remote"`
		Subpath          *string            `json:"subpath"`
	}
	if err := decodeStrict(data, &dto); err != nil {
		return err
	}
	value, err := NewRepositoryRef(dto.Provider, dto.RepositoryID, dto.NormalizedRemote, dto.Subpath)
	if err == nil {
		*v = value
	}
	return err
}

type ProjectDescriptor struct {
	projectID, projectKey, title string
	description                  *string
	defaultLocale                Locale
	timezone                     string
	catalogState                 CatalogState
	version                      int
}

func NewProjectDescriptor(projectID, projectKey, title string, description *string, locale Locale, timezone string, state CatalogState, version int) (ProjectDescriptor, error) {
	value := ProjectDescriptor{
		projectID: projectID, projectKey: projectKey, title: title, description: cloneString(description),
		defaultLocale: locale, timezone: timezone, catalogState: state, version: version,
	}
	if err := value.Validate(); err != nil {
		return ProjectDescriptor{}, err
	}
	return value, nil
}
func (v ProjectDescriptor) Validate() error {
	for _, item := range []struct {
		name, value string
		max         int
	}{{"project_id", v.projectID, MaxReportIDLength}, {"project_key", v.projectKey, MaxProjectKeyLength}, {"title", v.title, MaxReportTitleLength}, {"timezone", v.timezone, MaxReportIDLength}} {
		if err := requireText(item.name, item.value, item.max); err != nil {
			return err
		}
	}
	if err := requireOptionalText("description", v.description, MaxReportDescriptionLength); err != nil {
		return err
	}
	if v.defaultLocale != LocaleChinese && v.defaultLocale != LocaleEnglish {
		return fieldError("default_locale", "has an unsupported value")
	}
	if v.timezone == "Local" {
		return fieldError("timezone", "must be a recognized IANA timezone")
	}
	if _, err := time.LoadLocation(v.timezone); err != nil {
		return fieldError("timezone", "must be a recognized IANA timezone")
	}
	if v.catalogState != CatalogIncluded && v.catalogState != CatalogArchived {
		return fieldError("catalog_state", "has an unsupported value")
	}
	if v.version < 1 {
		return fieldError("version", "must be a positive integer")
	}
	return nil
}
func (v ProjectDescriptor) ProjectID() string          { return v.projectID }
func (v ProjectDescriptor) ProjectKey() string         { return v.projectKey }
func (v ProjectDescriptor) Title() string              { return v.title }
func (v ProjectDescriptor) Description() *string       { return cloneString(v.description) }
func (v ProjectDescriptor) DefaultLocale() Locale      { return v.defaultLocale }
func (v ProjectDescriptor) Timezone() string           { return v.timezone }
func (v ProjectDescriptor) CatalogState() CatalogState { return v.catalogState }
func (v ProjectDescriptor) Version() int               { return v.version }
func (v ProjectDescriptor) Schema() string             { return ProjectSchemaVersion }
func (v ProjectDescriptor) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"schema": ProjectSchemaVersion, "project_id": v.projectID, "project_key": v.projectKey, "title": v.title, "description": v.description, "default_locale": v.defaultLocale, "timezone": v.timezone, "catalog_state": v.catalogState, "version": v.version})
}
func (v *ProjectDescriptor) UnmarshalJSON(data []byte) error {
	var dto struct {
		Schema        string       `json:"schema"`
		ProjectID     string       `json:"project_id"`
		ProjectKey    string       `json:"project_key"`
		Title         string       `json:"title"`
		Description   *string      `json:"description"`
		DefaultLocale Locale       `json:"default_locale"`
		Timezone      string       `json:"timezone"`
		CatalogState  CatalogState `json:"catalog_state"`
		Version       int          `json:"version"`
	}
	if err := decodeStrict(data, &dto); err != nil {
		return err
	}
	if dto.Schema != ProjectSchemaVersion {
		return fieldError("schema", "has an unsupported value")
	}
	value, err := NewProjectDescriptor(dto.ProjectID, dto.ProjectKey, dto.Title, dto.Description, dto.DefaultLocale, dto.Timezone, dto.CatalogState, dto.Version)
	if err == nil {
		*v = value
	}
	return err
}

type WorkstreamDescriptor struct {
	scopeID, projectID string
	key                *string
	title              string
	kind               WorkstreamKind
	catalogState       CatalogState
	externalRefs       []ExternalReference
	labels             []string
	version            int
}

func NewWorkstreamDescriptor(scopeID, projectID string, key *string, title string, kind WorkstreamKind, state CatalogState, refs []ExternalReference, labels []string, version int) (WorkstreamDescriptor, error) {
	externalRefs := make([]ExternalReference, len(refs))
	copy(externalRefs, refs)
	clonedLabels := make([]string, len(labels))
	copy(clonedLabels, labels)
	value := WorkstreamDescriptor{
		scopeID: scopeID, projectID: projectID, key: cloneString(key), title: title,
		kind: kind, catalogState: state, externalRefs: externalRefs, labels: clonedLabels, version: version,
	}
	if err := value.Validate(); err != nil {
		return WorkstreamDescriptor{}, err
	}
	return value, nil
}
func (v WorkstreamDescriptor) Validate() error {
	if err := requireText("scope_id", v.scopeID, MaxScopeIDLength); err != nil {
		return err
	}
	if err := requireText("project_id", v.projectID, MaxReportIDLength); err != nil {
		return err
	}
	if err := requireOptionalText("key", v.key, MaxWorkstreamKeyLength); err != nil {
		return err
	}
	if err := requireText("title", v.title, MaxReportTitleLength); err != nil {
		return err
	}
	if !validWorkstreamKind(v.kind) {
		return fieldError("kind", "has an unsupported value")
	}
	if v.catalogState != CatalogIncluded && v.catalogState != CatalogArchived {
		return fieldError("catalog_state", "has an unsupported value")
	}
	if v.version < 1 {
		return fieldError("version", "must be a positive integer")
	}
	if len(v.externalRefs) > MaxReportExternalRefs {
		return fieldError("external_refs", fmt.Sprintf("must not exceed %d items", MaxReportExternalRefs))
	}
	if len(v.labels) > MaxReportLabels {
		return fieldError("labels", fmt.Sprintf("must not exceed %d items", MaxReportLabels))
	}
	seenRefs := map[string]struct{}{}
	for _, ref := range v.externalRefs {
		if err := ref.Validate(); err != nil {
			return err
		}
		if _, ok := seenRefs[ref.key()]; ok {
			return fieldError("external_refs", "must be unique")
		}
		seenRefs[ref.key()] = struct{}{}
	}
	seenLabels := map[string]struct{}{}
	for _, label := range v.labels {
		if err := requireText("label", label, MaxReportLabelLength); err != nil {
			return err
		}
		if _, ok := seenLabels[label]; ok {
			return fieldError("labels", "must be unique")
		}
		seenLabels[label] = struct{}{}
	}
	return nil
}
func (v WorkstreamDescriptor) ScopeID() string                   { return v.scopeID }
func (v WorkstreamDescriptor) ProjectID() string                 { return v.projectID }
func (v WorkstreamDescriptor) Key() *string                      { return cloneString(v.key) }
func (v WorkstreamDescriptor) Title() string                     { return v.title }
func (v WorkstreamDescriptor) Kind() WorkstreamKind              { return v.kind }
func (v WorkstreamDescriptor) CatalogState() CatalogState        { return v.catalogState }
func (v WorkstreamDescriptor) ExternalRefs() []ExternalReference { return slices.Clone(v.externalRefs) }
func (v WorkstreamDescriptor) Labels() []string                  { return slices.Clone(v.labels) }
func (v WorkstreamDescriptor) Version() int                      { return v.version }
func (v WorkstreamDescriptor) Schema() string                    { return WorkstreamSchemaVersion }
func (v WorkstreamDescriptor) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"schema": WorkstreamSchemaVersion, "scope_id": v.scopeID, "project_id": v.projectID, "key": v.key, "title": v.title, "kind": v.kind, "catalog_state": v.catalogState, "external_refs": v.externalRefs, "labels": v.labels, "version": v.version})
}
func (v *WorkstreamDescriptor) UnmarshalJSON(data []byte) error {
	var dto struct {
		Schema       string              `json:"schema"`
		ScopeID      string              `json:"scope_id"`
		ProjectID    string              `json:"project_id"`
		Key          *string             `json:"key"`
		Title        string              `json:"title"`
		Kind         WorkstreamKind      `json:"kind"`
		CatalogState CatalogState        `json:"catalog_state"`
		ExternalRefs []ExternalReference `json:"external_refs"`
		Labels       []string            `json:"labels"`
		Version      int                 `json:"version"`
	}
	if err := decodeStrict(data, &dto); err != nil {
		return err
	}
	if dto.Schema != WorkstreamSchemaVersion {
		return fieldError("schema", "has an unsupported value")
	}
	value, err := NewWorkstreamDescriptor(dto.ScopeID, dto.ProjectID, dto.Key, dto.Title, dto.Kind, dto.CatalogState, dto.ExternalRefs, dto.Labels, dto.Version)
	if err == nil {
		*v = value
	}
	return err
}

type WorkspaceBinding struct {
	workspaceInstanceID, projectID string
	repositoryRef                  RepositoryRef
	state                          WorkspaceBindingState
	confirmedAt                    time.Time
	version                        int
}

func NewWorkspaceBinding(workspaceID, projectID string, repository RepositoryRef, state WorkspaceBindingState, confirmedAt time.Time, version int) (WorkspaceBinding, error) {
	if err := requireText("workspace_instance_id", workspaceID, MaxWorkspaceInstanceIDLength); err != nil {
		return WorkspaceBinding{}, err
	}
	if err := requireText("project_id", projectID, MaxReportIDLength); err != nil {
		return WorkspaceBinding{}, err
	}
	if state != WorkspaceConfirmed && state != WorkspaceDetached {
		return WorkspaceBinding{}, fieldError("state", "has an unsupported value")
	}
	if confirmedAt.Location() != time.UTC {
		return WorkspaceBinding{}, fieldError("confirmed_at", "must be UTC")
	}
	if version < 1 {
		return WorkspaceBinding{}, fieldError("version", "must be positive")
	}
	return WorkspaceBinding{workspaceID, projectID, repository, state, confirmedAt, version}, nil
}
func (v WorkspaceBinding) WorkspaceInstanceID() string  { return v.workspaceInstanceID }
func (v WorkspaceBinding) ProjectID() string            { return v.projectID }
func (v WorkspaceBinding) RepositoryRef() RepositoryRef { return v.repositoryRef }
func (v WorkspaceBinding) State() WorkspaceBindingState { return v.state }
func (v WorkspaceBinding) ConfirmedAt() time.Time       { return v.confirmedAt }
func (v WorkspaceBinding) Version() int                 { return v.version }
func (v WorkspaceBinding) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"schema": "powercontext.workspace-binding.v1", "workspace_instance_id": v.workspaceInstanceID, "project_id": v.projectID, "repository_ref": v.repositoryRef, "state": v.state, "confirmed_at": JSONTimestampText(v.confirmedAt), "version": v.version})
}
func (v *WorkspaceBinding) UnmarshalJSON(data []byte) error {
	var dto struct {
		Schema      string                `json:"schema"`
		WorkspaceID string                `json:"workspace_instance_id"`
		ProjectID   string                `json:"project_id"`
		Repository  RepositoryRef         `json:"repository_ref"`
		State       WorkspaceBindingState `json:"state"`
		ConfirmedAt string                `json:"confirmed_at"`
		Version     int                   `json:"version"`
	}
	if err := decodeStrict(data, &dto); err != nil {
		return err
	}
	if dto.Schema != "powercontext.workspace-binding.v1" {
		return fieldError("schema", "has an unsupported value")
	}
	at, err := time.Parse(time.RFC3339Nano, dto.ConfirmedAt)
	if err != nil {
		return err
	}
	value, err := NewWorkspaceBinding(dto.WorkspaceID, dto.ProjectID, dto.Repository, dto.State, at.UTC(), dto.Version)
	if err == nil {
		*v = value
	}
	return err
}

type SelectionEntry struct {
	scopeID            string
	workstreamRevision int
	status             SelectionStatus
	handoffRef         *artifact.Ref
}

func NewSelectionEntry(scopeID string, revision int, status SelectionStatus, ref *artifact.Ref) (SelectionEntry, error) {
	value := SelectionEntry{scopeID, revision, status, cloneArtifactRef(ref)}
	if err := value.Validate(); err != nil {
		return SelectionEntry{}, err
	}
	return value, nil
}
func (v SelectionEntry) Validate() error {
	if err := requireText("scope_id", v.scopeID, MaxScopeIDLength); err != nil {
		return err
	}
	if v.workstreamRevision < 1 {
		return fieldError("workstream_revision", "must be positive")
	}
	if v.status == SelectionSelected {
		if v.handoffRef == nil || v.handoffRef.Validate() != nil || v.handoffRef.Family() != "handoff" {
			return fieldError("handoff_ref", "must be an exact Handoff reference")
		}
	} else if v.status == SelectionNoHandoff {
		if v.handoffRef != nil {
			return fieldError("handoff_ref", "must be null for no_handoff")
		}
	} else {
		return fieldError("status", "has an unsupported value")
	}
	return nil
}
func (v SelectionEntry) ScopeID() string           { return v.scopeID }
func (v SelectionEntry) WorkstreamRevision() int   { return v.workstreamRevision }
func (v SelectionEntry) Status() SelectionStatus   { return v.status }
func (v SelectionEntry) HandoffRef() *artifact.Ref { return cloneArtifactRef(v.handoffRef) }
func (v SelectionEntry) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"scope_id": v.scopeID, "workstream_revision": v.workstreamRevision, "status": v.status, "handoff_ref": artifactRefMap(v.handoffRef)})
}

type Page[T any] struct {
	Items      []T
	NextCursor *string
}
type StoredActivity struct {
	Event  ActivityEvent
	Cursor int64
}
type ActivityPage struct {
	Items         []ActivityEvent
	NextCursor    *int64
	HighWatermark int64
}

func NormalizeSortText(value string) string { return cases.Fold().String(norm.NFC.String(value)) }
func UTCText(value time.Time) string        { return value.UTC().Format("2006-01-02T15:04:05.000000Z") }
func TimestampText(value time.Time) string  { return value.Format("2006-01-02T15:04:05.000000Z07:00") }
func JSONTimestampText(value time.Time) string {
	value = value.Truncate(time.Microsecond)
	layout := "2006-01-02T15:04:05"
	if value.Nanosecond() != 0 {
		layout += ".000000"
	}
	_, offset := value.Zone()
	if offset == 0 {
		return value.Format(layout) + "Z"
	}
	return value.Format(layout + "Z07:00")
}

func CompareWorkstreams(left, right WorkstreamDescriptor) int {
	leftTitle, rightTitle := NormalizeSortText(left.Title()), NormalizeSortText(right.Title())
	if leftTitle < rightTitle {
		return -1
	}
	if leftTitle > rightTitle {
		return 1
	}
	if left.ScopeID() < right.ScopeID() {
		return -1
	}
	if left.ScopeID() > right.ScopeID() {
		return 1
	}
	return 0
}

func CompareActivities(left, right ActivityEvent) int {
	leftTime, rightTime := left.EffectivePeriodTime(), right.EffectivePeriodTime()
	if leftTime == nil && rightTime != nil {
		return 1
	}
	if leftTime != nil && rightTime == nil {
		return -1
	}
	if leftTime != nil && !leftTime.Equal(*rightTime) {
		if leftTime.Before(*rightTime) {
			return -1
		}
		return 1
	}
	if !left.ObservedAt().Equal(right.ObservedAt()) {
		if left.ObservedAt().Before(right.ObservedAt()) {
			return -1
		}
		return 1
	}
	if left.EventID() < right.EventID() {
		return -1
	}
	if left.EventID() > right.EventID() {
		return 1
	}
	return 0
}

func CompareSelections(left, right SelectionEntry) int {
	if left.ScopeID() < right.ScopeID() {
		return -1
	}
	if left.ScopeID() > right.ScopeID() {
		return 1
	}
	return 0
}

func normalizeRepositoryRemote(value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	if !strings.Contains(*value, "://") {
		v := strings.TrimRight(*value, "/")
		return &v, nil
	}
	parsed, err := url.Parse(*value)
	if err != nil {
		return nil, fieldError("normalized_remote", "must contain a valid URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fieldError("normalized_remote", "must not contain credentials or query fragments")
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return nil, fieldError("normalized_remote", "must contain a host")
	}
	if parsed.Port() != "" {
		host += ":" + parsed.Port()
	}
	parts := make([]string, 0)
	for _, part := range strings.Split(parsed.Path, "/") {
		if part != "" && part != "." {
			parts = append(parts, part)
		}
	}
	result := strings.ToLower(parsed.Scheme) + "://" + host + "/" + strings.Join(parts, "/")
	return &result, nil
}
func normalizeRepositorySubpath(value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	candidate := strings.ReplaceAll(*value, "\\", "/")
	for _, part := range strings.Split(candidate, "/") {
		if part == ".." {
			return nil, fieldError("subpath", "must not contain parent traversal")
		}
	}
	normalized := path.Clean(candidate)
	if normalized == "" || normalized == "." {
		normalized = "."
	}
	if strings.HasPrefix(normalized, "/") {
		return nil, fieldError("subpath", "must be relative")
	}
	return &normalized, nil
}

func requireText(field, value string, max int) error {
	if strings.TrimSpace(value) == "" {
		return fieldError(field, "must contain non-whitespace text")
	}
	if strings.TrimSpace(value) != value {
		return fieldError(field, "must not contain leading or trailing whitespace")
	}
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > max {
		return fieldError(field, fmt.Sprintf("must not exceed %d characters", max))
	}
	return nil
}
func requireOptionalText(field string, value *string, max int) error {
	if value == nil {
		return nil
	}
	return requireText(field, *value, max)
}
func fieldError(field, detail string) error {
	return &CatalogArgumentError{Field: field, Detail: detail}
}
func invalidActivity(field, detail string) error {
	return &InvalidActivityEventError{Field: field, Detail: detail}
}
func activityField(err error) error {
	if e, ok := err.(*CatalogArgumentError); ok {
		return invalidActivity(e.Field, e.Detail)
	}
	return err
}
func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
func cloneExternal(value *ExternalReference) *ExternalReference {
	if value == nil {
		return nil
	}
	copy := *value
	copy.url = cloneString(value.url)
	return &copy
}
func cloneAgent(value *ActivityAgent) *ActivityAgent {
	if value == nil {
		return nil
	}
	copy := *value
	copy.provider = cloneString(value.provider)
	copy.label = cloneString(value.label)
	return &copy
}
func cloneVCS(value *ActivityVCSContext) *ActivityVCSContext {
	if value == nil {
		return nil
	}
	copy := *value
	copy.branch = cloneString(value.branch)
	copy.headRevision = cloneString(value.headRevision)
	return &copy
}
func cloneArtifactRef(value *artifact.Ref) *artifact.Ref {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
func optionalKey(value *string) string {
	if value == nil {
		return "\x00"
	}
	return *value
}
func timeTextPtr(value *time.Time) any {
	if value == nil {
		return nil
	}
	return TimestampText(*value)
}
func artifactRefMap(value *artifact.Ref) any {
	if value == nil {
		return nil
	}
	return map[string]any{"family": value.Family(), "artifact_id": value.ID(), "revision": value.Revision()}
}
func decodeStrict(data []byte, value any) error {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	return decoder.Decode(value)
}
func validExternalKind(v ExternalReferenceKind) bool {
	return slices.Contains([]ExternalReferenceKind{ExternalIssue, ExternalTask, ExternalPullRequest, ExternalBranch, ExternalFeature, ExternalRelease, ExternalProgram, ExternalOther}, v)
}
func validRepositoryProvider(v RepositoryProvider) bool {
	return slices.Contains([]RepositoryProvider{RepositoryGitHub, RepositoryGitLab, RepositoryLocal, RepositoryOther}, v)
}
func validWorkstreamKind(v WorkstreamKind) bool {
	return slices.Contains([]WorkstreamKind{WorkstreamFeature, WorkstreamBug, WorkstreamRefactor, WorkstreamOperations, WorkstreamResearch, WorkstreamOther}, v)
}
func validActivitySource(v ActivitySource) bool {
	return slices.Contains([]ActivitySource{ActivityHandoffObservation, ActivityGitCommit, ActivityGitWorktree, ActivityCodingSession, ActivityOther}, v)
}
func validTimeBasis(v TimeBasis) bool {
	return slices.Contains([]TimeBasis{TimeSourceReported, TimeHostObserved, TimeFirstSeen, TimeCurrentOnly, TimeUnknown}, v)
}
