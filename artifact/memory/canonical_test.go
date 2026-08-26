package memory

import (
	"math"
	"strings"
	"testing"

	"github.com/ob-labs/powercontext-go/artifact"
	"github.com/ob-labs/powercontext-go/source"
)

func TestEntryHashNormalizesContentAndSortsDeduplicatesRefs(t *testing.T) {
	a, _ := source.NewRef("content", "a")
	b, _ := source.NewRef("content", "b")
	parent, _ := artifact.NewRef("experience", "z", 2)
	first, err := EntryContentHash("fact", "Cafe\u0301  ", []source.Ref{b, a, a}, []artifact.Ref{parent})
	if err != nil {
		t.Fatal(err)
	}
	second, err := EntryContentHash("fact", "Café", []source.Ref{a, b}, []artifact.Ref{parent})
	if err != nil {
		t.Fatal(err)
	}
	if first != second || len(first) != 64 {
		t.Fatalf("unexpected hashes: %s %s", first, second)
	}
	encoded, err := EntryContentBytes("fact", "Café", []source.Ref{a}, []artifact.Ref{parent})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(encoded), `{"artifact_refs"`) {
		t.Fatalf("not JCS ordered: %s", encoded)
	}
}

func TestEntryHashCoversKindTextAndBothEvidenceSets(t *testing.T) {
	sourceRef, err := source.NewRef("content", "source-a")
	if err != nil {
		t.Fatal(err)
	}
	artifactRef, err := artifact.NewRef("experience", "artifact-a", 1)
	if err != nil {
		t.Fatal(err)
	}
	hash := func(kind, text string, sources []source.Ref, artifacts []artifact.Ref) string {
		t.Helper()
		value, err := EntryContentHash(kind, text, sources, artifacts)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	baseline := hash("fact", "Durable fact.", []source.Ref{sourceRef}, []artifact.Ref{artifactRef})
	for name, changed := range map[string]string{
		"kind":      hash("decision", "Durable fact.", []source.Ref{sourceRef}, []artifact.Ref{artifactRef}),
		"text":      hash("fact", "Changed fact.", []source.Ref{sourceRef}, []artifact.Ref{artifactRef}),
		"sources":   hash("fact", "Durable fact.", nil, []artifact.Ref{artifactRef}),
		"artifacts": hash("fact", "Durable fact.", []source.Ref{sourceRef}, nil),
	} {
		if changed == baseline {
			t.Fatalf("%s was not bound into the entry hash", name)
		}
	}
}

func TestNormalizationLimitsMatchPythonSemantics(t *testing.T) {
	if got, _ := NormalizeText("  durable  "); got != "durable" {
		t.Fatalf("unexpected normalized text %q", got)
	}
	blank := "   "
	reason, err := NormalizeReason(&blank)
	if err != nil || reason != nil {
		t.Fatalf("blank reason was not removed: %v %v", reason, err)
	}
	if _, err := NormalizeText(strings.Repeat("界", 2731)); err == nil {
		t.Fatal("expected UTF-8 byte limit")
	}
	tooLong := strings.Repeat("x", 513)
	if _, err := NormalizeReason(&tooLong); err == nil {
		t.Fatal("expected reason rune limit")
	}
	if _, err := ValidateIdentifier("记忆"); err == nil {
		t.Fatal("expected ASCII-only identifier")
	}
}

func TestNormalizationUsesFrozenPythonWhitespaceSemantics(t *testing.T) {
	const separators = "\u001c\u001d\u001e\u001f"
	for _, test := range []struct {
		name      string
		normalize func(string) (string, error)
	}{
		{name: "text", normalize: NormalizeText},
		{name: "kind", normalize: NormalizeKind},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.normalize(separators + " durable " + separators)
			if err != nil {
				t.Fatal(err)
			}
			if got != "durable" {
				t.Fatalf("normalized value = %q, want frozen Python strip result", got)
			}
		})
	}
	reason := separators + " because " + separators
	got, err := NormalizeReason(&reason)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || *got != "because" {
		t.Fatalf("normalized reason = %v, want because", got)
	}
}

func TestEmbeddingNormalizationIsOverflowStable(t *testing.T) {
	got, err := NormalizeEmbedding([]float64{1e308, 1e308}, 2)
	if err != nil {
		t.Fatal(err)
	}
	want := math.Sqrt(0.5)
	if math.Abs(got[0]-want) > 1e-15 || math.Abs(got[1]-want) > 1e-15 {
		t.Fatalf("unexpected unit vector: %v", got)
	}
	if _, err := NormalizeEmbedding([]float64{0, 0}, 2); err == nil {
		t.Fatal("expected zero-vector rejection")
	}
	if _, err := ValidateEmbedding([]float64{1, math.Inf(1)}, 2); err == nil {
		t.Fatal("expected non-finite rejection")
	}
}

func TestEmbeddingValidationRejectsWrongOrNonFiniteVectors(t *testing.T) {
	got, err := ValidateEmbedding([]float64{1, 2.5, -3}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0] != 1 || got[1] != 2.5 || got[2] != -3 {
		t.Fatalf("validated embedding = %v", got)
	}
	if _, err := ValidateEmbedding([]float64{1, 2}, 3); err == nil {
		t.Fatal("wrong embedding dimension was accepted")
	}
	for _, invalid := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := ValidateEmbedding([]float64{1, invalid, 3}, 3); err == nil {
			t.Fatalf("non-finite embedding value %v was accepted", invalid)
		}
	}
	if _, err := ValidateEmbedding(nil, 0); err == nil {
		t.Fatal("non-positive embedding dimension was accepted")
	}
}

func TestEmbeddingHashBindsProfileAndEntryContent(t *testing.T) {
	profileA, err := NewEmbeddingProfile("profile-a", "model-a", 3, "none")
	if err != nil {
		t.Fatal(err)
	}
	profileB, err := NewEmbeddingProfile("profile-b", "model-a", 3, "none")
	if err != nil {
		t.Fatal(err)
	}
	first, err := EmbeddingContentHash(profileA, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	second, err := EmbeddingContentHash(profileB, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	changedContent, err := EmbeddingContentHash(profileA, strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}
	if first == second || first == changedContent || len(first) != 64 {
		t.Fatalf("embedding hash did not bind profile and content: %q %q %q", first, second, changedContent)
	}
}

func TestContentHashCoversChangeOperation(t *testing.T) {
	entry, err := NewManifestEntry("entry-a", "version-a1", strings.Repeat("a", 64), Active)
	if err != nil {
		t.Fatal(err)
	}
	to := "version-a1"
	added, _ := NewChange(Add, "entry-a", nil, &to, nil)
	reactivated, _ := NewChange(Reactivate, "entry-a", nil, &to, nil)
	first, err := ContentHash(NewContent(NewManifest([]ManifestEntry{entry}), []Change{added}))
	if err != nil {
		t.Fatal(err)
	}
	second, err := ContentHash(NewContent(NewManifest([]ManifestEntry{entry}), []Change{reactivated}))
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("change operation was not hashed")
	}
}
