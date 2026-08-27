package handoff

import (
	"fmt"

	"github.com/ob-labs/powercontext-go/artifact"
	"github.com/ob-labs/powercontext-go/artifact/memory"
	"github.com/ob-labs/powercontext-go/source"
)

type Evidence interface {
	Citation() Citation
	evidenceValue()
}

type SourceEvidence struct {
	citation SourceCitation
	source   source.Value
}

func NewSourceEvidence(citation SourceCitation, value source.Value) (SourceEvidence, error) {
	evidence := SourceEvidence{citation: citation, source: value}
	if err := evidence.Validate(); err != nil {
		return SourceEvidence{}, err
	}
	return evidence, nil
}
func (e SourceEvidence) Citation() Citation   { return e.citation }
func (SourceEvidence) evidenceValue()         {}
func (e SourceEvidence) Source() source.Value { return e.source }
func (e SourceEvidence) Validate() error {
	if err := e.citation.Validate(); err != nil {
		return err
	}
	if isNilInterface(e.source) || e.source.SourceName() != e.citation.ref.ID() {
		return fmt.Errorf("resolved Source does not match its Handoff citation")
	}
	return nil
}

type ArtifactEvidence struct {
	citation ArtifactCitation
	value    artifact.Snapshot
}

func NewArtifactEvidence(citation ArtifactCitation, value artifact.Snapshot) (ArtifactEvidence, error) {
	evidence := ArtifactEvidence{citation: citation, value: value}
	if err := evidence.Validate(); err != nil {
		return ArtifactEvidence{}, err
	}
	return evidence, nil
}
func (e ArtifactEvidence) Citation() Citation          { return e.citation }
func (ArtifactEvidence) evidenceValue()                {}
func (e ArtifactEvidence) Artifact() artifact.Snapshot { return e.value }
func (e ArtifactEvidence) Validate() error {
	if err := e.citation.Validate(); err != nil {
		return err
	}
	if isNilInterface(e.value) || e.value.Ref() != e.citation.ref {
		return fmt.Errorf("resolved Artifact does not match its Handoff citation")
	}
	return nil
}

type MemoryEvidence struct {
	citation MemoryCitation
	entry    memory.EntryVersion
}

func NewMemoryEvidence(citation MemoryCitation, entry memory.EntryVersion) (MemoryEvidence, error) {
	evidence := MemoryEvidence{citation: citation, entry: entry.Clone()}
	if err := evidence.Validate(); err != nil {
		return MemoryEvidence{}, err
	}
	return evidence, nil
}
func (e MemoryEvidence) Citation() Citation         { return e.citation }
func (MemoryEvidence) evidenceValue()               {}
func (e MemoryEvidence) Entry() memory.EntryVersion { return e.entry.Clone() }
func (e MemoryEvidence) Validate() error {
	if err := e.citation.Validate(); err != nil {
		return err
	}
	reference := e.citation.citation
	if reference.MemoryRef.ID() != e.entry.MemoryArtifactID || reference.EntryID != e.entry.EntryID ||
		reference.EntryVersionID != e.entry.EntryVersionID {
		return fmt.Errorf("resolved Memory entry does not match its Handoff citation")
	}
	return nil
}
