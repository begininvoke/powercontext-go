package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/ob-labs/powercontext-go/artifact"
	"github.com/ob-labs/powercontext-go/artifact/experience"
	"github.com/ob-labs/powercontext-go/artifact/memory"
	"github.com/ob-labs/powercontext-go/internal/contextpack"
)

func TestContextApplicationRecallsExperienceWithinOneScopedOperation(t *testing.T) {
	lifecycle := New()
	t.Cleanup(func() {
		if err := lifecycle.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	ref, err := artifact.NewRef(experience.Family, "experience-1", 2)
	if err != nil {
		t.Fatal(err)
	}
	content, err := experience.NewContent("situation", "action", "outcome", "lesson")
	if err != nil {
		t.Fatal(err)
	}
	called := false
	recall := ExperienceRecallFunc(func(_ context.Context, scopeID, query string, limit int) ([]experience.SearchHit, error) {
		called = true
		if scopeID != "scope-context" || query != "what happened" || limit != contextpack.ExperienceCandidateLimit {
			t.Fatalf("recall arguments = (%q, %q, %d)", scopeID, query, limit)
		}
		return []experience.SearchHit{{ArtifactRef: ref, Content: content}}, nil
	})
	application, err := NewContextApplication(lifecycle, nil, recall)
	if err != nil {
		t.Fatal(err)
	}
	request, err := contextpack.NewRequest("what happened", 0)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := application.Prepare(context.Background(), "scope-context", request)
	if err != nil {
		t.Fatal(err)
	}
	if !called || prepared.Status() != contextpack.Ready {
		t.Fatalf("recall called = %t, status = %q", called, prepared.Status())
	}
	if value := prepared.Content(); value == nil || !strings.Contains(*value, `"kind":"experience"`) {
		t.Fatalf("prepared content = %v", value)
	}
}

func TestContextApplicationRequiresSharedRuntime(t *testing.T) {
	first := New()
	second := New()
	memoryApplication, err := NewMemoryApplication(first, func(string) (*memory.Service, error) {
		return nil, nil
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewContextApplication(second, memoryApplication, nil); err == nil {
		t.Fatal("Context application accepted a Memory application owned by another Runtime")
	}
}
