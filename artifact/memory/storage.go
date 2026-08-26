package memory

import (
	"slices"

	"github.com/ob-labs/powercontext-go/artifact"
)

// Projection is rebuildable active-head search state. EntryVersion and vector
// values are copied at construction and access.
type Projection struct {
	entryVersion         EntryVersion
	searchableText       string
	embedding            []float64
	embeddingContentHash *string
}

func NewProjection(
	entry EntryVersion,
	searchableText string,
	embedding []float64,
	embeddingContentHash *string,
) (Projection, error) {
	return Projection{
		entryVersion:         entry.Clone(),
		searchableText:       searchableText,
		embedding:            slices.Clone(embedding),
		embeddingContentHash: cloneString(embeddingContentHash),
	}, nil
}

func (p Projection) EntryVersion() EntryVersion { return p.entryVersion.Clone() }
func (p Projection) SearchableText() string     { return p.searchableText }
func (p Projection) Embedding() []float64       { return slices.Clone(p.embedding) }
func (p Projection) EmbeddingContentHash() *string {
	return cloneString(p.embeddingContentHash)
}

// Commit is the complete authoritative revision and its transaction-coupled
// entry/index rows.
type Commit struct {
	base          *Memory
	memory        Memory
	contentHash   string
	entryVersions []EntryVersion
	projections   []Projection
}

func NewCommit(
	base *Memory,
	next Memory,
	contentHash string,
	entryVersions []EntryVersion,
	projections []Projection,
) Commit {
	return Commit{
		base:          cloneMemory(base),
		memory:        cloneMemoryValue(next),
		contentHash:   contentHash,
		entryVersions: cloneEntryVersions(entryVersions),
		projections:   cloneProjections(projections),
	}
}

func (c Commit) Base() *Memory                 { return cloneMemory(c.base) }
func (c Commit) Memory() Memory                { return cloneMemoryValue(c.memory) }
func (c Commit) ContentHash() string           { return c.contentHash }
func (c Commit) EntryVersions() []EntryVersion { return cloneEntryVersions(c.entryVersions) }
func (c Commit) Projections() []Projection     { return cloneProjections(c.projections) }

type WritePlan struct {
	result *Memory
	commit *Commit
}

func NewWritePlan(result *Memory, commit *Commit) WritePlan {
	var clonedCommit *Commit
	if commit != nil {
		value := NewCommit(commit.Base(), commit.Memory(), commit.ContentHash(), commit.EntryVersions(), commit.Projections())
		clonedCommit = &value
	}
	return WritePlan{result: cloneMemory(result), commit: clonedCommit}
}

func (p WritePlan) Result() *Memory { return cloneMemory(p.result) }
func (p WritePlan) Commit() *Commit {
	if p.commit == nil {
		return nil
	}
	value := NewCommit(p.commit.Base(), p.commit.Memory(), p.commit.ContentHash(), p.commit.EntryVersions(), p.commit.Projections())
	return &value
}

// RevisionChanges is the compact change list for one exact Memory revision.
type RevisionChanges struct {
	MemoryRef artifact.Ref
	Changes   []Change
}

func (r RevisionChanges) Clone() RevisionChanges {
	changes := make([]Change, len(r.Changes))
	for index, change := range r.Changes {
		changes[index] = change.Clone()
	}
	r.Changes = changes
	return r
}

// SearchRequest is the validated backend request after capability selection.
type SearchRequest struct {
	Query            string
	AnalyzedQuery    string
	Memories         []artifact.Ref
	CandidateLimit   int
	Mode             SearchMode
	QueryVector      []float64
	EmbeddingProfile *EmbeddingProfile
}

func (r SearchRequest) Clone() SearchRequest {
	r.Memories = slices.Clone(r.Memories)
	r.QueryVector = slices.Clone(r.QueryVector)
	if r.EmbeddingProfile != nil {
		profile := *r.EmbeddingProfile
		r.EmbeddingProfile = &profile
	}
	return r
}

// SearchChannels preserves backend ordering before shared admission and RRF.
type SearchChannels struct {
	FTS    []ChannelHit
	Vector []ChannelHit
}

func (c SearchChannels) Clone() SearchChannels {
	c.FTS = cloneChannelHits(c.FTS)
	c.Vector = cloneChannelHits(c.Vector)
	return c
}

func cloneMemory(value *Memory) *Memory {
	if value == nil {
		return nil
	}
	cloned := cloneMemoryValue(*value)
	return &cloned
}

func cloneMemoryValue(value Memory) Memory {
	content := NewContent(value.Content().Manifest(), value.Content().Changes())
	lineage := value.Lineage()
	cloned, err := artifact.Restore(value.Ref(), content, lineage)
	if err != nil {
		panic(err)
	}
	return cloned
}

func cloneEntryVersions(values []EntryVersion) []EntryVersion {
	result := make([]EntryVersion, len(values))
	for index, value := range values {
		result[index] = value.Clone()
	}
	return result
}

func cloneProjections(values []Projection) []Projection {
	result := make([]Projection, len(values))
	for index, value := range values {
		result[index] = Projection{
			entryVersion:         value.entryVersion.Clone(),
			searchableText:       value.searchableText,
			embedding:            slices.Clone(value.embedding),
			embeddingContentHash: cloneString(value.embeddingContentHash),
		}
	}
	return result
}

func cloneChannelHits(values []ChannelHit) []ChannelHit {
	result := make([]ChannelHit, len(values))
	for index, value := range values {
		result[index] = value.Clone()
	}
	return result
}
