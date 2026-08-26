package endpoint

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/go-faster/jx"
	v1 "github.com/ob-labs/powercontext-go/api/v1"
	"github.com/ob-labs/powercontext-go/handoffreport"
	"github.com/ob-labs/powercontext-go/runtime"
)

// HandoffReportOperations is deliberately shaped around the optional product
// surface. It keeps the generated HTTP contract out of Runtime and allows the
// same operations to be called directly by MCP without loopback HTTP.
type HandoffReportOperations interface {
	CreateProject(context.Context, runtime.CreateHandoffReportProject) (handoffreport.ProjectDescriptor, error)
	GetProject(context.Context, string) (handoffreport.ProjectDescriptor, error)
	UpdateProject(context.Context, handoffreport.ProjectDescriptor, int) (handoffreport.ProjectDescriptor, error)
	ListProjects(context.Context, *string, int, bool) (handoffreport.Page[handoffreport.ProjectDescriptor], error)
	RegisterWorkstream(context.Context, runtime.RegisterHandoffReportWorkstream) (handoffreport.WorkstreamDescriptor, error)
	UpdateWorkstream(context.Context, handoffreport.WorkstreamDescriptor, int) (handoffreport.WorkstreamDescriptor, error)
	ListWorkstreams(context.Context, string, *string, int, bool) (handoffreport.Page[handoffreport.WorkstreamDescriptor], error)
	RecordActivity(context.Context, runtime.RecordHandoffReportActivity) (handoffreport.StoredActivity, error)
	ListActivities(context.Context, runtime.HandoffReportActivityList) (handoffreport.ActivityPage, error)
	PurgeActivities(context.Context, string, time.Time) (int64, error)
	GetWorkspaceBinding(context.Context, string) (handoffreport.WorkspaceBinding, error)
	AttachWorkspaceBinding(context.Context, string, string, handoffreport.RepositoryRef, *int) (handoffreport.WorkspaceBinding, error)
	DetachWorkspaceBinding(context.Context, string, int) (handoffreport.WorkspaceBinding, error)
	GetReport(context.Context, runtime.GetHandoffReport) (handoffreport.Report, error)
}

func (h *Handler) CreateHandoffReportProject(ctx context.Context, req *v1.CreateHandoffReportProjectRequest) (v1.CreateHandoffReportProjectRes, error) {
	if h.handoffReport == nil {
		return nil, &RuntimeNotReadyError{}
	}
	value, err := h.handoffReport.CreateProject(ctx, runtime.CreateHandoffReportProject{
		ProjectKey:    req.ProjectKey,
		Title:         req.Title,
		Description:   optionalString(req.Description),
		DefaultLocale: handoffreport.Locale(req.DefaultLocale.Or(v1.ReportLocaleZhCN)),
		Timezone:      req.Timezone.Or("UTC"),
	})
	if err != nil {
		return nil, err
	}
	return projectResponse(ctx, value), nil
}

