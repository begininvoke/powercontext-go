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

package v1

import (
	"errors"
	"testing"
)

func TestPowerContextContractRejectsCombinedCandidateEvidenceOverLimit(t *testing.T) {
	t.Parallel()
	sources := make([]SourceReference, 20)
	artifacts := make([]ArtifactReference, 13)

	for name, value := range map[string]any{
		"artifact candidate":    ArtifactCandidate{SourceRefs: sources, ArtifactRefs: artifacts},
		"generate experience":   &GenerateExperienceRequest{SourceRefs: sources, ArtifactRefs: artifacts},
		"generate skill":        &GenerateSkillRequest{SourceRefs: sources, ArtifactRefs: artifacts},
		"propose experience":    &ProposeExperienceRequest{SourceRefs: sources, ArtifactRefs: artifacts},
		"propose skill":         &ProposeSkillRequest{SourceRefs: sources, ArtifactRefs: artifacts},
		"revise candidate":      &ReviseArtifactCandidateRequest{SourceRefs: sources, ArtifactRefs: artifacts},
		"candidate response":    &ArtifactCandidateHeaders{Response: ArtifactCandidate{SourceRefs: sources, ArtifactRefs: artifacts}},
		"candidate page":        &ArtifactCandidatePage{Candidates: []ArtifactCandidate{{SourceRefs: sources, ArtifactRefs: artifacts}}},
		"candidate page header": &ArtifactCandidatePageHeaders{Response: ArtifactCandidatePage{Candidates: []ArtifactCandidate{{SourceRefs: sources, ArtifactRefs: artifacts}}}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := ValidatePowerContextContract(value)
			var limit *CombinedEvidenceLimitError
			if !errors.As(err, &limit) {
				t.Fatalf("error = %#v, want CombinedEvidenceLimitError", err)
			}
			if limit.SourceReferences != 20 || limit.ArtifactReferences != 13 {
				t.Fatalf("evidence counts = (%d, %d), want (20, 13)", limit.SourceReferences, limit.ArtifactReferences)
			}
		})
	}
}

func TestPowerContextContractAcceptsCombinedCandidateEvidenceAtLimit(t *testing.T) {
	t.Parallel()
	request := &ProposeExperienceRequest{
		SourceRefs:   make([]SourceReference, 20),
		ArtifactRefs: make([]ArtifactReference, 12),
	}
	if err := ValidatePowerContextContract(request); err != nil {
		t.Fatalf("32 combined references rejected: %v", err)
	}
}
