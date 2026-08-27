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

package endpoint

import (
	"context"
	"encoding/json"
	"time"

	v1 "github.com/ob-labs/powercontext-go/api/v1"
	"github.com/ob-labs/powercontext-go/internal/handoffreport"
)

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
