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

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/ob-labs/powercontext-go/artifact"
	"github.com/ob-labs/powercontext-go/artifact/handoff"
	"github.com/ob-labs/powercontext-go/artifact/memory"
	"github.com/ob-labs/powercontext-go/source"
)

// HandoffArtifactCodec returns the versioned Handoff content route.
func HandoffArtifactCodec() ArtifactCodec {
	codec, err := NewArtifactCodec(handoff.Family, encodeHandoff, decodeHandoff)
	if err != nil {
		panic(err)
	}
	return codec
}

type sourceRefJSON struct {
	SourceType string `json:"source_type"`
	SourceID   string `json:"source_id"`
}

type artifactRefJSON struct {
	Family     string `json:"family"`
	ArtifactID string `json:"artifact_id"`
	Revision   int64  `json:"revision"`
}

type memoryCitationJSON struct {
	MemoryRef      artifactRefJSON `json:"memory_ref"`
	EntryID        string          `json:"entry_id"`
	EntryVersionID string          `json:"entry_version_id"`
}

type handoffCitationJSON struct {
	Kind           string              `json:"kind"`
	SourceRef      *sourceRefJSON      `json:"source_ref,omitempty"`
	ArtifactRef    *artifactRefJSON    `json:"artifact_ref,omitempty"`
	MemoryCitation *memoryCitationJSON `json:"memory_citation,omitempty"`
}

type handoffStatementJSON struct {
	Text      string                `json:"text"`
	Citations []handoffCitationJSON `json:"citations"`
}

type handoffOmissionJSON struct {
	Text     string               `json:"text"`
	Citation *handoffCitationJSON `json:"citation"`
}

type handoffContentJSON struct {
	Schema      string                 `json:"schema"`
	Objective   string                 `json:"objective"`
	State       []handoffStatementJSON `json:"state"`
	Disposition string                 `json:"disposition"`
	NextAction  *handoffStatementJSON  `json:"next_action"`
	Omissions   []handoffOmissionJSON  `json:"omissions"`
}

func encodeHandoff(value handoff.Content) ([]byte, error) {
	if err := value.Validate(); err != nil {
		return nil, err
	}
	state := value.State()
	encodedState := make([]handoffStatementJSON, len(state))
	for index, statement := range state {
		encoded, err := encodeHandoffStatement(statement)
		if err != nil {
			return nil, err
		}
		encodedState[index] = encoded
	}
	var nextAction *handoffStatementJSON
	if next := value.NextAction(); next != nil {
		encoded, err := encodeHandoffStatement(*next)
		if err != nil {
			return nil, err
		}
		nextAction = &encoded
	}
	omissions := value.Omissions()
	encodedOmissions := make([]handoffOmissionJSON, len(omissions))
	for index, omission := range omissions {
		encoded := handoffOmissionJSON{Text: omission.Text()}
		if omission.Citation() != nil {
			citation, err := encodeHandoffCitation(omission.Citation())
			if err != nil {
				return nil, err
			}
			encoded.Citation = &citation
		}
		encodedOmissions[index] = encoded
	}
	return marshalJSON(handoffContentJSON{
		Schema:      value.Schema(),
		Objective:   value.Objective(),
		State:       encodedState,
		Disposition: string(value.Disposition()),
		NextAction:  nextAction,
		Omissions:   encodedOmissions,
	})
}

func encodeHandoffStatement(value handoff.Statement) (handoffStatementJSON, error) {
	citations := value.Citations()
	encoded := make([]handoffCitationJSON, len(citations))
	for index, citation := range citations {
		value, err := encodeHandoffCitation(citation)
		if err != nil {
			return handoffStatementJSON{}, err
		}
		encoded[index] = value
	}
	return handoffStatementJSON{Text: value.Text(), Citations: encoded}, nil
}

