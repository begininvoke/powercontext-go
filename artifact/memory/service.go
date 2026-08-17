package memory

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/thunguo/powercontext-go/artifact"
	"github.com/thunguo/powercontext-go/inference"
	"github.com/thunguo/powercontext-go/source"
)

type RememberMode string

const (
	RememberAppend  RememberMode = "append"
	RememberExtract RememberMode = "extract"
	RememberAuto    RememberMode = "auto"
)

type OrganizeMode string

const (
	OrganizeDefault   OrganizeMode = "default"
	OrganizeDedupe    OrganizeMode = "dedupe"
	OrganizeNormalize OrganizeMode = "normalize"
)

// Backend is the exact persistence/search surface consumed by Memory. Commit
// must atomically enforce the Artifact head CAS and replace all projections.
type Backend interface {
	Capabilities() Capabilities
	Get(context.Context, artifact.Ref) (Memory, error)
	Latest(context.Context, string) (Memory, error)
	Entries(context.Context, artifact.Ref) ([]EntryVersion, error)
	Projections(context.Context, artifact.Ref) ([]Projection, error)
	Commit(context.Context, Commit) (Memory, error)
	Changes(context.Context, artifact.Ref, *int64) ([]RevisionChanges, error)
	VectorComplete(context.Context, []artifact.Ref, EmbeddingProfile) (bool, error)
	Search(context.Context, SearchRequest) (SearchChannels, error)
	Expand(context.Context, []Hit) ([]EntryVersion, error)
}

type SourceResolver interface {
	Get(context.Context, source.Value) (source.Value, error)
	Ref(source.Value) (source.Ref, error)
}

type ArtifactResolver interface {
	Get(context.Context, artifact.Snapshot) (artifact.Snapshot, error)
}

type IDFactory func(string) (string, error)
type Clock func() time.Time

type ServiceOptions struct {
	CandidatePipeline    CandidatePipeline
	EmbeddingModel       inference.EmbeddingModel
	Reranker             Reranker
	RerankCandidateLimit int
	SourceResolver       SourceResolver
	ArtifactResolver     ArtifactResolver
	IDFactory            IDFactory
	Clock                Clock
}

type Service struct {
	backend              Backend
	candidatePipeline    CandidatePipeline
	embeddingModel       inference.EmbeddingModel
	reranker             Reranker
	rerankCandidateLimit int
	sourceResolver       SourceResolver
	artifactResolver     ArtifactResolver
	idFactory            IDFactory
	now                  Clock
}

func NewService(backend Backend, options ServiceOptions) (*Service, error) {
	if backend == nil || isNilInterface(backend) {
		return nil, fmt.Errorf("Memory backend must not be nil")
	}
	rerankLimit := options.RerankCandidateLimit
	if rerankLimit == 0 {
		rerankLimit = 30
	}
	if rerankLimit < 1 {
		return nil, &InvalidOperationError{Code: "search-limit"}
	}
	idFactory := options.IDFactory
	if idFactory == nil {
		idFactory = defaultID
	}
	now := options.Clock
	if now == nil {
		now = time.Now
	}
	return &Service{
		backend: backend, candidatePipeline: options.CandidatePipeline,
		embeddingModel: options.EmbeddingModel, reranker: options.Reranker,
		rerankCandidateLimit: rerankLimit, sourceResolver: options.SourceResolver,
		artifactResolver: options.ArtifactResolver, idFactory: idFactory, now: now,
	}, nil
}

func (s *Service) Get(ctx context.Context, value Memory) (Memory, error) {
	return s.canonicalMemory(ctx, value)
}

func (s *Service) Latest(ctx context.Context, value Memory) (Memory, error) {
	canonical, err := s.Get(ctx, value)
	if err != nil {
		return Memory{}, err
	}
	return s.backend.Latest(ctx, canonical.ID())
}

