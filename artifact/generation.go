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

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

const (
	MaxGenerationEvidence      = 32
	MaxGenerationEvidenceChars = 64_000
)

type EvidenceKind string

const (
	SourceEvidence   EvidenceKind = "source"
	ArtifactEvidence EvidenceKind = "artifact"
)

type GenerationEvidence struct {
	EvidenceID string
	Kind       EvidenceKind
	Content    string
	Truncated  bool
}

func NewGenerationEvidence(id string, kind EvidenceKind, content string, truncated bool) (GenerationEvidence, error) {
	if id == "" {
		return GenerationEvidence{}, fmt.Errorf("generation evidence ID must not be empty")
	}
	if kind != SourceEvidence && kind != ArtifactEvidence {
		return GenerationEvidence{}, fmt.Errorf("invalid generation evidence kind %q", kind)
	}
	if content == "" || utf8.RuneCountInString(content) > MaxGenerationEvidenceChars {
		return GenerationEvidence{}, fmt.Errorf("generation evidence content must contain 1..%d characters", MaxGenerationEvidenceChars)
	}
	return GenerationEvidence{EvidenceID: id, Kind: kind, Content: content, Truncated: truncated}, nil
}

type GenerationInput struct {
	evidence         []GenerationEvidence
	targetEvidenceID *string
}

func NewGenerationInput(evidence []GenerationEvidence, targetEvidenceID *string) (GenerationInput, error) {
	if len(evidence) < 1 || len(evidence) > MaxGenerationEvidence {
		return GenerationInput{}, fmt.Errorf("generation evidence must contain 1..%d items", MaxGenerationEvidence)
	}
	if targetEvidenceID != nil && strings.TrimSpace(*targetEvidenceID) == "" {
		return GenerationInput{}, fmt.Errorf("target evidence ID must not be blank")
	}
	return GenerationInput{evidence: slices.Clone(evidence), targetEvidenceID: cloneText(targetEvidenceID)}, nil
}

func (i GenerationInput) Evidence() []GenerationEvidence { return slices.Clone(i.evidence) }
func (i GenerationInput) TargetEvidenceID() *string      { return cloneText(i.targetEvidenceID) }

func cloneText(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
