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

package source

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// MaxIDLength is the maximum Source identifier length in Unicode code points.
	MaxIDLength = 256
	// MaxTypeLength is the maximum registered Source type length in Unicode code points.
	MaxTypeLength = 128
	// MaxBindingNameLength is the maximum persistent Source cursor binding name.
	MaxBindingNameLength = 128
)

// Materialization describes where a value read for a Source comes from.
type Materialization string

const (
	Captured   Materialization = "captured"
	Referenced Materialization = "referenced"
)

// Value is an immutable adapter-owned Source description. Implementations
// should use value receivers or otherwise prevent callers from changing the
// identity returned by SourceName.
type Value interface {
	SourceName() string
	SourceMaterialization() Materialization
	SourceDescription() (string, bool)
}

// Ref is a stable reference to one Source in a catalog view.
type Ref struct {
	sourceType string
	sourceID   string
}

// NewRef validates and constructs a Source reference.
func NewRef(sourceType, sourceID string) (Ref, error) {
	if err := validateReferencePart("source_type", sourceType, MaxTypeLength); err != nil {
		return Ref{}, err
	}
	if err := validateReferencePart("source_id", sourceID, MaxIDLength); err != nil {
		return Ref{}, err
	}
	return Ref{sourceType: sourceType, sourceID: sourceID}, nil
}

func (r Ref) Type() string { return r.sourceType }
func (r Ref) ID() string   { return r.sourceID }

func (r Ref) String() string { return r.sourceType + ":" + r.sourceID }

func (r Ref) MarshalText() ([]byte, error) {
	if _, err := NewRef(r.sourceType, r.sourceID); err != nil {
		return nil, err
	}
	return []byte(r.String()), nil
}

func validateReferencePart(field, value string, maximum int) error {
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

func validateValue(value Value) error {
	if value == nil {
		return &InvalidEntryError{}
	}
	if _, err := NewRef("source", value.SourceName()); err != nil {
		if invalid, ok := err.(*InvalidReferenceError); ok {
			invalid.Field = "source_id"
		}
		return err
	}
	switch value.SourceMaterialization() {
	case Captured, Referenced:
		return nil
	default:
		return fmt.Errorf("invalid Source materialization %q", value.SourceMaterialization())
	}
}
