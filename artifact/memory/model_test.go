package memory

import (
	"testing"

	"github.com/ob-labs/powercontext-go/artifact"
)

func TestMemoryModelsExpressExactRevisionAndEntryIdentity(t *testing.T) {
	entry, err := NewManifestEntry("entry-a", "version-a1", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Active)
	if err != nil {
		t.Fatal(err)
	}
	versionID := "version-a1"
	change, err := NewChange(Add, "entry-a", nil, &versionID, nil)
	if err != nil {
		t.Fatal(err)
	}
	content := NewContent(NewManifest([]ManifestEntry{entry}), []Change{change})
	draft, err := NewDraft(content, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	value, err := artifact.New("memory-a", 1, draft)
	if err != nil {
		t.Fatal(err)
	}
	wantRef, err := artifact.NewRef(Family, "memory-a", 1)
	if err != nil {
		t.Fatal(err)
	}
	if value.Family() != Family || value.Ref() != wantRef || value.Content().Schema() != "powercontext.memory.v1" ||
		value.Content().Manifest().Format() != "flat-v1" {
		t.Fatalf("Memory identity contract = %#v", value)
	}
}

func TestMemoryEntryKindRemainsOpenAndNonEmptyAfterCanonicalization(t *testing.T) {
	input := NewEntryInput(nil, "integration-owned-kind", "Durable text.", nil, nil, nil)
	if input.Kind() != "integration-owned-kind" || len(input.Sources()) != 0 || len(input.Artifacts()) != 0 {
		t.Fatalf("entry input = %#v", input)
	}
	if normalized, err := NormalizeKind(input.Kind()); err != nil || normalized != input.Kind() {
		t.Fatalf("open kind normalization = %q, %v", normalized, err)
	}
	if _, err := NormalizeKind("   "); err == nil {
		t.Fatal("empty Memory kind was accepted")
	}
}