func encodeHandoffCitation(value handoff.Citation) (handoffCitationJSON, error) {
	switch citation := value.(type) {
	case handoff.SourceCitation:
		ref := citation.Ref()
		encoded := sourceRefJSON{SourceType: ref.Type(), SourceID: ref.ID()}
		return handoffCitationJSON{Kind: string(handoff.SourceCitationKind), SourceRef: &encoded}, nil
	case handoff.ArtifactCitation:
		encoded := encodeArtifactRef(citation.Ref())
		return handoffCitationJSON{Kind: string(handoff.ArtifactCitationKind), ArtifactRef: &encoded}, nil
	case handoff.MemoryCitation:
		memoryRef := citation.Citation()
		encoded := memoryCitationJSON{
			MemoryRef:      encodeArtifactRef(memoryRef.MemoryRef),
			EntryID:        memoryRef.EntryID,
			EntryVersionID: memoryRef.EntryVersionID,
		}
		return handoffCitationJSON{Kind: string(handoff.MemoryCitationKind), MemoryCitation: &encoded}, nil
	default:
		return handoffCitationJSON{}, fmt.Errorf("unsupported Handoff citation %T", value)
	}
}

func decodeHandoff(payload []byte) (handoff.Content, error) {
	var fields map[string]json.RawMessage
	if err := unmarshalJSON(payload, &fields); err != nil {
		return handoff.Content{}, err
	}
	if err := rejectUnknown(fields, "schema", "objective", "state", "disposition", "next_action", "omissions"); err != nil {
		return handoff.Content{}, err
	}
	schema := handoff.ContentSchemaVersion
	if raw, ok := fields["schema"]; ok {
		if string(raw) == "null" {
			return handoff.Content{}, fmt.Errorf("Handoff schema cannot be null")
		}
		if err := unmarshalJSON(raw, &schema); err != nil {
			return handoff.Content{}, err
		}
	}
	if err := handoff.ValidateContentSchema(schema); err != nil {
		return handoff.Content{}, err
	}
	objective, err := requiredStringField(fields, "objective")
	if err != nil {
		return handoff.Content{}, err
	}
	disposition, err := requiredStringField(fields, "disposition")
	if err != nil {
		return handoff.Content{}, err
	}
	stateRaw, ok := fields["state"]
	if !ok || string(stateRaw) == "null" {
		return handoff.Content{}, fmt.Errorf("Handoff state is required")
	}
	var encodedState []json.RawMessage
	if err := unmarshalJSON(stateRaw, &encodedState); err != nil {
		return handoff.Content{}, err
	}
	state := make([]handoff.Statement, len(encodedState))
	for index, raw := range encodedState {
		statement, err := decodeHandoffStatement(raw)
		if err != nil {
			return handoff.Content{}, err
		}
		state[index] = statement
	}
	var nextAction *handoff.Statement
	if raw, exists := fields["next_action"]; exists && string(raw) != "null" {
		statement, err := decodeHandoffStatement(raw)
		if err != nil {
			return handoff.Content{}, err
		}
		nextAction = &statement
	}
	omissions := []handoff.Omission{}
	if raw, exists := fields["omissions"]; exists {
		if string(raw) == "null" {
			return handoff.Content{}, fmt.Errorf("Handoff omissions must be an array")
		}
		var encoded []json.RawMessage
		if err := unmarshalJSON(raw, &encoded); err != nil {
			return handoff.Content{}, err
		}
		omissions = make([]handoff.Omission, len(encoded))
		for index, item := range encoded {
			omission, err := decodeHandoffOmission(item)
			if err != nil {
				return handoff.Content{}, err
			}
			omissions[index] = omission
		}
	}
	return handoff.NewContent(objective, state, handoff.Disposition(disposition), nextAction, omissions)
}

func decodeHandoffStatement(payload []byte) (handoff.Statement, error) {
	var fields map[string]json.RawMessage
	if err := unmarshalJSON(payload, &fields); err != nil {
		return handoff.Statement{}, err
	}
	if err := rejectUnknown(fields, "text", "citations"); err != nil {
		return handoff.Statement{}, err
	}
	text, err := requiredStringField(fields, "text")
	if err != nil {
		return handoff.Statement{}, err
	}
	raw, ok := fields["citations"]
	if !ok || string(raw) == "null" {
		return handoff.Statement{}, fmt.Errorf("Handoff citations are required")
	}
	var encoded []json.RawMessage
	if err := unmarshalJSON(raw, &encoded); err != nil {
		return handoff.Statement{}, err
	}
	citations := make([]handoff.Citation, len(encoded))
	for index, item := range encoded {
		citation, err := decodeHandoffCitation(item)
		if err != nil {
			return handoff.Statement{}, err
		}
		citations[index] = citation
	}
	return handoff.NewStatement(text, citations)
}