func (s *Service) Revisions(ctx context.Context, value Memory) ([]Memory, error) {
	canonical, err := s.Get(ctx, value)
	if err != nil {
		return nil, err
	}
	latest, err := s.backend.Latest(ctx, canonical.ID())
	if err != nil {
		return nil, err
	}
	result := make([]Memory, 0, latest.Revision())
	for revision := int64(1); revision <= latest.Revision(); revision++ {
		ref, _ := artifact.NewRef(Family, canonical.ID(), revision)
		item, err := s.backend.Get(ctx, ref)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}

func (s *Service) Head(ctx context.Context, artifactID string) (Memory, error) {
	return s.backend.Latest(ctx, artifactID)
}

func (s *Service) Revision(ctx context.Context, ref artifact.Ref) (Memory, error) {
	return s.backend.Get(ctx, ref)
}

func (s *Service) Remember(
	ctx context.Context,
	base *Memory,
	sources []source.Value,
	artifacts []artifact.Snapshot,
	entries []EntryInput,
	mode RememberMode,
) (*Memory, error) {
	plan, err := s.PlanRemember(ctx, base, sources, artifacts, entries, mode)
	if err != nil {
		return nil, err
	}
	return s.Apply(ctx, plan)
}

func (s *Service) PlanRemember(
	ctx context.Context,
	base *Memory,
	sources []source.Value,
	artifacts []artifact.Snapshot,
	entries []EntryInput,
	mode RememberMode,
) (WritePlan, error) {
	selectedMode, err := selectRememberMode(mode, len(entries) > 0, len(sources)+len(artifacts) > 0)
	if err != nil {
		return WritePlan{}, err
	}
	canonicalBase, err := s.canonicalBase(ctx, base)
	if err != nil {
		return WritePlan{}, err
	}
	evidence, err := s.canonicalOperationEvidence(ctx, sources, artifacts)
	if err != nil {
		return WritePlan{}, err
	}
	var currentEntries []EntryVersion
	if canonicalBase != nil {
		currentEntries, err = s.validatedEntries(ctx, *canonicalBase)
		if err != nil {
			return WritePlan{}, err
		}
	}
	candidates, err := s.candidates(ctx, selectedMode, entries, evidence, canonicalBase, currentEntries)
	if err != nil {
		return WritePlan{}, err
	}
	if len(candidates) == 0 {
		return NewWritePlan(canonicalBase, nil), nil
	}
	commit, err := s.prepareCommit(ctx, canonicalBase, candidates, evidence, currentEntries)
	if err != nil {
		return WritePlan{}, err
	}
	if commit == nil {
		return NewWritePlan(canonicalBase, nil), nil
	}
	result := commit.Memory()
	return NewWritePlan(&result, commit), nil
}

func (s *Service) Apply(ctx context.Context, plan WritePlan) (*Memory, error) {
	commit := plan.Commit()
	if commit == nil {
		return plan.Result(), nil
	}
	committed, err := s.backend.Commit(ctx, *commit)
	if err != nil {
		return nil, err
	}
	result := plan.Result()
	if result == nil || !reflect.DeepEqual(*result, committed) {
		return nil, &InvalidCitationError{Code: "memory-mismatch"}
	}
	return &committed, nil
}

func (s *Service) Forget(ctx context.Context, value Memory, entries []EntryVersion, reason *string) (Memory, error) {
	return s.setEntryState(ctx, value, entries, Inactive, reason)
}

func (s *Service) Reactivate(ctx context.Context, value Memory, entries []EntryVersion, reason *string) (Memory, error) {
	return s.setEntryState(ctx, value, entries, Active, reason)
}

func (s *Service) Organize(ctx context.Context, value Memory, mode OrganizeMode) (Memory, error) {
	if mode != OrganizeDefault && mode != OrganizeDedupe && mode != OrganizeNormalize {
		return Memory{}, &InvalidOperationError{Code: "organize-mode"}
	}
	base, err := s.canonicalBase(ctx, &value)
	if err != nil {
		return Memory{}, err
	}
	currentEntries, err := s.validatedEntries(ctx, *base)
	if err != nil {
		return Memory{}, err
	}
	manifest := manifestMap(base.Content().Manifest().Entries())
	current := entryMap(currentEntries)
	var changes []Change
	changedIDs := make(map[string]struct{})
	var newVersions []EntryVersion
	if mode == OrganizeDefault || mode == OrganizeDedupe {
		var dedupe []Change
		dedupe, changedIDs, err = s.deduplicateManifest(manifest, current)
		if err != nil {
			return Memory{}, err
		}
		changes = append(changes, dedupe...)
	}
	if mode == OrganizeDefault || mode == OrganizeNormalize {
		var normalized []Change
		normalized, newVersions, err = s.normalizeManifestEntries(*base, manifest, current, changedIDs)
		if err != nil {
			return Memory{}, err
		}
		changes = append(changes, normalized...)
	}
	if len(changes) == 0 {
		return *base, nil
	}
	return s.commitExistingTransition(ctx, *base, manifest, changes, current, newVersions)
}

func (s *Service) Changes(ctx context.Context, value Memory, sinceRevision *int64) ([]RevisionChanges, error) {
	target, err := s.canonicalMemory(ctx, value)
	if err != nil {
		return nil, err
	}
	if sinceRevision != nil {
		if *sinceRevision < 0 {
			return nil, &InvalidOperationError{Code: "since-negative"}
		}
		if *sinceRevision > target.Revision() {
			return nil, &InvalidOperationError{Code: "since-greater"}
		}
		if *sinceRevision == target.Revision() {
			return []RevisionChanges{}, nil
		}
		if *sinceRevision > 0 {
			ref, _ := artifact.NewRef(Family, target.ID(), *sinceRevision)
			if _, err := s.backend.Get(ctx, ref); err != nil {
				return nil, err
			}
		}
	}
	return s.backend.Changes(ctx, target.Ref(), sinceRevision)
}

func (s *Service) Search(
	ctx context.Context,
	query string,
	memories []Memory,
	limit int,
	mode SearchMode,
) (SearchResult, error) {
	if len(memories) == 0 {
		return SearchResult{}, &InvalidOperationError{Code: "search-memories"}
	}
	if limit < 1 {
		return SearchResult{}, &InvalidOperationError{Code: "search-limit"}
	}
	if mode != SearchFTS && mode != SearchVector && mode != SearchHybrid && mode != SearchAuto {
		return SearchResult{}, &InvalidOperationError{Code: "search-mode"}
	}
	selected := dedupeMemories(memories)
	refs := make([]artifact.Ref, len(selected))
	for index, value := range selected {
		refs[index] = value.Ref()
	}
	if err := s.validateSearchHeads(ctx, selected); err != nil {
		return SearchResult{}, err
	}
	capabilities := s.backend.Capabilities()
	selectedMode, err := s.selectSearchMode(ctx, mode, refs, capabilities)
	if err != nil {
		return SearchResult{}, err
	}
	normalizedQuery, err := NormalizeText(query)
	if err != nil {
		return SearchResult{}, err
	}
	var queryVector []float64
	var profile *EmbeddingProfile
	if selectedMode == SearchVector || selectedMode == SearchHybrid {
		profile = cloneEmbeddingProfile(capabilities.EmbeddingProfile)
		if profile == nil {
			return SearchResult{}, &CapabilityNotSupportedError{Capability: string(selectedMode)}
		}
		if profile.Distance != "l2" || profile.Normalization != "unit" {
			return SearchResult{}, &CapabilityNotSupportedError{
				Capability: string(selectedMode),
				Detail:     "cosine admission requires a unit-normalized L2 embedding profile",
			}
		}
		vectors, embedErr := s.embedTexts(ctx, []string{normalizedQuery}, *profile)
		if embedErr != nil {
			var unavailable *inference.UnavailableError
			var timeout *inference.TimeoutError
			if mode == SearchAuto && capabilities.FTS && (errors.As(embedErr, &unavailable) || errors.As(embedErr, &timeout)) {
				selectedMode = SearchFTS
				profile = nil
			} else if errors.As(embedErr, &unavailable) || errors.As(embedErr, &timeout) {
				return SearchResult{}, &CapabilityNotSupportedError{
					Capability: string(selectedMode), Detail: "embedding model is temporarily unavailable",
				}
			} else {
				return SearchResult{}, embedErr
			}
		} else {
			queryVector = vectors[0]
		}
	}
	coarseLimit := limit
	if s.reranker != nil {
		coarseLimit = max(limit, s.rerankCandidateLimit)
	}
	candidateLimit := 32
	if coarseLimit <= math.MaxInt/4 {
		candidateLimit = max(coarseLimit*4, candidateLimit)
	} else {
		candidateLimit = math.MaxInt
	}
	channels, err := s.backend.Search(ctx, SearchRequest{
		Query: normalizedQuery, AnalyzedQuery: AnalyzeText(normalizedQuery),
		Memories: refs, CandidateLimit: candidateLimit, Mode: selectedMode,
		QueryVector: queryVector, EmbeddingProfile: profile,
	})
	if err != nil {
		return SearchResult{}, err
	}
	fts := AdmitFTSCandidates(normalizedQuery, channels.FTS)
	vector := AdmitVectorCandidates(channels.Vector)
	if selectedMode == SearchFTS {
		vector = nil
	}
	if selectedMode == SearchVector {
		fts = nil
	}
	hits, err := FuseRankings(fts, vector, coarseLimit)
	if err != nil {
		return SearchResult{}, err
	}
	return s.rerankedSearchResult(ctx, selectedMode, normalizedQuery, hits, limit)
}

func (s *Service) Expand(ctx context.Context, hits []Hit) ([]EntryVersion, error) {
	versions, err := s.backend.Expand(ctx, cloneHits(hits))
	if err != nil {
		return nil, err
	}
	if len(versions) != len(hits) {
		return nil, &InvalidCitationError{Code: "expand-count"}
	}
	for index, hit := range hits {
		if err := s.validateAnchor(ctx, hit.MemoryRef, hit.EntryID, hit.EntryVersionID, versions[index]); err != nil {
			return nil, err
		}
	}
	return cloneEntryVersions(versions), nil
}

func (s *Service) Entries(ctx context.Context, value Memory) ([]EntryVersion, error) {
	canonical, err := s.canonicalMemory(ctx, value)
	if err != nil {
		return nil, err
	}
	return s.validatedEntries(ctx, canonical)
}

func (s *Service) ValidateCitation(ctx context.Context, citation Citation) (EntryVersion, error) {
	hit := Hit{
		MemoryRef: citation.MemoryRef, EntryID: citation.EntryID,
		EntryVersionID: citation.EntryVersionID,
	}
	values, err := s.Expand(ctx, []Hit{hit})
	if err != nil {
		return EntryVersion{}, err
	}
	return values[0], nil
}

type operationEvidence struct {
	sources   []source.Value
	artifacts []artifact.Snapshot
}

type entryMaterial struct {
	kind         string
	text         string
	sources      []source.Ref
	artifacts    []artifact.Ref
	contentBytes []byte
	contentHash  string
}

func selectRememberMode(mode RememberMode, hasEntries, hasEvidence bool) (RememberMode, error) {
	switch mode {
	case RememberAppend:
		if !hasEntries {
			return "", &InvalidOperationError{Code: "append-entries"}
		}
		return RememberAppend, nil
	case RememberExtract:
		if hasEntries || !hasEvidence {
			return "", &InvalidOperationError{Code: "extract-evidence"}
		}
		return RememberExtract, nil
	case RememberAuto:
		if hasEntries {
			return RememberAppend, nil
		}
		if hasEvidence {
			return RememberExtract, nil
		}
		return "", &InvalidOperationError{Code: "no-work"}
	default:
		return "", &InvalidCandidateError{Code: "remember-mode", Detail: string(mode)}
	}
}

func (s *Service) canonicalBase(ctx context.Context, value *Memory) (*Memory, error) {
	if value == nil {
		return nil, nil
	}
	exact, err := s.backend.Get(ctx, value.Ref())
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(exact, *value) {
		return nil, &InvalidCitationError{Code: "base-mismatch"}
	}
	latest, err := s.backend.Latest(ctx, value.ID())
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(latest, exact) {
		return nil, &artifact.RevisionConflictError{Requested: value.Ref(), Current: latest.Ref()}
	}
	return cloneMemory(&exact), nil
}

func (s *Service) canonicalMemory(ctx context.Context, value Memory) (Memory, error) {
	exact, err := s.backend.Get(ctx, value.Ref())
	if err != nil {
		return Memory{}, err
	}
	if !reflect.DeepEqual(exact, value) {
		return Memory{}, &InvalidCitationError{Code: "memory-mismatch"}
	}
	return exact, nil
}

func (s *Service) canonicalOperationEvidence(
	ctx context.Context,
	sources []source.Value,
	artifacts []artifact.Snapshot,
) (operationEvidence, error) {
	result := operationEvidence{}
	for _, value := range sources {
		if s.sourceResolver == nil || isNilInterface(s.sourceResolver) {
			return operationEvidence{}, &InvalidEvidenceError{Code: "source-resolver"}
		}
		canonical, err := s.sourceResolver.Get(ctx, value)
		if err != nil {
			return operationEvidence{}, err
		}
		appendUniqueSource(&result.sources, canonical)
	}
	for _, value := range artifacts {
		if s.artifactResolver == nil || isNilInterface(s.artifactResolver) {
			return operationEvidence{}, &InvalidEvidenceError{Code: "artifact-resolver"}
		}
		canonical, err := s.artifactResolver.Get(ctx, value)
		if err != nil {
			return operationEvidence{}, err
		}
		appendUniqueArtifact(&result.artifacts, canonical)
	}
	return result, nil
}

func (s *Service) validatedEntries(ctx context.Context, value Memory) ([]EntryVersion, error) {
	versions, err := s.backend.Entries(ctx, value.Ref())
	if err != nil {
		return nil, err
	}
	byID := make(map[string]EntryVersion, len(versions))
	for _, version := range versions {
		if _, exists := byID[version.EntryVersionID]; exists {
			return nil, &InvalidCitationError{Code: "duplicate-versions"}
		}
		byID[version.EntryVersionID] = version.Clone()
	}
	manifest := value.Content().Manifest().Entries()
	ordered := make([]EntryVersion, 0, len(manifest))
	for _, item := range manifest {
		version, exists := byID[item.EntryVersionID()]
		if !exists {
			return nil, &InvalidCitationError{Code: "missing-version"}
		}
		if version.MemoryArtifactID != value.ID() || version.EntryID != item.EntryID() {
			return nil, &InvalidCitationError{Code: "cross-identity"}
		}
		material, err := s.materialFromVersion(version)
		if err != nil {
			return nil, err
		}
		if material.contentHash != item.EntryContentHash() {
			return nil, &InvalidCitationError{Code: "hash-mismatch"}
		}
		ordered = append(ordered, version.Clone())
	}
	return ordered, nil
}

func (s *Service) candidates(
	ctx context.Context,
	mode RememberMode,
	entries []EntryInput,
	evidence operationEvidence,
	base *Memory,
	currentEntries []EntryVersion,
) ([]EntryInput, error) {
	if mode == RememberAppend {
		return cloneEntryInputs(entries), nil
	}
	if s.candidatePipeline == nil || isNilInterface(s.candidatePipeline) {
		return nil, &CapabilityNotSupportedError{Capability: "extract"}
	}
	activeVersionIDs := make(map[string]struct{})
	if base != nil {
		for _, item := range base.Content().Manifest().Entries() {
			if item.State() == Active {
				activeVersionIDs[item.EntryVersionID()] = struct{}{}
			}
		}
	}
	bounded := make([]EntryVersion, 0, len(currentEntries))
	for _, entry := range currentEntries {
		if _, active := activeVersionIDs[entry.EntryVersionID]; active {
			bounded = append(bounded, entry.Clone())
		}
	}
	request, err := NewCandidateRequest(evidence.sources, evidence.artifacts, bounded)
	if err != nil {
		return nil, err
	}
	return s.candidatePipeline.Extract(ctx, request)
}

func (s *Service) prepareCommit(
	ctx context.Context,
	base *Memory,
	candidates []EntryInput,
	evidence operationEvidence,
	currentEntries []EntryVersion,
) (*Commit, error) {
	memoryID := ""
	nextRevision := int64(1)
	manifest := make(map[string]ManifestEntry)
	if base == nil {
		var err error
		memoryID, err = s.newID("memory")
		if err != nil {
			return nil, err
		}
	} else {
		memoryID = base.ID()
		nextRevision = base.Revision() + 1
		manifest = manifestMap(base.Content().Manifest().Entries())
	}
	current := entryMap(currentEntries)
	newVersions := make([]EntryVersion, 0)
	changes := make([]Change, 0)
	targeted := make(map[string]struct{})
	newContent := make(map[string]struct{})
	for _, version := range currentEntries {
		if item, exists := manifest[version.EntryID]; exists && item.State() == Active {
			material, err := s.materialFromVersion(version)
			if err != nil {
				return nil, err
			}
			newContent[string(material.contentBytes)] = struct{}{}
		}
	}

	for _, candidate := range candidates {
		if candidate.entry == nil {
			material, err := s.materialFromCandidate(ctx, candidate, evidence.sources, evidence.artifacts, nil)
			if err != nil {
				return nil, err
			}
			if _, duplicate := newContent[string(material.contentBytes)]; duplicate {
				continue
			}
			newContent[string(material.contentBytes)] = struct{}{}
			entryID, err := s.newID("entry")
			if err != nil {
				return nil, err
			}
			if _, collision := manifest[entryID]; collision {
				return nil, &InvalidOperationError{Code: "id-collision"}
			}
			version, err := s.newEntryVersion(memoryID, entryID, nil, material, nextRevision)
			if err != nil {
				return nil, err
			}
			item, _ := NewManifestEntry(entryID, version.EntryVersionID, version.EntryContentHash, Active)
			manifest[entryID] = item
			current[entryID] = version
			newVersions = append(newVersions, version)
			change, err := NewChange(Add, entryID, nil, &version.EntryVersionID, candidate.reason)
			if err != nil {
				return nil, err
			}
			changes = append(changes, change)
			continue
		}

		entryID, previous, err := claimRevisionTarget(candidate, current, targeted)
		if err != nil {
			return nil, err
		}
		item, exists := manifest[entryID]
		if !exists {
			return nil, &EntryNotFoundError{EntryID: entryID}
		}
		if item.State() == Inactive {
			return nil, &EntryInactiveError{EntryID: entryID}
		}
		material, err := s.materialFromCandidate(ctx, candidate, evidence.sources, evidence.artifacts, &previous)
		if err != nil {
			return nil, err
		}
		previousMaterial, err := s.materialFromVersion(previous)
		if err != nil {
			return nil, err
		}
		if material.contentHash == previousMaterial.contentHash && bytesEqual(material.contentBytes, previousMaterial.contentBytes) {
			continue
		}
		version, err := s.newEntryVersion(memoryID, entryID, &previous, material, nextRevision)
		if err != nil {
			return nil, err
		}
		manifestItem, _ := NewManifestEntry(entryID, version.EntryVersionID, version.EntryContentHash, Active)
		manifest[entryID] = manifestItem
		current[entryID] = version
		newVersions = append(newVersions, version)
		change, err := NewChange(Revise, entryID, &previous.EntryVersionID, &version.EntryVersionID, candidate.reason)
		if err != nil {
			return nil, err
		}
		changes = append(changes, change)
	}
	if len(changes) == 0 {
		return nil, nil
	}
	manifestEntries := sortedManifest(manifest)
	sortedChanges := sortedChanges(changes)
	content := NewContent(NewManifest(manifestEntries), sortedChanges)
	sourceRefs, err := s.sourceRefs(evidence.sources)
	if err != nil {
		return nil, err
	}
	artifactRefs := make([]artifact.Ref, len(evidence.artifacts))
	for index, value := range evidence.artifacts {
		artifactRefs[index] = value.Ref()
	}
	draft, err := NewDraft(content, sourceRefs, artifactRefs)
	if err != nil {
		return nil, err
	}
	next, err := artifact.New(memoryID, nextRevision, draft)
	if err != nil {
		return nil, err
	}
	changedIDs := make(map[string]struct{}, len(newVersions))
	for _, version := range newVersions {
		changedIDs[version.EntryVersionID] = struct{}{}
	}
	projections, err := s.prepareProjections(ctx, base, manifestEntries, current, changedIDs)
	if err != nil {
		return nil, err
	}
	contentHash, err := ContentHash(content)
	if err != nil {
		return nil, err
	}
	commit := NewCommit(base, next, contentHash, newVersions, projections)
	return &commit, nil
}

func claimRevisionTarget(
	candidate EntryInput,
	current map[string]EntryVersion,
	targeted map[string]struct{},
) (string, EntryVersion, error) {
	if candidate.entry == nil {
		return "", EntryVersion{}, &InvalidCitationError{Code: "entry-missing"}
	}
	entryID, err := ValidateIdentifier(candidate.entry.EntryID)
	if err != nil {
		return "", EntryVersion{}, err
	}
	if _, duplicate := targeted[entryID]; duplicate {
		return "", EntryVersion{}, &InvalidOperationError{Code: "duplicate-target"}
	}
	targeted[entryID] = struct{}{}
	previous, exists := current[entryID]
	if !exists {
		return "", EntryVersion{}, &EntryNotFoundError{EntryID: entryID}
	}
	if !reflect.DeepEqual(candidate.entry.Clone(), previous) {
		return "", EntryVersion{}, &InvalidCitationError{Code: "entry-mismatch"}
	}
	return entryID, previous.Clone(), nil
}

func (s *Service) materialFromCandidate(
	ctx context.Context,
	candidate EntryInput,
	allowedSources []source.Value,
	allowedArtifacts []artifact.Snapshot,
	previous *EntryVersion,
) (entryMaterial, error) {
	canonicalSources, err := s.canonicalCandidateSources(ctx, candidate.sources, allowedSources)
	if err != nil {
		return entryMaterial{}, err
	}
	candidateSourceRefs, err := s.sourceRefs(canonicalSources)
	if err != nil {
		return entryMaterial{}, err
	}
	var previousArtifacts []artifact.Ref
	if previous != nil {
		previousArtifacts = previous.Artifacts
	}
	candidateArtifactRefs, err := s.canonicalCandidateArtifacts(
		ctx, candidate.artifacts, allowedArtifacts, previousArtifacts,
	)
	if err != nil {
		return entryMaterial{}, err
	}
	selectedSources := candidateSourceRefs
	selectedArtifacts := candidateArtifactRefs
	if previous != nil {
		selectedSources = append(slices.Clone(previous.Sources), selectedSources...)
		selectedArtifacts = append(slices.Clone(previous.Artifacts), selectedArtifacts...)
	}
	material, err := newEntryMaterial(candidate.kind, candidate.text, selectedSources, selectedArtifacts)
	if err != nil {
		return entryMaterial{}, &InvalidCandidateError{Code: "canonical", Detail: err.Error()}
	}
	return material, nil
}

func (s *Service) canonicalCandidateSources(
	ctx context.Context,
	values []source.Value,
	allowed []source.Value,
) ([]source.Value, error) {
	result := make([]source.Value, 0, len(values))
	for _, value := range values {
		canonical := value
		var err error
		if s.sourceResolver != nil && !isNilInterface(s.sourceResolver) {
			canonical, err = s.sourceResolver.Get(ctx, value)
			if err != nil {
				return nil, err
			}
		}
		match := matchingSource(canonical, allowed)
		if match == nil {
			return nil, &InvalidEvidenceError{Code: "source-outside"}
		}
		appendUniqueSource(&result, match)
	}
	return result, nil
}

func (s *Service) canonicalCandidateArtifacts(
	ctx context.Context,
	values []artifact.Snapshot,
	allowed []artifact.Snapshot,
	previous []artifact.Ref,
) ([]artifact.Ref, error) {
	allowedRefs := make(map[artifact.Ref]struct{}, len(allowed)+len(previous))
	for _, value := range allowed {
		allowedRefs[value.Ref()] = struct{}{}
	}
	for _, ref := range previous {
		allowedRefs[ref] = struct{}{}
	}
	result := make([]artifact.Ref, 0, len(values))
	seen := make(map[artifact.Ref]struct{})
	for _, value := range values {
		canonical := value
		var err error
		if s.artifactResolver != nil && !isNilInterface(s.artifactResolver) {
			canonical, err = s.artifactResolver.Get(ctx, value)
			if err != nil {
				return nil, err
			}
		}
		ref := canonical.Ref()
		if _, exists := allowedRefs[ref]; !exists {
			return nil, &InvalidEvidenceError{Code: "artifact-outside"}
		}
		if _, exists := seen[ref]; !exists {
			seen[ref] = struct{}{}
			result = append(result, ref)
		}
	}
	return result, nil
}

func (s *Service) materialFromVersion(value EntryVersion) (entryMaterial, error) {
	return newEntryMaterial(value.Kind, value.Text, value.Sources, value.Artifacts)
}

func newEntryMaterial(kind, text string, sources []source.Ref, artifacts []artifact.Ref) (entryMaterial, error) {
	normalizedKind, err := NormalizeKind(kind)
	if err != nil {
		return entryMaterial{}, err
	}
	normalizedText, err := NormalizeText(text)
	if err != nil {
		return entryMaterial{}, err
	}
	canonicalSources, err := canonicalSourceRefs(sources)
	if err != nil {
		return entryMaterial{}, err
	}
	canonicalArtifacts, err := canonicalArtifactRefs(artifacts)
	if err != nil {
		return entryMaterial{}, err
	}
	contentBytes, err := EntryContentBytes(normalizedKind, normalizedText, canonicalSources, canonicalArtifacts)
	if err != nil {
		return entryMaterial{}, err
	}
	contentHash, err := EntryContentHash(normalizedKind, normalizedText, canonicalSources, canonicalArtifacts)
	if err != nil {
		return entryMaterial{}, err
	}
	return entryMaterial{
		kind: normalizedKind, text: normalizedText,
		sources: canonicalSources, artifacts: canonicalArtifacts,
		contentBytes: contentBytes, contentHash: contentHash,
	}, nil
}

func (s *Service) sourceRefs(values []source.Value) ([]source.Ref, error) {
	if len(values) == 0 {
		return []source.Ref{}, nil
	}
	if s.sourceResolver == nil || isNilInterface(s.sourceResolver) {
		return nil, &InvalidEvidenceError{Code: "source-adapter"}
	}
	refs := make([]source.Ref, 0, len(values))
	for _, value := range values {
		ref, err := s.sourceResolver.Ref(value)
		if err != nil {
			return nil, &InvalidEvidenceError{Code: "source-adapter"}
		}
		refs = append(refs, ref)
	}
	return canonicalSourceRefs(refs)
}

func (s *Service) newEntryVersion(
	memoryID, entryID string,
	previous *EntryVersion,
	material entryMaterial,
	createdInRevision int64,
) (EntryVersion, error) {
	versionID, err := s.newID("version")
	if err != nil {
		return EntryVersion{}, err
	}
	version := int64(1)
	var previousID *string
	if previous != nil {
		version = previous.Version + 1
		previousID = &previous.EntryVersionID
	}
	return EntryVersion{
		MemoryArtifactID: memoryID, EntryID: entryID, EntryVersionID: versionID,
		Version: version, PreviousVersionID: cloneString(previousID),
		Kind: material.kind, Text: material.text, EntryContentHash: material.contentHash,
		CreatedInRevision: createdInRevision, Sources: slices.Clone(material.sources),
		Artifacts: slices.Clone(material.artifacts),
	}, nil
}

func (s *Service) newID(kind string) (string, error) {
	value, err := s.idFactory(kind)
	if err != nil {
		return "", err
	}
	if _, err := ValidateIdentifier(value); err != nil {
		return "", err
	}
	return value, nil
}

func (s *Service) prepareProjections(
	ctx context.Context,
	base *Memory,
	manifest []ManifestEntry,
	versions map[string]EntryVersion,
	changedVersionIDs map[string]struct{},
) ([]Projection, error) {
	previous := make(map[string]Projection)
	if base != nil {
		values, err := s.backend.Projections(ctx, base.Ref())
		if err != nil {
			return nil, err
		}
		for _, projection := range values {
			previous[projection.EntryVersion().EntryVersionID] = projection
		}
	}
	prepared := make([]Projection, 0)
	embedIndices := make([]int, 0)
	for _, item := range manifest {
		if item.State() != Active {
			continue
		}
		version, exists := versions[item.EntryID()]
		if !exists || version.EntryVersionID != item.EntryVersionID() {
			return nil, &InvalidCitationError{Code: "projection-version"}
		}
		searchable := AnalyzeText(version.Text)
		if old, exists := previous[version.EntryVersionID]; exists {
			oldVersion := old.EntryVersion()
			_, changed := changedVersionIDs[version.EntryVersionID]
			if !changed && oldVersion.EntryVersionID == version.EntryVersionID &&
				oldVersion.EntryContentHash == version.EntryContentHash && old.SearchableText() == searchable {
				reused, _ := NewProjection(version, searchable, old.Embedding(), old.EmbeddingContentHash())
				prepared = append(prepared, reused)
				continue
			}
		}
		projection, _ := NewProjection(version, searchable, nil, nil)
		prepared = append(prepared, projection)
		embedIndices = append(embedIndices, len(prepared)-1)
	}
	return s.attachEmbeddings(ctx, prepared, embedIndices)
}

func (s *Service) attachEmbeddings(ctx context.Context, projections []Projection, indices []int) ([]Projection, error) {
	if len(projections) == 0 || len(indices) == 0 || s.embeddingModel == nil || isNilInterface(s.embeddingModel) {
		return cloneProjections(projections), nil
	}
	capabilities := s.backend.Capabilities()
	profile := capabilities.EmbeddingProfile
	if !capabilities.Vector || profile == nil || !embeddingProfileMatches(s.embeddingModel.Profile(), *profile) {
		return cloneProjections(projections), nil
	}
	texts := make([]string, len(indices))
	for index, projectionIndex := range indices {
		texts[index] = projections[projectionIndex].EntryVersion().Text
	}
	vectors, err := s.embedTexts(ctx, texts, *profile)
	if err != nil {
		var unavailable *inference.UnavailableError
		var timeout *inference.TimeoutError
		if errors.As(err, &unavailable) || errors.As(err, &timeout) {
			return cloneProjections(projections), nil
		}
		return nil, err
	}
	updated := cloneProjections(projections)
	for vectorIndex, projectionIndex := range indices {
		projection := updated[projectionIndex]
		entry := projection.EntryVersion()
		hash, err := EmbeddingContentHash(*profile, entry.EntryContentHash)
		if err != nil {
			return nil, err
		}
		updated[projectionIndex], _ = NewProjection(entry, projection.SearchableText(), vectors[vectorIndex], &hash)
	}
	return updated, nil
}

func (s *Service) embedTexts(ctx context.Context, texts []string, profile EmbeddingProfile) ([][]float64, error) {
	if s.embeddingModel == nil || isNilInterface(s.embeddingModel) || !embeddingProfileMatches(s.embeddingModel.Profile(), profile) {
		return nil, &CapabilityNotSupportedError{Capability: "embedding-profile"}
	}
	result, err := s.embeddingModel.Embed(ctx, slices.Clone(texts))
	if err != nil {
		return nil, err
	}
	if len(result.Vectors) != len(texts) {
		return nil, &InvalidEmbeddingError{Code: "count"}
	}
	vectors := make([][]float64, len(result.Vectors))
	for index, vector := range result.Vectors {
		canonical, err := CanonicalEmbedding(vector, profile.Dimension, profile.Normalization)
		if err != nil {
			return nil, err
		}
		vectors[index] = canonical
	}
	return vectors, nil
}

func (s *Service) setEntryState(
	ctx context.Context,
	value Memory,
	entries []EntryVersion,
	target EntryState,
	reason *string,
) (Memory, error) {
	base, err := s.canonicalBase(ctx, &value)
	if err != nil {
		return Memory{}, err
	}
	currentEntries, err := s.validatedEntries(ctx, *base)
	if err != nil {
		return Memory{}, err
	}
	manifest := manifestMap(base.Content().Manifest().Entries())
	current := entryMap(currentEntries)
	normalizedReason, err := NormalizeReason(reason)
	if err != nil {
		return Memory{}, err
	}
	changes := make([]Change, 0)
	seen := make(map[string]struct{})
	for _, requested := range entries {
		identity := requested.EntryID + "\x00" + requested.EntryVersionID
		if _, duplicate := seen[identity]; duplicate {
			continue
		}
		seen[identity] = struct{}{}
		entryID, err := ValidateIdentifier(requested.EntryID)
		if err != nil {
			return Memory{}, err
		}
		item, exists := manifest[entryID]
		if !exists {
			return Memory{}, &EntryNotFoundError{EntryID: entryID}
		}
		currentEntry, exists := current[entryID]
		if !exists {
			return Memory{}, &EntryNotFoundError{EntryID: entryID}
		}
		if !reflect.DeepEqual(requested.Clone(), currentEntry) {
			return Memory{}, &InvalidCitationError{Code: "entry-mismatch"}
		}
		if item.State() == target {
			continue
		}
		updated, _ := NewManifestEntry(item.EntryID(), item.EntryVersionID(), item.EntryContentHash(), target)
		manifest[entryID] = updated
		var op ChangeOp
		var from, to *string
		if target == Active {
			op = Reactivate
			to = ptrString(item.EntryVersionID())
		} else {
			op = Deactivate
			from = ptrString(item.EntryVersionID())
		}
		change, err := NewChange(op, entryID, from, to, normalizedReason)
		if err != nil {
			return Memory{}, err
		}
		changes = append(changes, change)
	}
	if len(changes) == 0 {
		return *base, nil
	}
	return s.commitExistingTransition(ctx, *base, manifest, changes, current, nil)
}

func (s *Service) commitExistingTransition(
	ctx context.Context,
	base Memory,
	manifest map[string]ManifestEntry,
	changes []Change,
	current map[string]EntryVersion,
	newVersions []EntryVersion,
) (Memory, error) {
	manifestEntries := sortedManifest(manifest)
	changes = sortedChanges(changes)
	content := NewContent(NewManifest(manifestEntries), changes)
	draft, err := NewDraft(content, nil, nil)
	if err != nil {
		return Memory{}, err
	}
	next, err := artifact.New(base.ID(), base.Revision()+1, draft)
	if err != nil {
		return Memory{}, err
	}
	changedIDs := make(map[string]struct{}, len(newVersions))
	for _, version := range newVersions {
		changedIDs[version.EntryVersionID] = struct{}{}
	}
	projections, err := s.prepareProjections(ctx, &base, manifestEntries, current, changedIDs)
	if err != nil {
		return Memory{}, err
	}
	hash, err := ContentHash(content)
	if err != nil {
		return Memory{}, err
	}
	return s.backend.Commit(ctx, NewCommit(&base, next, hash, newVersions, projections))
}

func (s *Service) deduplicateManifest(
	manifest map[string]ManifestEntry,
	current map[string]EntryVersion,
) ([]Change, map[string]struct{}, error) {
	groups := make(map[string][]string)
	for _, item := range manifest {
		if item.State() != Active {
			continue
		}
		version, exists := current[item.EntryID()]
		if !exists {
			return nil, nil, &InvalidCitationError{Code: "missing-version"}
		}
		material, err := s.materialFromVersion(version)
		if err != nil {
			return nil, nil, err
		}
		groups[string(material.contentBytes)] = append(groups[string(material.contentBytes)], item.EntryID())
	}
	changes := make([]Change, 0)
	changed := make(map[string]struct{})
	for _, ids := range groups {
		slices.Sort(ids)
		for _, entryID := range ids[1:] {
			item := manifest[entryID]
			updated, _ := NewManifestEntry(item.EntryID(), item.EntryVersionID(), item.EntryContentHash(), Inactive)
			manifest[entryID] = updated
			reason := "dedupe"
			from := item.EntryVersionID()
			change, _ := NewChange(Deactivate, entryID, &from, nil, &reason)
			changes = append(changes, change)
			changed[entryID] = struct{}{}
		}
	}
	return changes, changed, nil
}

func (s *Service) normalizeManifestEntries(
	base Memory,
	manifest map[string]ManifestEntry,
	current map[string]EntryVersion,
	skip map[string]struct{},
) ([]Change, []EntryVersion, error) {
	changes := make([]Change, 0)
	versions := make([]EntryVersion, 0)
	entryIDs := make([]string, 0, len(current))
	for entryID := range current {
		entryIDs = append(entryIDs, entryID)
	}
	slices.Sort(entryIDs)
	for _, entryID := range entryIDs {
		if _, skipped := skip[entryID]; skipped {
			continue
		}
		previous := current[entryID]
		material, err := s.materialFromVersion(previous)
		if err != nil {
			return nil, nil, err
		}
		if isCanonicalVersion(previous, material) {
			continue
		}
		version, err := s.newEntryVersion(base.ID(), entryID, &previous, material, base.Revision()+1)
		if err != nil {
			return nil, nil, err
		}
		item := manifest[entryID]
		manifest[entryID], _ = NewManifestEntry(entryID, version.EntryVersionID, version.EntryContentHash, item.State())
		current[entryID] = version
		versions = append(versions, version)
		reason := "normalize"
		change, _ := NewChange(Revise, entryID, &previous.EntryVersionID, &version.EntryVersionID, &reason)
		changes = append(changes, change)
	}
	return changes, versions, nil
}

func (s *Service) validateAnchor(
	ctx context.Context,
	memoryRef artifact.Ref,
	entryID, versionID string,
	version EntryVersion,
) error {
	value, err := s.backend.Get(ctx, memoryRef)
	if err != nil {
		return err
	}
	var item *ManifestEntry
	for _, candidate := range value.Content().Manifest().Entries() {
		if candidate.EntryID() == entryID {
			copy := candidate
			item = &copy
			break
		}
	}
	if item == nil || item.EntryVersionID() != versionID ||
		version.MemoryArtifactID != memoryRef.ID() || version.EntryID != entryID || version.EntryVersionID != versionID {
		return &InvalidCitationError{Code: "expand-anchor"}
	}
	material, err := s.materialFromVersion(version)
	if err != nil {
		return err
	}
	if material.contentHash != item.EntryContentHash() || version.EntryContentHash != item.EntryContentHash() {
		return &InvalidCitationError{Code: "hash-mismatch"}
	}
	return nil
}

func (s *Service) validateSearchHeads(ctx context.Context, memories []Memory) error {
	for _, value := range memories {
		exact, err := s.backend.Get(ctx, value.Ref())
		if err != nil {
			return err
		}
		latest, err := s.backend.Latest(ctx, value.ID())
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(exact, value) || exact.Ref() != latest.Ref() {
			return &CapabilityNotSupportedError{Capability: "head"}
		}
	}
	return nil
}

func (s *Service) selectSearchMode(
	ctx context.Context,
	requested SearchMode,
	memories []artifact.Ref,
	capabilities Capabilities,
) (SearchMode, error) {
	if requested == SearchFTS {
		if !capabilities.FTS {
			return "", &CapabilityNotSupportedError{Capability: "fts"}
		}
		return SearchFTS, nil
	}
	profile := capabilities.EmbeddingProfile
	profileMatches := profile != nil && s.embeddingModel != nil && !isNilInterface(s.embeddingModel) &&
		embeddingProfileMatches(s.embeddingModel.Profile(), *profile)
	vectorComplete := false
	var err error
	if capabilities.Vector && profileMatches {
		vectorComplete, err = s.backend.VectorComplete(ctx, memories, *profile)
		if err != nil {
			return "", err
		}
	}
	vectorAvailable := capabilities.Vector && profileMatches && vectorComplete
	hybridAvailable := capabilities.Hybrid && vectorAvailable
	switch requested {
	case SearchVector:
		if !vectorAvailable {
			return "", &CapabilityNotSupportedError{Capability: "vector"}
		}
		return SearchVector, nil
	case SearchHybrid:
		if !hybridAvailable {
			return "", &CapabilityNotSupportedError{Capability: "hybrid"}
		}
		return SearchHybrid, nil
	case SearchAuto:
		if hybridAvailable {
			return SearchHybrid, nil
		}
		if capabilities.FTS {
			return SearchFTS, nil
		}
		return "", &CapabilityNotSupportedError{Capability: "fts"}
	default:
		return "", &InvalidOperationError{Code: "search-mode"}
	}
}

func (s *Service) rerankedSearchResult(
	ctx context.Context,
	mode SearchMode,
	query string,
	hits []Hit,
	limit int,
) (SearchResult, error) {
	if s.reranker == nil || isNilInterface(s.reranker) || len(hits) == 0 {
		return SearchResult{Mode: mode, Hits: cloneHits(hits[:min(limit, len(hits))])}, nil
	}
	started := s.now()
	decision, err := s.reranker.Rerank(ctx, query, cloneHits(hits), min(limit, len(hits)))
	if err != nil {
		return SearchResult{}, err
	}
	latency := s.now().Sub(started).Seconds() * 1000
	if latency < 0 || math.IsNaN(latency) || math.IsInf(latency, 0) {
		latency = 0
	}
	ranks := decision.SelectedRanks()
	selected := make([]Hit, len(ranks))
	for index, rank := range ranks {
		if rank < 1 || rank > len(hits) {
			return SearchResult{}, inference.NewInvalidOutputError("memory-rerank", "normalized rank is outside the candidate pool")
		}
		selected[index] = hits[rank-1].Clone()
	}
	return SearchResult{
		Mode: mode, Hits: selected,
		Rerank: &RerankTrace{
			PolicyID: s.reranker.PolicyID(), CandidateHits: cloneHits(hits), SelectedRanks: ranks,
			DiscardedRankCount: decision.DiscardedRankCount(), UsedFallback: decision.UsedFallback(),
			LatencyMS: latency, Usage: decision.Usage(),
		},
	}, nil
}

func canonicalSourceRefs(values []source.Ref) ([]source.Ref, error) {
	type item struct {
		key string
		ref source.Ref
	}
	byKey := make(map[string]source.Ref, len(values))
	for _, ref := range values {
		if _, err := source.NewRef(ref.Type(), ref.ID()); err != nil {
			return nil, err
		}
		encoded, err := CanonicalJSON(map[string]any{"source_type": ref.Type(), "source_id": ref.ID()})
		if err != nil {
			return nil, err
		}
		byKey[string(encoded)] = ref
	}
	items := make([]item, 0, len(byKey))
	for key, ref := range byKey {
		items = append(items, item{key, ref})
	}
	slices.SortFunc(items, func(left, right item) int { return strings.Compare(left.key, right.key) })
	result := make([]source.Ref, len(items))
	for index, item := range items {
		result[index] = item.ref
	}
	return result, nil
}

func canonicalArtifactRefs(values []artifact.Ref) ([]artifact.Ref, error) {
	type item struct {
		key string
		ref artifact.Ref
	}
	byKey := make(map[string]artifact.Ref, len(values))
	for _, ref := range values {
		if err := ref.Validate(); err != nil {
			return nil, err
		}
		encoded, err := CanonicalJSON(map[string]any{
			"family": ref.Family(), "artifact_id": ref.ID(), "revision": ref.Revision(),
		})
		if err != nil {
			return nil, err
		}
		byKey[string(encoded)] = ref
	}
	items := make([]item, 0, len(byKey))
	for key, ref := range byKey {
		items = append(items, item{key, ref})
	}
	slices.SortFunc(items, func(left, right item) int { return strings.Compare(left.key, right.key) })
	result := make([]artifact.Ref, len(items))
	for index, item := range items {
		result[index] = item.ref
	}
	return result, nil
}

func sortedManifest(values map[string]ManifestEntry) []ManifestEntry {
	result := make([]ManifestEntry, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	slices.SortFunc(result, func(left, right ManifestEntry) int {
		return strings.Compare(left.EntryID(), right.EntryID())
	})
	return result
}

func sortedChanges(values []Change) []Change {
	result := make([]Change, len(values))
	for index, value := range values {
		result[index] = value.Clone()
	}
	slices.SortFunc(result, func(left, right Change) int {
		return strings.Compare(left.EntryID(), right.EntryID())
	})
	return result
}

func manifestMap(values []ManifestEntry) map[string]ManifestEntry {
	result := make(map[string]ManifestEntry, len(values))
	for _, value := range values {
		result[value.EntryID()] = value
	}
	return result
}

func entryMap(values []EntryVersion) map[string]EntryVersion {
	result := make(map[string]EntryVersion, len(values))
	for _, value := range values {
		result[value.EntryID] = value.Clone()
	}
	return result
}

func cloneEntryInputs(values []EntryInput) []EntryInput {
	result := make([]EntryInput, len(values))
	for index, value := range values {
		result[index] = NewEntryInput(value.entry, value.kind, value.text, value.sources, value.artifacts, value.reason)
	}
	return result
}

func cloneHits(values []Hit) []Hit {
	result := make([]Hit, len(values))
	for index, value := range values {
		result[index] = value.Clone()
	}
	return result
}

func dedupeMemories(values []Memory) []Memory {
	type key struct {
		family string
		id     string
		rev    int64
	}
	seen := make(map[key]struct{}, len(values))
	result := make([]Memory, 0, len(values))
	for _, value := range values {
		ref := value.Ref()
		identity := key{ref.Family(), ref.ID(), ref.Revision()}
		if _, exists := seen[identity]; exists {
			continue
		}
		seen[identity] = struct{}{}
		result = append(result, cloneMemoryValue(value))
	}
	return result
}

func matchingSource(value source.Value, values []source.Value) source.Value {
	for _, candidate := range values {
		if reflect.TypeOf(candidate) == reflect.TypeOf(value) && reflect.DeepEqual(candidate, value) {
			return candidate
		}
	}
	return nil
}

func appendUniqueSource(values *[]source.Value, value source.Value) {
	if matchingSource(value, *values) == nil {
		*values = append(*values, value)
	}
}

func appendUniqueArtifact(values *[]artifact.Snapshot, value artifact.Snapshot) {
	for _, candidate := range *values {
		if candidate.Ref() == value.Ref() && reflect.DeepEqual(candidate, value) {
			return
		}
	}
	*values = append(*values, value)
}

func embeddingProfileMatches(value inference.EmbeddingProfile, profile EmbeddingProfile) bool {
	if value == nil || isNilInterface(value) {
		return false
	}
	return value.ID() == profile.ProfileID && value.ModelName() == profile.Model &&
		value.DimensionCount() == profile.Dimension && value.NormalizationMode() == profile.Normalization
}

func cloneEmbeddingProfile(value *EmbeddingProfile) *EmbeddingProfile {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func isCanonicalVersion(value EntryVersion, material entryMaterial) bool {
	return value.Kind == material.kind && value.Text == material.text &&
		slices.Equal(value.Sources, material.sources) && slices.Equal(value.Artifacts, material.artifacts)
}

func bytesEqual(left, right []byte) bool { return slices.Equal(left, right) }

func ptrString(value string) *string { return &value }

func defaultID(kind string) (string, error) {
	prefixes := map[string]string{"memory": "mem_art", "entry": "mem_ent", "version": "mem_ver"}
	prefix, exists := prefixes[kind]
	if !exists {
		return "", &InvalidCandidateError{Code: "identity-kind", Detail: kind}
	}
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate Memory identity: %w", err)
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	return prefix + "_" + hex.EncodeToString(value[:]), nil
}

func isNilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
