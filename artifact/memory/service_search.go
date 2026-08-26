package memory

import (
	"context"
	"errors"
	"math"
	"reflect"

	"github.com/ob-labs/powercontext-go/artifact"
	"github.com/ob-labs/powercontext-go/inference"
)

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
