package runtime

import (
	"slices"
	"testing"

	"github.com/thunguo/powercontext-go/artifact/memory"
)

func TestCapabilitiesAreValidatedAndImmutable(t *testing.T) {
	t.Parallel()
	sources := []string{"content"}
	capabilities, err := NewCapabilities(CapabilityOptions{
		SourceTypes: sources, ArtifactFamilies: []string{"memory", "experience", "skill", "handoff"},
		MemoryExtraction: true, ExperienceGeneration: true, ManagedSkillGeneration: true,
		ExternalSkillRegistry: true, HandoffGeneration: true,
		SearchModes:     []memory.SearchMode{memory.SearchAuto, memory.SearchFTS},
		ContextVersions: []string{PreparedContextV1},
	})
	if err != nil {
		t.Fatal(err)
	}
	sources[0] = "mutated"
	got := capabilities.SourceTypes()
	got[0] = "also-mutated"
	if !slices.Equal(capabilities.SourceTypes(), []string{"content"}) {
		t.Fatalf("SourceTypes mutated: %#v", capabilities.SourceTypes())
	}
	if !capabilities.MemoryExtraction() || !capabilities.HandoffGeneration() {
		t.Fatal("boolean capabilities were lost")
	}
}

func TestCapabilitiesRejectDuplicatesAndUnknownValues(t *testing.T) {
	t.Parallel()
	for _, options := range []CapabilityOptions{
		{SourceTypes: []string{"content", "content"}},
		{SearchModes: []memory.SearchMode{"unknown"}},
		{ContextVersions: []string{"powercontext.future"}},
	} {
		if _, err := NewCapabilities(options); err == nil {
			t.Fatalf("NewCapabilities(%#v) succeeded", options)
		}
	}
}
