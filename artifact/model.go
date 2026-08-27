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
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ob-labs/powercontext-go/source"
)

const (
	MaxFamilyLength = 128
	MaxIDLength     = 128
)

type Ref struct {
	family     string
	artifactID string
	revision   int64
}

func NewRef(family, artifactID string, revision int64) (Ref, error) {
	if err := validatePart("family", family, MaxFamilyLength); err != nil {
		return Ref{}, err
	}
	if err := validatePart("artifact_id", artifactID, MaxIDLength); err != nil {
		return Ref{}, err
	}
	if revision < 1 {
		return Ref{}, &InvalidReferenceError{Field: "revision", Detail: "must be greater than or equal to 1"}
	}
	return Ref{family: family, artifactID: artifactID, revision: revision}, nil
}

func (r Ref) Family() string  { return r.family }
func (r Ref) ID() string      { return r.artifactID }
func (r Ref) Revision() int64 { return r.revision }
func (r Ref) String() string  { return fmt.Sprintf("%s:%s@%d", r.family, r.artifactID, r.revision) }
func (r Ref) IsZero() bool    { return r.family == "" && r.artifactID == "" && r.revision == 0 }
func (r Ref) Validate() error { _, err := NewRef(r.family, r.artifactID, r.revision); return err }

type Lineage struct {
	sources   []source.Ref
	artifacts []Ref
}

func NewLineage(sources []source.Ref, artifacts []Ref) (Lineage, error) {
	for _, ref := range sources {
		if _, err := source.NewRef(ref.Type(), ref.ID()); err != nil {
			return Lineage{}, err
		}
	}
	for _, ref := range artifacts {
		if err := ref.Validate(); err != nil {
			return Lineage{}, err
		}
	}
	return Lineage{sources: cloneSlice(sources), artifacts: cloneSlice(artifacts)}, nil
}

func (l Lineage) Sources() []source.Ref { return cloneSlice(l.sources) }
func (l Lineage) Artifacts() []Ref      { return cloneSlice(l.artifacts) }

type Draft[T any] struct {
	family  string
	content T
	lineage Lineage
}

func NewDraft[T any](family string, content T, sources []source.Ref, artifacts []Ref) (Draft[T], error) {
	if err := validatePart("family", family, MaxFamilyLength); err != nil {
		return Draft[T]{}, err
	}
	lineage, err := NewLineage(sources, artifacts)
	if err != nil {
		return Draft[T]{}, err
	}
	return Draft[T]{family: family, content: content, lineage: lineage}, nil
}

func (d Draft[T]) Family() string    { return d.family }
func (d Draft[T]) Content() T        { return d.content }
func (d Draft[T]) ContentValue() any { return d.content }
func (d Draft[T]) Lineage() Lineage  { return cloneLineage(d.lineage) }

type Artifact[T any] struct {
	ref     Ref
	content T
	lineage Lineage
}

func New[T any](artifactID string, revision int64, draft Draft[T]) (Artifact[T], error) {
	ref, err := NewRef(draft.family, artifactID, revision)
	if err != nil {
		return Artifact[T]{}, err
	}
	return Artifact[T]{ref: ref, content: draft.content, lineage: cloneLineage(draft.lineage)}, nil
}

func Restore[T any](ref Ref, content T, lineage Lineage) (Artifact[T], error) {
	if err := ref.Validate(); err != nil {
		return Artifact[T]{}, err
	}
	validated, err := NewLineage(lineage.sources, lineage.artifacts)
	if err != nil {
		return Artifact[T]{}, err
	}
	return Artifact[T]{ref: ref, content: content, lineage: validated}, nil
}

func (a Artifact[T]) Ref() Ref          { return a.ref }
func (a Artifact[T]) Family() string    { return a.ref.family }
func (a Artifact[T]) ID() string        { return a.ref.artifactID }
func (a Artifact[T]) Revision() int64   { return a.ref.revision }
func (a Artifact[T]) Content() T        { return a.content }
func (a Artifact[T]) ContentValue() any { return a.content }
func (a Artifact[T]) Lineage() Lineage  { return cloneLineage(a.lineage) }

// Snapshot is the non-generic read view used when evidence may cite different
// Artifact families.
type Snapshot interface {
	Ref() Ref
	ContentValue() any
	Lineage() Lineage
}

// DraftSnapshot is the non-generic write view used by relational stores whose
// family registry may contain several concrete content types.
type DraftSnapshot interface {
	Family() string
	ContentValue() any
	Lineage() Lineage
}

func cloneLineage(value Lineage) Lineage {
	return Lineage{sources: cloneSlice(value.sources), artifacts: cloneSlice(value.artifacts)}
}

func cloneSlice[T any](values []T) []T {
	result := make([]T, len(values))
	copy(result, values)
	return result
}

func validatePart(field, value string, maximum int) error {
	trimmed := strings.TrimFunc(value, isPythonWhitespace)
	if trimmed == "" {
		return &InvalidReferenceError{Field: field, Detail: "must be a non-empty string"}
	}
	if value != trimmed {
		return &InvalidReferenceError{Field: field, Detail: "must not contain leading or trailing whitespace"}
	}
	if utf8.RuneCountInString(value) > maximum {
		return &InvalidReferenceError{Field: field, Detail: fmt.Sprintf("must not exceed %d characters", maximum)}
	}
	return nil
}

func isPythonWhitespace(character rune) bool {
	return unicode.IsSpace(character) || character >= '\u001c' && character <= '\u001f'
}
