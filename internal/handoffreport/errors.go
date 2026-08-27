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

package handoffreport

import "fmt"

type Error struct{ Cause error }

func (e *Error) Error() string {
	if e.Cause == nil {
		return "Handoff Report is unavailable"
	}
	return "Handoff Report is unavailable: " + e.Cause.Error()
}
func (e *Error) Unwrap() error { return e.Cause }

type BusyError struct{ Attempts int }

func (e *BusyError) Error() string {
	return fmt.Sprintf("Handoff heads remained unstable after %d attempts", e.Attempts)
}

type InconsistentError struct{ ScopeID string }

func (e *InconsistentError) Error() string {
	return fmt.Sprintf("the frozen Handoff selection became inconsistent for scope %q", e.ScopeID)
}

type EvidenceCheckUnavailableError struct{}

func (*EvidenceCheckUnavailableError) Error() string {
	return "the Handoff read adapter cannot independently check evidence"
}

type TooLargeError struct {
	SelectedWorkstreams int
	SelectedActivities  int
	EstimatedBytes      *int
}

func (*TooLargeError) Error() string {
	return "the Handoff Report exceeds the configured projection limit"
}

type CatalogArgumentError struct {
	Field  string
	Detail string
}

func (e *CatalogArgumentError) Error() string {
	return fmt.Sprintf("invalid Handoff Report catalog argument %s: %s", e.Field, e.Detail)
}

type InvalidActivityEventError struct {
	Field  string
	Detail string
}

func (e *InvalidActivityEventError) Error() string {
	return fmt.Sprintf("invalid Handoff Report Activity %s: %s", e.Field, e.Detail)
}

type InvalidActivityRepositoryArgumentError struct {
	Field  string
	Detail string
}

func (e *InvalidActivityRepositoryArgumentError) Error() string {
	return fmt.Sprintf("invalid Handoff Report Activity repository argument %s: %s", e.Field, e.Detail)
}

type InvalidStoredCatalogError struct {
	Kind   string
	Detail string
}

func (e *InvalidStoredCatalogError) Error() string {
	return fmt.Sprintf("invalid stored Handoff Report %s: %s", e.Kind, e.Detail)
}

type ProjectNotFoundError struct{ ProjectID string }

func (e *ProjectNotFoundError) Error() string {
	return fmt.Sprintf("Report Project %q was not found", e.ProjectID)
}
func (*ProjectNotFoundError) Code() string { return "project_not_found" }

type WorkstreamNotFoundError struct{ ScopeID string }

func (e *WorkstreamNotFoundError) Error() string {
	return fmt.Sprintf("Report Workstream %q was not found", e.ScopeID)
}
func (*WorkstreamNotFoundError) Code() string { return "scope_not_grouped" }

type ProjectConflictError struct {
	ProjectID       string
	ExpectedVersion *int
	CurrentVersion  *int
	Detail          string
}

func (e *ProjectConflictError) Error() string {
	if e.Detail != "" {
		return e.Detail
	}
	return "Project version or key conflicts with the current catalog"
}
func (*ProjectConflictError) Code() string { return "project_conflict" }

type WorkstreamConflictError struct {
	ScopeID         string
	ExpectedVersion *int
	CurrentVersion  *int
	Detail          string
}

func (e *WorkstreamConflictError) Error() string {
	if e.Detail != "" {
		return e.Detail
	}
	return "Workstream version or key conflicts with the current catalog"
}
func (*WorkstreamConflictError) Code() string { return "workstream_conflict" }

type ScopeAlreadyGroupedError struct {
	ScopeID   string
	ProjectID string
}

func (e *ScopeAlreadyGroupedError) Error() string {
	return fmt.Sprintf("scope %q already belongs to Project %q", e.ScopeID, e.ProjectID)
}
func (*ScopeAlreadyGroupedError) Code() string { return "scope_already_grouped" }

type WorkspaceBindingNotFoundError struct{ WorkspaceInstanceID string }

func (e *WorkspaceBindingNotFoundError) Error() string {
	return fmt.Sprintf("workspace %q has no confirmed Report binding", e.WorkspaceInstanceID)
}
func (*WorkspaceBindingNotFoundError) Code() string { return "workspace_not_bound" }

type WorkspaceBindingConflictError struct {
	WorkspaceInstanceID string
	ExpectedVersion     *int
	CurrentVersion      *int
	Detail              string
}

func (e *WorkspaceBindingConflictError) Error() string {
	if e.Detail != "" {
		return e.Detail
	}
	return "workspace binding version conflicts with the current catalog"
}
func (*WorkspaceBindingConflictError) Code() string { return "workspace_binding_conflict" }

type ActivityEventConflictError struct {
	Source        ActivitySource
	SourceEventID string
}

func (e *ActivityEventConflictError) Error() string {
	return fmt.Sprintf("Activity idempotency key %s/%s identifies different content", e.Source, e.SourceEventID)
}

type CanonicalizationError struct {
	Code   string
	Detail any
}

func (e *CanonicalizationError) Error() string {
	switch e.Code {
	case "unknown-event":
		return fmt.Sprintf("activity selection references unknown event %q", e.Detail)
	case "timestamp":
		return "digest timestamps must be timezone-aware"
	case "float":
		return "digest inputs must not contain floating-point values"
	case "key-type":
		return "digest object keys must be strings"
	case "key-collision":
		return "digest object keys collide after NFC normalization"
	default:
		return fmt.Sprintf("digest input contains unsupported value type %v", e.Detail)
	}
}
