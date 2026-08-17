package cli

import (
	"fmt"
	"strconv"
	"strings"

	v1 "github.com/thunguo/powercontext-go/api/v1"
)

func sourceReferences(values []string) ([]v1.SourceReference, error) {
	result := make([]v1.SourceReference, len(values))
	seen := make(map[v1.SourceReference]struct{}, len(values))
	for index, value := range values {
		name, id, ok := strings.Cut(value, "/")
		if !ok || name == "" || id == "" || value != strings.TrimSpace(value) {
			return nil, fmt.Errorf("invalid --source-ref %q; expected TYPE/ID", value)
		}
		reference := v1.SourceReference{Name: name, SourceID: id}
		if _, duplicate := seen[reference]; duplicate {
			return nil, fmt.Errorf("duplicate Source reference: %s", value)
		}
		result[index] = reference
		seen[reference] = struct{}{}
	}
	return result, nil
}

func artifactReferences(values []string) ([]v1.ArtifactReference, error) {
	result := make([]v1.ArtifactReference, len(values))
	seen := make(map[v1.ArtifactReference]struct{}, len(values))
	for index, value := range values {
		ref, err := artifactReference(value)
		if err != nil {
			return nil, fmt.Errorf("invalid --artifact-ref: %w", err)
		}
		if _, duplicate := seen[ref]; duplicate {
			return nil, fmt.Errorf("duplicate Artifact reference: %s", value)
		}
		result[index] = ref
		seen[ref] = struct{}{}
	}
	return result, nil
}

func artifactReference(value string) (v1.ArtifactReference, error) {
	family, rest, ok := strings.Cut(value, "/")
	separator := strings.LastIndexByte(rest, '@')
	id, revisionText := "", ""
	if separator >= 0 {
		id, revisionText = rest[:separator], rest[separator+1:]
	}
	revision, err := strconv.Atoi(revisionText)
	if !ok || separator < 0 || family == "" || id == "" || revisionText == "" ||
		err != nil || revision < 1 || value != strings.TrimSpace(value) {
		return v1.ArtifactReference{}, fmt.Errorf("expected FAMILY/ID@REVISION")
	}
	return v1.ArtifactReference{Family: family, ArtifactID: id, Revision: revision}, nil
}

func evidenceReferences(
	sourceValues, artifactValues []string,
	targetValue string,
) ([]v1.SourceReference, []v1.ArtifactReference, v1.OptNilArtifactReference, error) {
	sources, err := sourceReferences(sourceValues)
	if err != nil {
		return nil, nil, v1.OptNilArtifactReference{}, err
	}
	artifacts, err := artifactReferences(artifactValues)
	if err != nil {
		return nil, nil, v1.OptNilArtifactReference{}, err
	}
	if targetValue == "" {
		return sources, artifacts, v1.OptNilArtifactReference{}, nil
	}
	target, err := artifactReference(targetValue)
	if err != nil {
		return nil, nil, v1.OptNilArtifactReference{}, fmt.Errorf("invalid --target: %w", err)
	}
	found := false
	for _, value := range artifacts {
		if value == target {
			found = true
			break
		}
	}
	if !found {
		artifacts = append(artifacts, target)
	}
	return sources, artifacts, v1.NewOptNilArtifactReference(target), nil
}

func requireTargetFamily(target v1.OptNilArtifactReference, family string) error {
	if value, ok := target.Get(); ok && value.Family != family {
		return fmt.Errorf("--target must reference family %s", family)
	}
	return nil
}
