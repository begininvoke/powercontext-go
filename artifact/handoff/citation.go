package handoff

import (
	"github.com/ob-labs/powercontext-go/artifact"
	"github.com/ob-labs/powercontext-go/artifact/memory"
	"github.com/ob-labs/powercontext-go/source"
)

type Citation interface {
	Kind() CitationKind
	citationKey() string
}

type SourceCitation struct{ ref source.Ref }

func NewSourceCitation(ref source.Ref) (SourceCitation, error) {
	value := SourceCitation{ref: ref}
	if err := value.Validate(); err != nil {
		return SourceCitation{}, err
	}
	return value, nil
}
func (c SourceCitation) Kind() CitationKind { return SourceCitationKind }
func (c SourceCitation) Ref() source.Ref    { return c.ref }
func (c SourceCitation) Validate() error {
	_, err := source.NewRef(c.ref.Type(), c.ref.ID())
	return err
}
func (c SourceCitation) citationKey() string {
	return "source\x00" + c.ref.Type() + "\x00" + c.ref.ID()
}

type ArtifactCitation struct{ ref artifact.Ref }

func NewArtifactCitation(ref artifact.Ref) (ArtifactCitation, error) {
	value := ArtifactCitation{ref: ref}
	if err := value.Validate(); err != nil {
		return ArtifactCitation{}, err
	}
	return value, nil
}
func (c ArtifactCitation) Kind() CitationKind  { return ArtifactCitationKind }
func (c ArtifactCitation) Ref() artifact.Ref   { return c.ref }
func (c ArtifactCitation) Validate() error     { return c.ref.Validate() }
func (c ArtifactCitation) citationKey() string { return "artifact\x00" + c.ref.String() }

type MemoryCitation struct{ citation memory.Citation }

func NewMemoryCitation(value memory.Citation) (MemoryCitation, error) {
	citation := MemoryCitation{citation: value}
	if err := citation.Validate(); err != nil {
		return MemoryCitation{}, err
	}
	return citation, nil
}
func (c MemoryCitation) Kind() CitationKind        { return MemoryCitationKind }
func (c MemoryCitation) Citation() memory.Citation { return c.citation }
func (c MemoryCitation) Validate() error {
	if err := c.citation.MemoryRef.Validate(); err != nil {
		return err
	}
	if _, err := memory.ValidateIdentifier(c.citation.EntryID); err != nil {
		return err
	}
	_, err := memory.ValidateIdentifier(c.citation.EntryVersionID)
	return err
}
func (c MemoryCitation) citationKey() string {
	return "memory\x00" + c.citation.MemoryRef.String() + "\x00" + c.citation.EntryID + "\x00" + c.citation.EntryVersionID
}
