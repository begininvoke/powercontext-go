package memory

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/ob-labs/powercontext-go/artifact"
	"github.com/ob-labs/powercontext-go/inference"
)

type contractSearchBackend struct {
	memory   Memory
	channels SearchChannels
	request  SearchRequest
}

func (b *contractSearchBackend) Capabilities() Capabilities {
	return Capabilities{FTS: true}
}

func (b *contractSearchBackend) Get(_ context.Context, ref artifact.Ref) (Memory, error) {
	if ref != b.memory.Ref() {
		return Memory{}, fmt.Errorf("unexpected Memory ref %s", ref)
	}
	return cloneMemoryValue(b.memory), nil
}

func (b *contractSearchBackend) Latest(_ context.Context, artifactID string) (Memory, error) {
	if artifactID != b.memory.ID() {
		return Memory{}, fmt.Errorf("unexpected Memory ID %q", artifactID)
	}
	return cloneMemoryValue(b.memory), nil
}

func (*contractSearchBackend) Entries(context.Context, artifact.Ref) ([]EntryVersion, error) {
	return nil, fmt.Errorf("unexpected Entries call")
}

func (*contractSearchBackend) Projections(context.Context, artifact.Ref) ([]Projection, error) {
	return nil, fmt.Errorf("unexpected Projections call")
}

func (*contractSearchBackend) Commit(context.Context, Commit) (Memory, error) {
	return Memory{}, fmt.Errorf("unexpected Commit call")
}

func (*contractSearchBackend) Changes(context.Context, artifact.Ref, *int64) ([]RevisionChanges, error) {
	return nil, fmt.Errorf("unexpected Changes call")
}

func (*contractSearchBackend) VectorComplete(context.Context, []artifact.Ref, EmbeddingProfile) (bool, error) {
	return false, nil
}

func (b *contractSearchBackend) Search(_ context.Context, request SearchRequest) (SearchChannels, error) {
	b.request = request.Clone()
	return b.channels.Clone(), nil
}

func (*contractSearchBackend) Expand(context.Context, []Hit) ([]EntryVersion, error) {
	return nil, fmt.Errorf("unexpected Expand call")
}

type selectingContractReranker struct {
	candidates []Hit
	query      string
	limit      int
}

func (*selectingContractReranker) PolicyID() string { return "test.memory.rerank.v1" }

func (r *selectingContractReranker) Rerank(
	_ context.Context,
	query string,
	candidates []Hit,
	limit int,
) (RerankDecision, error) {
	r.query = query
	r.candidates = cloneHits(candidates)
	r.limit = limit
	inputTokens, outputTokens := int64(20), int64(2)
	return RerankDecision{
		selectedRanks: []int{3, 1},
		usage: inference.Usage{
			Requests: 1, InputTokens: &inputTokens, OutputTokens: &outputTokens,
		},
	}, nil
}

func TestMemorySearchAppliesInjectedRerankerAfterCoarseFusion(t *testing.T) {
	content := NewContent(NewManifest(nil), nil)
	draft, err := NewDraft(content, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	value, err := artifact.New("memory-a", 1, draft)
	if err != nil {
		t.Fatal(err)
	}
	hits := make([]ChannelHit, 4)
	for index := range hits {
		hits[index], err = NewChannelHit(
			value.Ref(),
			fmt.Sprintf("entry-%d", index+1),
			fmt.Sprintf("version-%d", index+1),
			fmt.Sprintf("Project fact %d.", index+1),
			nil,
		)
		if err != nil {
			t.Fatal(err)
		}
	}
	backend := &contractSearchBackend{memory: value, channels: SearchChannels{FTS: hits}}
	reranker := &selectingContractReranker{}
	times := []time.Time{time.Unix(0, 0), time.Unix(0, int64(5*time.Millisecond))}
	clockIndex := 0
	service, err := NewService(backend, ServiceOptions{
		Reranker: reranker, RerankCandidateLimit: 4,
		Clock: func() time.Time {
			value := times[min(clockIndex, len(times)-1)]
			clockIndex++
			return value
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := service.Search(t.Context(), "project", []Memory{value}, 2, SearchFTS)
	if err != nil {
		t.Fatal(err)
	}
	if reranker.query != "project" || reranker.limit != 2 || len(reranker.candidates) != 4 {
		t.Fatalf("reranker call = query %q limit %d candidates %#v", reranker.query, reranker.limit, reranker.candidates)
	}
	wantHits := []Hit{reranker.candidates[2], reranker.candidates[0]}
	if !reflect.DeepEqual(result.Hits, wantHits) {
		t.Fatalf("reranked hits = %#v, want %#v", result.Hits, wantHits)
	}
	if result.Rerank == nil || result.Rerank.PolicyID != reranker.PolicyID() ||
		!reflect.DeepEqual(result.Rerank.CandidateHits, reranker.candidates) ||
		!reflect.DeepEqual(result.Rerank.SelectedRanks, []int{3, 1}) ||
		result.Rerank.Usage.Requests != 1 || result.Rerank.LatencyMS != 5 {
		t.Fatalf("rerank trace = %#v", result.Rerank)
	}
	if backend.request.CandidateLimit != 32 || backend.request.Mode != SearchFTS {
		t.Fatalf("coarse search request = %#v", backend.request)
	}
}
