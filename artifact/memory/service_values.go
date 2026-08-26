package memory

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/ob-labs/powercontext-go/artifact"
	"github.com/ob-labs/powercontext-go/inference"
	"github.com/ob-labs/powercontext-go/source"
)

// This file owns deterministic value normalization and defensive copies used
// by the Memory service. I/O and orchestration remain in service.go.
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