func (h *Handler) GetHandoffReportProject(ctx context.Context, req *v1.GetHandoffReportProjectRequest) (v1.GetHandoffReportProjectRes, error) {
	if h.handoffReport == nil {
		return nil, &RuntimeNotReadyError{}
	}
	value, err := h.handoffReport.GetProject(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	return projectResponse(ctx, value), nil
}

func (h *Handler) UpdateHandoffReportProject(ctx context.Context, req *v1.UpdateHandoffReportProjectRequest) (v1.UpdateHandoffReportProjectRes, error) {
	if h.handoffReport == nil {
		return nil, &RuntimeNotReadyError{}
	}
	value, err := runtimeReportProject(req.Project)
	if err != nil {
		return nil, err
	}
	value, err = h.handoffReport.UpdateProject(ctx, value, req.ExpectedVersion)
	if err != nil {
		return nil, err
	}
	return projectResponse(ctx, value), nil
}

func (h *Handler) ListHandoffReportProjects(ctx context.Context, req *v1.ListHandoffReportProjectsRequest) (v1.ListHandoffReportProjectsRes, error) {
	if h.handoffReport == nil {
		return nil, &RuntimeNotReadyError{}
	}
	page, err := h.handoffReport.ListProjects(ctx, optionalString(req.Cursor), req.Limit.Or(handoffreport.DefaultCatalogPageSize), req.IncludeArchived.Or(false))
	if err != nil {
		return nil, err
	}
	items := make([]v1.ProjectDescriptor, len(page.Items))
	for i, item := range page.Items {
		items[i] = wireReportProject(item)
	}
	return &v1.ProjectPageHeaders{
		XPowerContextRequestID: requestID(ctx),
		Response:               v1.ProjectPage{Items: items, NextCursor: nullableString(page.NextCursor)},
	}, nil
}

func (h *Handler) RegisterHandoffReportWorkstream(ctx context.Context, req *v1.RegisterHandoffReportWorkstreamRequest) (v1.RegisterHandoffReportWorkstreamRes, error) {
	if h.handoffReport == nil {
		return nil, &RuntimeNotReadyError{}
	}
	refs, err := runtimeReportExternalReferences(req.ExternalRefs)
	if err != nil {
		return nil, err
	}
	value, err := h.handoffReport.RegisterWorkstream(ctx, runtime.RegisterHandoffReportWorkstream{
		ProjectID:    req.ProjectID,
		ScopeID:      req.ScopeID,
		Key:          optionalString(req.Key),
		Title:        req.Title,
		Kind:         handoffreport.WorkstreamKind(req.Kind),
		CatalogState: handoffreport.CatalogState(req.CatalogState.Or(v1.ReportCatalogStateIncluded)),
		ExternalRefs: refs,
		Labels:       req.Labels,
	})
	if err != nil {
		return nil, err
	}
	return workstreamResponse(ctx, value), nil
}

func (h *Handler) UpdateHandoffReportWorkstream(ctx context.Context, req *v1.UpdateHandoffReportWorkstreamRequest) (v1.UpdateHandoffReportWorkstreamRes, error) {
	if h.handoffReport == nil {
		return nil, &RuntimeNotReadyError{}
	}
	value, err := runtimeReportWorkstream(req.Workstream)
	if err != nil {
		return nil, err
	}
	value, err = h.handoffReport.UpdateWorkstream(ctx, value, req.ExpectedVersion)
	if err != nil {
		return nil, err
	}
	return workstreamResponse(ctx, value), nil
}

func (h *Handler) ListHandoffReportWorkstreams(ctx context.Context, req *v1.ListHandoffReportWorkstreamsRequest) (v1.ListHandoffReportWorkstreamsRes, error) {
	if h.handoffReport == nil {
		return nil, &RuntimeNotReadyError{}
	}
	page, err := h.handoffReport.ListWorkstreams(ctx, req.ProjectID, optionalString(req.Cursor), req.Limit.Or(handoffreport.DefaultCatalogPageSize), req.IncludeArchived.Or(false))
	if err != nil {
		return nil, err
	}
	items := make([]v1.WorkstreamDescriptor, len(page.Items))
	for i, item := range page.Items {
		items[i] = wireReportWorkstream(item)
	}
	return &v1.WorkstreamPageHeaders{
		XPowerContextRequestID: requestID(ctx),
		Response:               v1.WorkstreamPage{Items: items, NextCursor: nullableString(page.NextCursor)},
	}, nil
}

func (h *Handler) RecordHandoffReportActivity(ctx context.Context, req *v1.RecordHandoffReportActivityRequest) (v1.RecordHandoffReportActivityRes, error) {
	if h.handoffReport == nil {
		return nil, &RuntimeNotReadyError{}
	}
	sourceRef, err := runtimeOptionalReportExternalReference(req.SourceRef)
	if err != nil {
		return nil, err
	}
	refs, err := runtimeReportExternalReferences(req.EvidenceRefs)
	if err != nil {
		return nil, err
	}
	agent, err := runtimeReportActivityAgent(req.Agent)
	if err != nil {
		return nil, err
	}
	vcs, err := runtimeReportActivityVCS(req.VcsContext)
	if err != nil {
		return nil, err
	}
	stored, err := h.handoffReport.RecordActivity(ctx, runtime.RecordHandoffReportActivity{
		ProjectID:     req.ProjectID,
		ScopeID:       optionalString(req.ScopeID),
		Source:        handoffreport.ActivitySource(req.Source),
		SourceEventID: req.SourceEventID,
		SourceRef:     sourceRef,
		OccurredAt:    optionalTime(req.OccurredAt),
		TimeBasis:     handoffreport.TimeBasis(req.TimeBasis),
		Title:         optionalString(req.Title),
		Summary:       optionalString(req.Summary),
		Agent:         agent,
		SessionID:     optionalString(req.SessionID),
		VCSContext:    vcs,
		EvidenceRefs:  refs,
	})
	if err != nil {
		return nil, err
	}
	return &v1.StoredHandoffReportActivityHeaders{
		XPowerContextRequestID: requestID(ctx),
		Response:               v1.StoredHandoffReportActivity{Cursor: int(stored.Cursor), Event: wireReportActivity(stored.Event)},
	}, nil
}

func (h *Handler) ListHandoffReportActivities(ctx context.Context, req *v1.ListHandoffReportActivitiesRequest) (v1.ListHandoffReportActivitiesRes, error) {
	if h.handoffReport == nil {
		return nil, &RuntimeNotReadyError{}
	}
	sources := []handoffreport.ActivitySource(nil)
	if values, ok := req.Sources.Get(); ok {
		sources = make([]handoffreport.ActivitySource, len(values))
		for i, value := range values {
			sources[i] = handoffreport.ActivitySource(value)
		}
	}
	page, err := h.handoffReport.ListActivities(ctx, runtime.HandoffReportActivityList{
		ProjectID:     req.ProjectID,
		PeriodStart:   optionalTime(req.PeriodStart),
		PeriodEnd:     optionalTime(req.PeriodEnd),
		Sources:       sources,
		AfterCursor:   int64(req.AfterCursor.Or(0)),
		ThroughCursor: optionalInt64(req.ThroughCursor),
		Limit:         req.Limit.Or(handoffreport.DefaultCatalogPageSize),
	})
	if err != nil {
		return nil, err
	}
	items := make([]v1.HandoffReportActivity, len(page.Items))
	for i, item := range page.Items {
		items[i] = wireReportActivity(item)
	}
	return &v1.HandoffReportActivityPageHeaders{
		XPowerContextRequestID: requestID(ctx),
		Response: v1.HandoffReportActivityPage{
			Items:         items,
			NextCursor:    nullableInt64(page.NextCursor),
			HighWatermark: int(page.HighWatermark),
		},
	}, nil
}

func (h *Handler) PurgeHandoffReportActivities(ctx context.Context, req *v1.PurgeHandoffReportActivitiesRequest) (v1.PurgeHandoffReportActivitiesRes, error) {
	if h.handoffReport == nil {
		return nil, &RuntimeNotReadyError{}
	}
	deleted, err := h.handoffReport.PurgeActivities(ctx, req.ProjectID, req.ObservedBefore)
	if err != nil {
		return nil, err
	}
	return &v1.PurgeHandoffReportActivitiesResponseHeaders{
		XPowerContextRequestID: requestID(ctx),
		Response:               v1.PurgeHandoffReportActivitiesResponse{DeletedCount: int(deleted)},
	}, nil
}

func (h *Handler) GetHandoffReportWorkspace(ctx context.Context, req *v1.GetHandoffReportWorkspaceRequest) (v1.GetHandoffReportWorkspaceRes, error) {
	if h.handoffReport == nil {
		return nil, &RuntimeNotReadyError{}
	}
	value, err := h.handoffReport.GetWorkspaceBinding(ctx, req.WorkspaceInstanceID)
	if err != nil {
		return nil, err
	}
	return workspaceResponse(ctx, value), nil
}

func (h *Handler) AttachHandoffReportWorkspace(ctx context.Context, req *v1.AttachHandoffReportWorkspaceRequest) (v1.AttachHandoffReportWorkspaceRes, error) {
	if h.handoffReport == nil {
		return nil, &RuntimeNotReadyError{}
	}
	repository, err := runtimeReportRepository(req.RepositoryRef)
	if err != nil {
		return nil, err
	}
	value, err := h.handoffReport.AttachWorkspaceBinding(ctx, req.WorkspaceInstanceID, req.ProjectID, repository, nullableInt(req.ExpectedVersion))
	if err != nil {
		return nil, err
	}
	return workspaceResponse(ctx, value), nil
}

func (h *Handler) DetachHandoffReportWorkspace(ctx context.Context, req *v1.DetachHandoffReportWorkspaceRequest) (v1.DetachHandoffReportWorkspaceRes, error) {
	if h.handoffReport == nil {
		return nil, &RuntimeNotReadyError{}
	}
	value, err := h.handoffReport.DetachWorkspaceBinding(ctx, req.WorkspaceInstanceID, req.ExpectedVersion)
	if err != nil {
		return nil, err
	}
	return workspaceResponse(ctx, value), nil
}

func (h *Handler) GetHandoffReport(ctx context.Context, req *v1.GetHandoffReportRequest) (v1.GetHandoffReportRes, error) {
	if h.handoffReport == nil {
		return nil, &RuntimeNotReadyError{}
	}
	var locale *handoffreport.Locale
	if value, ok := req.Locale.Get(); ok {
		converted := handoffreport.Locale(value)
		locale = &converted
	}
	var period *runtime.HandoffReportPeriod
	if value, ok := req.Period.Get(); ok {
		period = &runtime.HandoffReportPeriod{
			Start:                   value.Start,
			End:                     value.End,
			Timezone:                optionalString(value.Timezone),
			CompareToPreviousPeriod: value.CompareToPreviousPeriod.Or(false),
		}
	}
	format := handoffreport.Format(req.Format.Or(v1.ReportFormatMarkdown))
	report, err := h.handoffReport.GetReport(ctx, runtime.GetHandoffReport{
		ProjectID:             req.ProjectID,
		Locale:                locale,
		IncludeEvidenceChecks: req.IncludeEvidenceChecks.Or(true),
		Format:                format,
		IncludeArchived:       req.IncludeArchived.Or(false),
		Period:                period,
	})
	if err != nil {
		return nil, err
	}

	selectionDigest, reportDigest := report.SelectionDigest(), report.ReportDigest()
	contentDisposition := v1.OptString{}
	if req.Download.Or(false) {
		extension := "json"
		if format == handoffreport.FormatMarkdown {
			extension = "md"
		}
		contentDisposition = v1.NewOptString(`attachment; filename="handoff-report.` + extension + `"`)
	}
	if format == handoffreport.FormatMarkdown {
		markdown, renderErr := handoffreport.RenderMarkdown(report)
		if renderErr != nil {
			return nil, renderErr
		}
		if err := enforceHandoffReportSize(report, len([]byte(markdown))); err != nil {
			return nil, err
		}
		return &v1.GetHandoffReportOKTextMarkdownHeaders{
			CacheControl:                 v1.NewOptGetHandoffReportOKCacheControl(v1.GetHandoffReportOKCacheControlNoStore),
			ContentDisposition:           contentDisposition,
			XPowerContextReportDigest:    v1.NewOptString(reportDigest),
			XPowerContextRequestID:       requestID(ctx),
			XPowerContextSelectionDigest: v1.NewOptString(selectionDigest),
			Response:                     v1.GetHandoffReportOKTextMarkdown{Data: strings.NewReader(markdown)},
		}, nil
	}

	wireReport, err := wireHandoffReportObject(report)
	if err != nil {
		return nil, err
	}
	response := v1.HandoffReportResponse{
		Format:          v1.ReportFormatJSON,
		Report:          v1.NewNilHandoffReportResponseReport(wireReport),
		SelectionDigest: selectionDigest,
		ReportDigest:    reportDigest,
	}
	response.Markdown.SetToNull()
	encoder := new(jx.Encoder)
	response.Encode(encoder)
	if err := enforceHandoffReportSize(report, len(encoder.Bytes())); err != nil {
		return nil, err
	}
	return &v1.HandoffReportResponseHeaders{
		CacheControl:                 v1.NewOptGetHandoffReportOKCacheControl(v1.GetHandoffReportOKCacheControlNoStore),
		ContentDisposition:           contentDisposition,
		XPowerContextReportDigest:    v1.NewOptString(reportDigest),
		XPowerContextRequestID:       requestID(ctx),
		XPowerContextSelectionDigest: v1.NewOptString(selectionDigest),
		Response:                     response,
	}, nil
}

func projectResponse(ctx context.Context, value handoffreport.ProjectDescriptor) *v1.ProjectDescriptorHeaders {
	return &v1.ProjectDescriptorHeaders{XPowerContextRequestID: requestID(ctx), Response: wireReportProject(value)}
}

func workstreamResponse(ctx context.Context, value handoffreport.WorkstreamDescriptor) *v1.WorkstreamDescriptorHeaders {
	return &v1.WorkstreamDescriptorHeaders{XPowerContextRequestID: requestID(ctx), Response: wireReportWorkstream(value)}
}

func workspaceResponse(ctx context.Context, value handoffreport.WorkspaceBinding) *v1.HandoffReportWorkspaceBindingHeaders {
	return &v1.HandoffReportWorkspaceBindingHeaders{XPowerContextRequestID: requestID(ctx), Response: wireReportWorkspace(value)}
}

func runtimeReportProject(value v1.ProjectDescriptor) (handoffreport.ProjectDescriptor, error) {
	if value.Schema != v1.ProjectDescriptorSchemaPowercontextProjectV1 {
		return handoffreport.ProjectDescriptor{}, &handoffreport.CatalogArgumentError{Field: "project.schema", Detail: "has an unsupported value"}
	}
	return handoffreport.NewProjectDescriptor(value.ProjectID, value.ProjectKey, value.Title, nullableWireString(value.Description), handoffreport.Locale(value.DefaultLocale), value.Timezone, handoffreport.CatalogState(value.CatalogState), value.Version)
}

func runtimeReportWorkstream(value v1.WorkstreamDescriptor) (handoffreport.WorkstreamDescriptor, error) {
	if value.Schema != v1.WorkstreamDescriptorSchemaPowercontextWorkstreamV1 {
		return handoffreport.WorkstreamDescriptor{}, &handoffreport.CatalogArgumentError{Field: "workstream.schema", Detail: "has an unsupported value"}
	}
	refs, err := runtimeReportExternalReferences(value.ExternalRefs)
	if err != nil {
		return handoffreport.WorkstreamDescriptor{}, err
	}
	return handoffreport.NewWorkstreamDescriptor(value.ScopeID, value.ProjectID, nullableWireString(value.Key), value.Title, handoffreport.WorkstreamKind(value.Kind), handoffreport.CatalogState(value.CatalogState), refs, value.Labels, value.Version)
}

func runtimeReportExternalReferences(values []v1.HandoffReportExternalReference) ([]handoffreport.ExternalReference, error) {
	result := make([]handoffreport.ExternalReference, len(values))
	for i, value := range values {
		converted, err := runtimeReportExternalReference(value)
		if err != nil {
			return nil, err
		}
		result[i] = converted
	}
	return result, nil
}

func runtimeReportExternalReference(value v1.HandoffReportExternalReference) (handoffreport.ExternalReference, error) {
	return handoffreport.NewExternalReference(handoffreport.ExternalReferenceKind(value.Kind), value.Provider, value.ExternalID, nullableWireString(value.URL))
}

func runtimeOptionalReportExternalReference(value v1.OptNilHandoffReportExternalReference) (*handoffreport.ExternalReference, error) {
	wire, ok := value.Get()
	if !ok {
		return nil, nil
	}
	converted, err := runtimeReportExternalReference(wire)
	if err != nil {
		return nil, err
	}
	return &converted, nil
}

func runtimeReportRepository(value v1.HandoffReportRepositoryRef) (handoffreport.RepositoryRef, error) {
	return handoffreport.NewRepositoryRef(
		handoffreport.RepositoryProvider(value.Provider),
		nullableWireString(value.RepositoryID),
		nullableWireString(value.NormalizedRemote),
		nullableWireString(value.Subpath),
	)
}

func runtimeReportActivityAgent(value v1.OptNilHandoffReportActivityAgent) (*handoffreport.ActivityAgent, error) {
	wire, ok := value.Get()
	if !ok {
		return nil, nil
	}
	converted, err := handoffreport.NewActivityAgent(optionalString(wire.Provider), optionalString(wire.Label))
	if err != nil {
		return nil, err
	}
	return &converted, nil
}

func runtimeReportActivityVCS(value v1.OptNilHandoffReportActivityVcsContext) (*handoffreport.ActivityVCSContext, error) {
	wire, ok := value.Get()
	if !ok {
		return nil, nil
	}
	converted, err := handoffreport.NewActivityVCSContext(optionalString(wire.Branch), optionalString(wire.HeadRevision))
	if err != nil {
		return nil, err
	}
	return &converted, nil
}

func wireReportProject(value handoffreport.ProjectDescriptor) v1.ProjectDescriptor {
	return v1.ProjectDescriptor{
		Schema:        v1.ProjectDescriptorSchemaPowercontextProjectV1,
		ProjectID:     value.ProjectID(),
		ProjectKey:    value.ProjectKey(),
		Title:         value.Title(),
		Description:   nullableString(value.Description()),
		DefaultLocale: v1.ReportLocale(value.DefaultLocale()),
		Timezone:      value.Timezone(),
		CatalogState:  v1.ReportCatalogState(value.CatalogState()),
		Version:       value.Version(),
	}
}

func wireReportWorkstream(value handoffreport.WorkstreamDescriptor) v1.WorkstreamDescriptor {
	refs := value.ExternalRefs()
	wireRefs := make([]v1.HandoffReportExternalReference, len(refs))
	for i, ref := range refs {
		wireRefs[i] = wireReportExternalReference(ref)
	}
	return v1.WorkstreamDescriptor{
		Schema:       v1.WorkstreamDescriptorSchemaPowercontextWorkstreamV1,
		ScopeID:      value.ScopeID(),
		ProjectID:    value.ProjectID(),
		Key:          nullableString(value.Key()),
		Title:        value.Title(),
		Kind:         v1.WorkstreamKind(value.Kind()),
		CatalogState: v1.ReportCatalogState(value.CatalogState()),
		ExternalRefs: wireRefs,
		Labels:       value.Labels(),
		Version:      value.Version(),
	}
}

func wireReportExternalReference(value handoffreport.ExternalReference) v1.HandoffReportExternalReference {
	return v1.HandoffReportExternalReference{
		Kind:       v1.HandoffReportExternalReferenceKind(value.Kind()),
		Provider:   value.Provider(),
		ExternalID: value.ExternalID(),
		URL:        nullableString(value.URL()),
	}
}

func wireReportRepository(value handoffreport.RepositoryRef) v1.HandoffReportRepositoryRef {
	return v1.HandoffReportRepositoryRef{
		Provider:         v1.HandoffReportRepositoryRefProvider(value.Provider()),
		RepositoryID:     nullableString(value.RepositoryID()),
		NormalizedRemote: nullableString(value.NormalizedRemote()),
		Subpath:          nullableString(value.Subpath()),
	}
}

func wireReportWorkspace(value handoffreport.WorkspaceBinding) v1.HandoffReportWorkspaceBinding {
	return v1.HandoffReportWorkspaceBinding{
		Schema:              v1.HandoffReportWorkspaceBindingSchemaPowercontextWorkspaceBindingV1,
		WorkspaceInstanceID: value.WorkspaceInstanceID(),
		ProjectID:           value.ProjectID(),
		RepositoryRef:       wireReportRepository(value.RepositoryRef()),
		State:               v1.HandoffReportWorkspaceBindingState(value.State()),
		ConfirmedAt:         value.ConfirmedAt(),
		Version:             value.Version(),
	}
}

func wireReportActivity(value handoffreport.ActivityEvent) v1.HandoffReportActivity {
	refs := value.EvidenceRefs()
	wireRefs := make([]v1.HandoffReportExternalReference, len(refs))
	for i, ref := range refs {
		wireRefs[i] = wireReportExternalReference(ref)
	}
	result := v1.HandoffReportActivity{
		Schema:        v1.HandoffReportActivitySchemaPowercontextHandoffReportActivityV1,
		EventID:       value.EventID(),
		ProjectID:     value.ProjectID(),
		ScopeID:       nullableString(value.ScopeID()),
		Source:        v1.ReportActivitySource(value.Source()),
		SourceEventID: value.SourceEventID(),
		OccurredAt:    nullableDateTime(value.OccurredAt()),
		ObservedAt:    value.ObservedAt(),
		TimeBasis:     v1.ReportTimeBasis(value.TimeBasis()),
		Title:         nullableString(value.Title()),
		Summary:       nullableString(value.Summary()),
		SessionID:     nullableString(value.SessionID()),
		EvidenceRefs:  wireRefs,
		Trust:         v1.HandoffReportActivityTrustUntrustedObservation,
	}
	if source := value.SourceRef(); source != nil {
		result.SourceRef = v1.NewNilHandoffReportExternalReference(wireReportExternalReference(*source))
	} else {
		result.SourceRef.SetToNull()
	}
	if agent := value.Agent(); agent != nil {
		result.Agent = v1.NewNilHandoffReportActivityAgent(v1.HandoffReportActivityAgent{
			Provider: optionalNullableString(agent.Provider()),
			Label:    optionalNullableString(agent.Label()),
		})
	} else {
		result.Agent.SetToNull()
	}
	if vcs := value.VCSContext(); vcs != nil {
		result.VcsContext = v1.NewNilHandoffReportActivityVcsContext(v1.HandoffReportActivityVcsContext{
			Branch:       optionalNullableString(vcs.Branch()),
			HeadRevision: optionalNullableString(vcs.HeadRevision()),
		})
	} else {
		result.VcsContext.SetToNull()
	}
	return result
}

func wireHandoffReportObject(value handoffreport.Report) (v1.HandoffReportResponseReport, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	result := v1.HandoffReportResponseReport{}
	if err := result.UnmarshalJSON(encoded); err != nil {
		return nil, err
	}
	return result, nil
}

func enforceHandoffReportSize(value handoffreport.Report, size int) error {
	if size <= handoffreport.MaxReportBytes {
		return nil
	}
	return &handoffreport.TooLargeError{
		EstimatedBytes:      &size,
		SelectedWorkstreams: value.Coverage().SelectedWorkstreams,
		SelectedActivities:  len(value.ActivitySelection()),
	}
}

func optionalTime(value v1.OptNilDateTime) *time.Time {
	resolved, ok := value.Get()
	if !ok {
		return nil
	}
	return &resolved
}

func nullableDateTime(value *time.Time) v1.NilDateTime {
	if value == nil {
		result := v1.NilDateTime{}
		result.SetToNull()
		return result
	}
	return v1.NewNilDateTime(*value)
}

func nullableWireString(value v1.NilString) *string {
	resolved, ok := value.Get()
	if !ok {
		return nil
	}
	return &resolved
}

func nullableInt(value v1.NilInt) *int {
	resolved, ok := value.Get()
	if !ok {
		return nil
	}
	return &resolved
}

func optionalNullableString(value *string) v1.OptNilString {
	if value == nil {
		result := v1.OptNilString{}
		result.SetToNull()
		return result
	}
	return v1.NewOptNilString(*value)
}

var _ HandoffReportOperations = (*runtime.HandoffReportApplication)(nil)
