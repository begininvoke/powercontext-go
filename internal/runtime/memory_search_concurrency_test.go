// Copyright (c) 2026 OceanBase.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package runtime

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/ob-labs/powercontext-go/artifact"
	"github.com/ob-labs/powercontext-go/artifact/memory"
	"github.com/ob-labs/powercontext-go/inference"
)

type advancingSearchBackend struct {
	revisions   []memory.Memory
	current     int
	failures    int
	searchCalls int
	channels    memory.SearchChannels
}

func (*advancingSearchBackend) Capabilities() memory.Capabilities {
	return memory.Capabilities{FTS: true}
}

func (b *advancingSearchBackend) Get(_ context.Context, ref artifact.Ref) (memory.Memory, error) {
	for _, value := range b.revisions {
		if value.Ref() == ref {
			return value, nil
		}
	}
	return memory.Memory{}, &artifact.NotFoundError{Ref: ref}
}

func (b *advancingSearchBackend) Latest(_ context.Context, artifactID string) (memory.Memory, error) {
	if len(b.revisions) == 0 || artifactID != b.revisions[0].ID() {
		return memory.Memory{}, fmt.Errorf("unexpected Memory identity %q", artifactID)
	}
	return b.revisions[b.current], nil
}

func (*advancingSearchBackend) Entries(context.Context, artifact.Ref) ([]memory.EntryVersion, error) {
	return nil, errors.New("unexpected Entries call")
}

func (*advancingSearchBackend) Projections(context.Context, artifact.Ref) ([]memory.Projection, error) {
	return nil, errors.New("unexpected Projections call")
}

func (*advancingSearchBackend) Commit(context.Context, memory.Commit) (memory.Memory, error) {
	return memory.Memory{}, errors.New("unexpected Commit call")
}

func (*advancingSearchBackend) Changes(context.Context, artifact.Ref, *int64) ([]memory.RevisionChanges, error) {
	return nil, errors.New("unexpected Changes call")
}

func (*advancingSearchBackend) VectorComplete(context.Context, []artifact.Ref, memory.EmbeddingProfile) (bool, error) {
	return false, nil
}

func (b *advancingSearchBackend) Search(context.Context, memory.SearchRequest) (memory.SearchChannels, error) {
	b.searchCalls++
	if b.searchCalls <= b.failures {
		b.current++
		return memory.SearchChannels{}, &memory.InvalidCitationError{Code: "memory-mismatch"}
	}
	return b.channels.Clone(), nil
}

func (*advancingSearchBackend) Expand(context.Context, []memory.Hit) ([]memory.EntryVersion, error) {
	return nil, errors.New("unexpected Expand call")
}

func TestMemorySearchRetriesOneAdvancedHead(t *testing.T) {
	backend := &advancingSearchBackend{revisions: searchMemoryRevisions(t, 2), failures: 1}
	application := newSearchConcurrencyApplication(t, backend)
	page, err := application.Search(t.Context(), "scope", "project", 5, memory.SearchFTS)
	if err != nil {
		t.Fatal(err)
	}
	if backend.searchCalls != 2 || page.MemoryRef == nil || page.MemoryRef.Revision() != 2 ||
		page.Mode == nil || *page.Mode != memory.SearchFTS {
		t.Fatalf("search calls = %d, page = %#v", backend.searchCalls, page)
	}
}

func TestMemorySearchReturnsConflictAfterThreeAdvancedHeads(t *testing.T) {
	backend := &advancingSearchBackend{revisions: searchMemoryRevisions(t, 4), failures: 3}
	application := newSearchConcurrencyApplication(t, backend)
	_, err := application.Search(t.Context(), "scope", "project", 5, memory.SearchFTS)
	var conflict *artifact.RevisionConflictError
	if !errors.As(err, &conflict) || conflict.Requested.Revision() != 3 || conflict.Current.Revision() != 4 {
		t.Fatalf("search error = %#v", err)
	}
	if backend.searchCalls != memorySearchAttempts {
		t.Fatalf("search calls = %d", backend.searchCalls)
	}
}

type searchRerankGenerator func(
	context.Context,
	memory.RerankInput,
) (inference.GenerationResult[memory.RerankOutput], error)

func (f searchRerankGenerator) Generate(
	ctx context.Context,
	input memory.RerankInput,
) (inference.GenerationResult[memory.RerankOutput], error) {
	return f(ctx, input)
}

func TestMemorySearchKeepsCompletedRevisionWhenHeadAdvancesDuringReranking(t *testing.T) {
	revisions := searchMemoryRevisions(t, 2)
	hit, err := memory.NewChannelHit(
		revisions[0].Ref(), "entry-1", "version-1", "Stable searchable fact.", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	backend := &advancingSearchBackend{
		revisions: revisions,
		channels:  memory.SearchChannels{FTS: []memory.ChannelHit{hit}},
	}
	reranker, err := memory.NewLLMReranker(searchRerankGenerator(func(
		context.Context,
		memory.RerankInput,
	) (inference.GenerationResult[memory.RerankOutput], error) {
		backend.current = 1
		return inference.GenerationResult[memory.RerankOutput]{
			Output: memory.NewRerankOutput([]int{1}),
			Usage:  inference.Usage{Requests: 1},
		}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	application := newSearchConcurrencyApplicationWithOptions(t, backend, memory.ServiceOptions{Reranker: reranker})
	page, err := application.Search(t.Context(), "scope", "stable searchable", 1, memory.SearchFTS)
	if err != nil {
		t.Fatal(err)
	}
	if backend.current != 1 || page.MemoryRef == nil || *page.MemoryRef != revisions[0].Ref() ||
		len(page.Hits) != 1 || page.Hits[0].MemoryRef != revisions[0].Ref() || page.Rerank == nil {
		t.Fatalf("completed search snapshot = current %d, page %#v", backend.current, page)
	}
}

func newSearchConcurrencyApplication(t *testing.T, backend memory.Backend) *MemoryApplication {
	return newSearchConcurrencyApplicationWithOptions(t, backend, memory.ServiceOptions{})
}

func newSearchConcurrencyApplicationWithOptions(
	t *testing.T,
	backend memory.Backend,
	options memory.ServiceOptions,
) *MemoryApplication {
	t.Helper()
	lifecycle := New()
	t.Cleanup(func() {
		if err := lifecycle.Close(context.Background()); err != nil {
			t.Errorf("close Runtime: %v", err)
		}
	})
	service, err := memory.NewService(backend, options)
	if err != nil {
		t.Fatal(err)
	}
	application, err := NewMemoryApplication(lifecycle, func(string) (*memory.Service, error) {
		return service, nil
	}, "memory")
	if err != nil {
		t.Fatal(err)
	}
	return application
}

func searchMemoryRevisions(t *testing.T, count int) []memory.Memory {
	t.Helper()
	content := memory.NewContent(memory.NewManifest(nil), nil)
	draft, err := memory.NewDraft(content, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	values := make([]memory.Memory, count)
	for index := range values {
		values[index], err = artifact.New("memory", int64(index+1), draft)
		if err != nil {
			t.Fatal(err)
		}
	}
	return values
}

var _ memory.Backend = (*advancingSearchBackend)(nil)
