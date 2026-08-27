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

package artifact

import "fmt"

type NotFoundError struct{ Ref Ref }

func (e *NotFoundError) Error() string { return "artifact was not found" }

type InvalidReferenceError struct {
	Field  string
	Detail string
}

func (e *InvalidReferenceError) Error() string {
	return fmt.Sprintf("invalid Artifact reference %s: %s", e.Field, e.Detail)
}

type FamilyMismatchError struct {
	ArtifactFamily string
	DraftFamily    string
}

func (e *FamilyMismatchError) Error() string { return "artifact and draft families do not match" }

type RevisionConflictError struct {
	Requested Ref
	Current   Ref
}

func (e *RevisionConflictError) Error() string { return "artifact is not the latest revision" }
