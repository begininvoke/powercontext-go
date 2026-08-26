package memory

import (
	"context"
	"errors"
	"slices"

	"github.com/ob-labs/powercontext-go/inference"
)

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
