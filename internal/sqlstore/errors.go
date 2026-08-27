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

package sqlstore

import "fmt"

// DatabaseClosedError reports that the database owner no longer admits work.
type DatabaseClosedError struct{}

func (*DatabaseClosedError) Error() string { return "database owner is closed" }

// InvalidStoredPayloadError reports persisted JSON outside its schema.
type InvalidStoredPayloadError struct {
	Kind  string
	Name  string
	Issue string
}

func (e *InvalidStoredPayloadError) Error() string {
	return fmt.Sprintf("invalid stored %s payload for %q: %s", e.Kind, e.Name, e.Issue)
}

// IdentityMismatchError reports disagreement between indexed columns and a
// decoded payload.
type IdentityMismatchError struct {
	Kind    string
	Indexed any
	Decoded any
}

func (e *IdentityMismatchError) Error() string {
	return fmt.Sprintf("stored %s identity mismatch: indexed=%v, decoded=%v", e.Kind, e.Indexed, e.Decoded)
}

// StoredPayloadConflictError reports reuse of a stable identity for different
// canonical bytes.
type StoredPayloadConflictError struct {
	Kind     string
	Identity any
}

func (e *StoredPayloadConflictError) Error() string {
	return fmt.Sprintf("%s identity %v already stores a different payload", e.Kind, e.Identity)
}

// RepositoryNotFoundError reports a missing persisted object or codec route.
type RepositoryNotFoundError struct {
	Kind     string
	Identity any
}

func (e *RepositoryNotFoundError) Error() string {
	return fmt.Sprintf("%s %v was not found", e.Kind, e.Identity)
}

// InvalidRepositoryArgumentError reports an invalid repository control value.
type InvalidRepositoryArgumentError struct {
	Field  string
	Detail string
}

func (e *InvalidRepositoryArgumentError) Error() string {
	return fmt.Sprintf("invalid repository argument %s: %s", e.Field, e.Detail)
}

// InvalidStoredColumnError reports a driver value outside the declared schema.
type InvalidStoredColumnError struct {
	Column   string
	Expected string
}

func (e *InvalidStoredColumnError) Error() string {
	return fmt.Sprintf("stored %s column is not %s", e.Column, e.Expected)
}

// CodecConflictError reports an ambiguous family or concrete-type route.
type CodecConflictError struct {
	Route string
	Value any
}

func (e *CodecConflictError) Error() string {
	return fmt.Sprintf("duplicate persistence codec %s: %v", e.Route, e.Value)
}

// GenerationConflictError reports a stale Source cursor CAS.
type GenerationConflictError struct {
	BindingName string
	Expected    *int64
	Actual      *int64
}

func (e *GenerationConflictError) Error() string {
	return fmt.Sprintf(
		"trigger state %q generation conflict: expected %v, found %v",
		e.BindingName,
		optionalInteger(e.Expected),
		optionalInteger(e.Actual),
	)
}

func optionalInteger(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}
