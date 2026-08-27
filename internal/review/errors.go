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

package review

import (
	"fmt"

	"github.com/ob-labs/powercontext-go/artifact"
)

type CandidateNotFoundError struct{ CandidateID string }

func (*CandidateNotFoundError) Error() string { return "Candidate was not found" }

type CandidateConflictError struct {
	CandidateID     string
	ExpectedVersion int64
	CurrentVersion  int64
}

func (*CandidateConflictError) Error() string { return "Candidate version is stale" }

type CandidateTerminalError struct {
	CandidateID string
	Status      Status
}

func (*CandidateTerminalError) Error() string { return "Candidate is already terminal" }

type InvalidCandidateError struct {
	Field  string
	Detail string
}

func (e *InvalidCandidateError) Error() string {
	return fmt.Sprintf("invalid Candidate %s: %s", e.Field, e.Detail)
}

type ArtifactTargetConflictError struct {
	Target  artifact.Ref
	Current artifact.Ref
}

func (*ArtifactTargetConflictError) Error() string {
	return "Candidate target is not the current Artifact head"
}

type GenerationCapabilityUnavailableError struct{ Family string }

func (e *GenerationCapabilityUnavailableError) Error() string {
	return e.Family + " generation is not configured"
}