func decodeHandoffOmission(payload []byte) (handoff.Omission, error) {
	var fields map[string]json.RawMessage
	if err := unmarshalJSON(payload, &fields); err != nil {
		return handoff.Omission{}, err
	}
	if err := rejectUnknown(fields, "text", "citation"); err != nil {
		return handoff.Omission{}, err
	}
	text, err := requiredStringField(fields, "text")
	if err != nil {
		return handoff.Omission{}, err
	}
	var citation handoff.Citation
	if raw, exists := fields["citation"]; exists && string(raw) != "null" {
		citation, err = decodeHandoffCitation(raw)
		if err != nil {
			return handoff.Omission{}, err
		}
	}
	return handoff.NewOmission(text, citation)
}

func decodeHandoffCitation(payload []byte) (handoff.Citation, error) {
	var fields map[string]json.RawMessage
	if err := unmarshalJSON(payload, &fields); err != nil {
		return nil, err
	}
	kind, err := requiredStringField(fields, "kind")
	if err != nil {
		return nil, err
	}
	switch handoff.CitationKind(kind) {
	case handoff.SourceCitationKind:
		if err := rejectUnknown(fields, "kind", "source_ref"); err != nil {
			return nil, err
		}
		raw, ok := fields["source_ref"]
		if !ok || string(raw) == "null" {
			return nil, fmt.Errorf("Handoff source_ref is required")
		}
		ref, err := decodeSourceRef(raw)
		if err != nil {
			return nil, err
		}
		return handoff.NewSourceCitation(ref)
	case handoff.ArtifactCitationKind:
		if err := rejectUnknown(fields, "kind", "artifact_ref"); err != nil {
			return nil, err
		}
		raw, ok := fields["artifact_ref"]
		if !ok || string(raw) == "null" {
			return nil, fmt.Errorf("Handoff artifact_ref is required")
		}
		ref, err := decodeArtifactRef(raw)
		if err != nil {
			return nil, err
		}
		return handoff.NewArtifactCitation(ref)
	case handoff.MemoryCitationKind:
		if err := rejectUnknown(fields, "kind", "memory_citation"); err != nil {
			return nil, err
		}
		raw, ok := fields["memory_citation"]
		if !ok || string(raw) == "null" {
			return nil, fmt.Errorf("Handoff memory_citation is required")
		}
		var encoded memoryCitationJSON
		if err := unmarshalJSON(raw, &encoded); err != nil {
			return nil, err
		}
		ref, err := artifact.NewRef(
			encoded.MemoryRef.Family,
			encoded.MemoryRef.ArtifactID,
			encoded.MemoryRef.Revision,
		)
		if err != nil {
			return nil, err
		}
		return handoff.NewMemoryCitation(memory.Citation{
			MemoryRef:      ref,
			EntryID:        encoded.EntryID,
			EntryVersionID: encoded.EntryVersionID,
		})
	default:
		return nil, fmt.Errorf("unsupported Handoff citation kind %q", kind)
	}
}

func decodeSourceRef(payload []byte) (source.Ref, error) {
	var encoded sourceRefJSON
	if err := unmarshalJSON(payload, &encoded); err != nil {
		return source.Ref{}, err
	}
	return source.NewRef(encoded.SourceType, encoded.SourceID)
}

func encodeArtifactRef(value artifact.Ref) artifactRefJSON {
	return artifactRefJSON{Family: value.Family(), ArtifactID: value.ID(), Revision: value.Revision()}
}

func decodeArtifactRef(payload []byte) (artifact.Ref, error) {
	var encoded artifactRefJSON
	if err := unmarshalJSON(payload, &encoded); err != nil {
		return artifact.Ref{}, err
	}
	return artifact.NewRef(encoded.Family, encoded.ArtifactID, encoded.Revision)
}

func requiredStringField(fields map[string]json.RawMessage, name string) (string, error) {
	raw, ok := fields[name]
	if !ok || string(raw) == "null" {
		return "", fmt.Errorf("field %q is required", name)
	}
	var value string
	if err := unmarshalJSON(raw, &value); err != nil {
		return "", err
	}
	return value, nil
}

func rejectUnknown(fields map[string]json.RawMessage, allowed ...string) error {
	for name := range fields {
		if !slices.Contains(allowed, name) {
			return fmt.Errorf("unexpected field %q", name)
		}
	}
	return nil
}
