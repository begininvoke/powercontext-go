package locomo_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ob-labs/powercontext-go/artifact"
	"github.com/ob-labs/powercontext-go/artifact/memory"
	"github.com/ob-labs/powercontext-go/inference"
	"github.com/ob-labs/powercontext-go/internal/benchmark/locomo"
	benchmarkprompts "github.com/ob-labs/powercontext-go/internal/benchmark/locomo/prompts"
	pcruntime "github.com/ob-labs/powercontext-go/runtime"
	"github.com/ob-labs/powercontext-go/source"
)

func TestRunnerIngestEvaluateFallbackRejudgeAndResume(t *testing.T) {
	dataset := loadDataset(t)
	conversation := dataset.Conversations()[0]
	question := dataset.SelectedQuestions(locomo.Selection{
		Categories: []int{1, 2, 3, 4}, ConversationLimit: intPointer(1), QuestionLimit: intPointer(1),
	})[0]
	root := t.TempDir()
	runDirectory := filepath.Join(root, "run")
	fixed := time.Date(2026, time.August, 26, 12, 34, 56, 123456789, time.FixedZone("test", 8*60*60))
	answerK := 1
	manifest, err := locomo.PrepareRun(dataset, locomo.RunOptions{
		RunID: "runner smoke", OutputDirectory: runDirectory, TopK: 1, AnswerK: &answerK,
		RerankMode: locomo.RerankNone, AnswerSourceContent: true,
		AnswerUnknownFallbackInference: true, JudgeProfile: benchmarkprompts.StrictJudge,
		Categories: []int{1, 2, 3, 4}, ConversationLimit: intPointer(1), QuestionLimit: intPointer(1),
		OperationRetries: 3, Configuration: publicConfiguration(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if manifest.SelectedQuestionCount != 1 {
		t.Fatalf("selected questions = %d", manifest.SelectedQuestionCount)
	}

	operations := newBenchmarkOperations(t)
	ingestion, err := locomo.Ingest(context.Background(), dataset, operations, locomo.IngestOptions{
		RunID: "runner smoke", OutputDirectory: runDirectory, DatabaseKind: "sqlite",
		ConversationLimit: intPointer(1), Concurrency: 2, OperationRetries: 3, Clock: func() time.Time { return fixed },
	})
	if err != nil {
		t.Fatal(err)
	}
	if ingestion.SessionCount != len(conversation.Sessions()) || ingestion.NewlyProcessedSessionCount != len(conversation.Sessions()) || ingestion.MemoryEntryCount != 1 {
		t.Fatalf("unexpected ingestion report: %+v", ingestion)
	}
	if ingestion.CompletedAt != "2026-08-26T04:34:56.123456+00:00" {
		t.Fatalf("Python-compatible timestamp = %q", ingestion.CompletedAt)
	}
	operations.assertCaptured(t, conversation)

	direct := &answerStub{answer: " Unknown "}
	fallback := &answerStub{answer: question.Answer()}
	judge := &judgeStub{label: "CORRECT"}
	report, err := locomo.Evaluate(context.Background(), dataset, operations, locomo.EvaluateOptions{
		RunID: "runner smoke", OutputDirectory: runDirectory, TopK: 1, AnswerK: 1,
		RerankMode: locomo.RerankNone, AnswerSourceContent: true,
		AnswerUnknownFallbackInference: true, JudgeProfile: benchmarkprompts.StrictJudge,
		Categories: []int{1, 2, 3, 4}, ConversationLimit: intPointer(1), QuestionLimit: intPointer(1),
		Concurrency: 2, OperationRetries: 3, AnswerGenerator: direct,
		FallbackAnswerGenerator: fallback, JudgeGenerator: judge, Clock: func() time.Time { return fixed },
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.CompletedAt != ingestion.CompletedAt || report.Metrics["overall"].Metrics["llm_judge"] != 1 ||
		report.Diagnostics.AnswerFallback == nil || report.Diagnostics.AnswerFallback.TriggeredCount != 1 {
		t.Fatalf("unexpected evaluation report: %+v", report)
	}
	if direct.calls != 1 || fallback.calls != 1 || judge.calls != 1 {
		t.Fatalf("generator calls direct=%d fallback=%d judge=%d", direct.calls, fallback.calls, judge.calls)
	}
	if fallback.last.Question != question.Text() || len(fallback.last.SourceSessions) != 1 || fallback.last.SourceSessions[0].SourceID != conversation.Sessions()[0].ID() {
		t.Fatalf("unexpected answer input: %+v", fallback.last)
	}

	observation := readJSONObjectForTest(t, filepath.Join(runDirectory, "observations.jsonl"))
	if fallbackRecord, ok := observation["answer_fallback"].(map[string]any); !ok || fallbackRecord["triggered"] != true {
		t.Fatalf("fallback observation = %#v", observation["answer_fallback"])
	}
	hits, ok := observation["hits"].([]any)
	if !ok || len(hits) != 1 {
		t.Fatalf("hits = %#v", observation["hits"])
	}
	sourceIDs := hits[0].(map[string]any)["source_ids"].([]any)
	if len(sourceIDs) != 1 || sourceIDs[0] != conversation.Sessions()[0].ID() || strings.Contains(sourceIDs[0].(string), ":") {
		t.Fatalf("source IDs are not Python-compatible: %#v", sourceIDs)
	}
	summary := readJSONMap(t, filepath.Join(runDirectory, "summary.json"))
	overall := summary["metrics"].(map[string]any)["overall"].(map[string]any)
	if _, nested := overall["metrics"]; nested || overall["llm_judge"] != float64(1) || overall["search_latency_ms_p50"] == nil {
		t.Fatalf("summary is not the frozen flat shape: %#v", overall)
	}

	// A successful checkpoint is reused and does not spend another model call.
	_, err = locomo.Evaluate(context.Background(), dataset, operations, locomo.EvaluateOptions{
		RunID: "runner smoke", OutputDirectory: runDirectory, TopK: 1, AnswerK: 1,
		RerankMode: locomo.RerankNone, AnswerSourceContent: true,
		AnswerUnknownFallbackInference: true, JudgeProfile: benchmarkprompts.StrictJudge,
		Categories: []int{1, 2, 3, 4}, ConversationLimit: intPointer(1), QuestionLimit: intPointer(1),
		Concurrency: 1, OperationRetries: 3, AnswerGenerator: direct,
		FallbackAnswerGenerator: fallback, JudgeGenerator: judge, Clock: func() time.Time { return fixed.Add(time.Hour) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if direct.calls != 1 || fallback.calls != 1 || judge.calls != 1 {
		t.Fatal("evaluation did not reuse its successful observation checkpoint")
	}

	rejudgeDirectory := filepath.Join(root, "rejudge")
	independentJudge := &judgeStub{label: "WRONG"}
	rejudged, err := locomo.Rejudge(context.Background(), dataset, locomo.RejudgeRunOptions{
		SourceDirectory: runDirectory, OutputDirectory: rejudgeDirectory, RunID: "independent judge",
		JudgeModel: "openai:independent", JudgeProfile: benchmarkprompts.TopicalJudge,
		Concurrency: 2, OperationRetries: 3, RetryErrors: true,
		JudgeGenerator: independentJudge, Clock: func() time.Time { return fixed },
	})
	if err != nil {
		t.Fatal(err)
	}
	if rejudged.Metrics["overall"].Metrics["llm_judge"] != 0 || independentJudge.calls != 1 {
		t.Fatalf("unexpected rejudge report: %+v", rejudged)
	}
	rejudgeObservation := readJSONObjectForTest(t, filepath.Join(rejudgeDirectory, "observations.jsonl"))
	sourceObservation := rejudgeObservation["source_observation"].(map[string]any)
	if sourceObservation["previous_llm_judge"] != float64(1) {
		t.Fatalf("previous judge score = %#v", sourceObservation["previous_llm_judge"])
	}
	markdown, err := os.ReadFile(filepath.Join(rejudgeDirectory, "summary.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(markdown), "Top K / Answer K / Source expansion: `1` / `1` / `True`") ||
		!strings.Contains(string(markdown), "Judge latency p50 / p95:") {
		t.Fatalf("rejudge summary omitted frozen answer context:\n%s", markdown)
	}

	resumed, err := locomo.Rejudge(context.Background(), dataset, locomo.RejudgeRunOptions{
		SourceDirectory: runDirectory, OutputDirectory: rejudgeDirectory, RunID: "independent judge",
		JudgeModel: "openai:independent", JudgeProfile: benchmarkprompts.TopicalJudge,
		Concurrency: 1, OperationRetries: 3, RetryErrors: true,
		JudgeGenerator: independentJudge, Clock: func() time.Time { return fixed.Add(24 * time.Hour) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if independentJudge.calls != 1 || resumed.CompletedAt != rejudged.CompletedAt {
		t.Fatalf("completed rejudge was not resumed: calls=%d completed=%q", independentJudge.calls, resumed.CompletedAt)
	}
}

func TestBenchmarkGeneratorsUseTemperatureZeroAndStrictStructuredRetry(t *testing.T) {
	model := &textModelStub{responses: []string{
		`{"answer":"first","unexpected":true}`,
		`{"answer":" accepted "}`,
	}}
	limits, err := inference.NewLimits(time.Second, 2)
	if err != nil {
		t.Fatal(err)
	}
	generator, err := locomo.NewAnswerGenerator(model, &limits, false, false)
	if err != nil {
		t.Fatal(err)
	}
	result, err := generator.Generate(context.Background(), locomo.AnswerInput{Question: "question"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Output.Answer != "accepted" || result.Usage.Requests != 2 || len(model.requests) != 2 {
		t.Fatalf("structured retry result = %+v, requests=%d", result, len(model.requests))
	}
	for _, request := range model.requests {
		temperature := request.Settings().Temperature()
		if temperature == nil || *temperature != locomo.BenchmarkTemperature || !request.StructuredOutput() {
			t.Fatalf("benchmark model settings = %+v", request.Settings())
		}
		instructions := strings.Join(request.Instructions(), "\n")
		if !strings.Contains(instructions, benchmarkprompts.AnswerVersion) && !strings.Contains(instructions, "Use only the provided memories") {
			t.Fatalf("answer instructions were not installed: %q", instructions)
		}
	}
	if len(model.requests[1].Messages()) != 3 {
		t.Fatalf("retry feedback messages = %d", len(model.requests[1].Messages()))
	}

	judgeModel := &textModelStub{responses: []string{`{"label":"MAYBE"}`, `{"label":"CORRECT"}`}}
	judgeGenerator, err := locomo.NewJudgeGenerator(judgeModel, &limits, benchmarkprompts.TopicalJudge)
	if err != nil {
		t.Fatal(err)
	}
	judged, err := judgeGenerator.Generate(context.Background(), locomo.JudgeInput{
		Question: "q", GoldAnswer: "g", GeneratedAnswer: "a",
	})
	if err != nil || judged.Output.Label != "CORRECT" || len(judgeModel.requests) != 2 {
		t.Fatalf("strict judge retry = %+v, %v", judged, err)
	}
}

type benchmarkScope struct {
	sources []string
	content map[string]string
	cursor  int64
}

type benchmarkOperations struct {
	testing *testing.T
	mutex   sync.Mutex
	scopes  map[string]*benchmarkScope
}

func newBenchmarkOperations(t *testing.T) *benchmarkOperations {
	return &benchmarkOperations{testing: t, scopes: make(map[string]*benchmarkScope)}
}

func (o *benchmarkOperations) scope(scopeID string) *benchmarkScope {
	value := o.scopes[scopeID]
	if value == nil {
		value = &benchmarkScope{content: make(map[string]string)}
		o.scopes[scopeID] = value
	}
	return value
}

func (o *benchmarkOperations) Capture(_ context.Context, scopeID, sourceID, content string, _ map[string]any) (int64, error) {
	o.mutex.Lock()
	defer o.mutex.Unlock()
	value := o.scope(scopeID)
	if _, exists := value.content[sourceID]; !exists {
		value.sources = append(value.sources, sourceID)
	}
	value.content[sourceID] = content
	return int64(len(value.sources)), nil
}

func (o *benchmarkOperations) Flush(_ context.Context, scopeID string) (pcruntime.MemoryFlushResult, error) {
	o.mutex.Lock()
	defer o.mutex.Unlock()
	value := o.scope(scopeID)
	previous := value.cursor
	processed := 0
	if value.cursor < int64(len(value.sources)) {
		value.cursor++
		processed = 1
	}
	result := pcruntime.MemoryFlushResult{
		PreviousCursor: previous, CurrentCursor: value.cursor,
		HighWatermark: int64(len(value.sources)), ProcessedSourceCount: processed,
	}
	if value.cursor > 0 {
		ref, err := artifact.NewRef(memory.Family, "benchmark-memory", value.cursor)
		if err != nil {
			return pcruntime.MemoryFlushResult{}, err
		}
		result.MemoryRef = &ref
	}
	return result, nil
}

func (o *benchmarkOperations) List(_ context.Context, scopeID string) (pcruntime.MemoryEntriesPage, error) {
	o.mutex.Lock()
	defer o.mutex.Unlock()
	value := o.scope(scopeID)
	if value.cursor == 0 {
		return pcruntime.MemoryEntriesPage{}, nil
	}
	memoryRef, err := artifact.NewRef(memory.Family, "benchmark-memory", value.cursor)
	if err != nil {
		return pcruntime.MemoryEntriesPage{}, err
	}
	sourceRef, err := source.NewRef("content", value.sources[0])
	if err != nil {
		return pcruntime.MemoryEntriesPage{}, err
	}
	return pcruntime.MemoryEntriesPage{
		MemoryRef: &memoryRef,
		Entries: []pcruntime.MemoryEntryRecord{{
			MemoryRef: memoryRef, State: memory.Active,
			Entry: memory.EntryVersion{
				MemoryArtifactID: memoryRef.ID(), EntryID: "entry-1", EntryVersionID: "entry-version-1",
				Version: 1, Kind: "fact", Text: "A deterministic remembered fact.", Sources: []source.Ref{sourceRef},
			},
		}},
	}, nil
}

func (o *benchmarkOperations) Search(_ context.Context, scopeID, _ string, _ int, mode memory.SearchMode) (pcruntime.MemorySearchPage, error) {
	page, err := o.List(context.Background(), scopeID)
	if err != nil || len(page.Entries) == 0 {
		return pcruntime.MemorySearchPage{}, err
	}
	entry := page.Entries[0].Entry
	return pcruntime.MemorySearchPage{
		MemoryRef: page.MemoryRef, Mode: &mode,
		Hits: []memory.Hit{{
			MemoryRef: *page.MemoryRef, EntryID: entry.EntryID, EntryVersionID: entry.EntryVersionID,
			Text: entry.Text, Score: 1, MatchedBy: []memory.MatchedBy{memory.MatchedFTS},
		}},
	}, nil
}

func (o *benchmarkOperations) assertCaptured(t *testing.T, conversation locomo.Conversation) {
	t.Helper()
	o.mutex.Lock()
	defer o.mutex.Unlock()
	scopeID, err := locomo.ScopeID("runner smoke", conversation.SampleID())
	if err != nil {
		t.Fatal(err)
	}
	value := o.scope(scopeID)
	if len(value.sources) != len(conversation.Sessions()) {
		t.Fatalf("captured sources = %d", len(value.sources))
	}
	for _, session := range conversation.Sessions() {
		content := value.content[session.ID()]
		if !strings.Contains(content, "Date and time: "+session.DateTime()) || strings.Contains(content, "Gold answer") {
			t.Fatalf("captured Source %s violated benchmark hygiene", session.ID())
		}
	}
}

type answerStub struct {
	answer string
	calls  int
	last   locomo.AnswerInput
}

func (g *answerStub) Generate(_ context.Context, input locomo.AnswerInput) (inference.GenerationResult[locomo.AnswerOutput], error) {
	g.calls++
	g.last = input
	return inference.GenerationResult[locomo.AnswerOutput]{
		Output: locomo.AnswerOutput{Answer: g.answer}, Usage: testUsage(7, 2),
	}, nil
}

type judgeStub struct {
	label string
	calls int
}

type textModelStub struct {
	responses []string
	requests  []inference.TextRequest
}

func (m *textModelStub) Complete(_ context.Context, request inference.TextRequest) (inference.TextResponse, error) {
	m.requests = append(m.requests, request)
	content := m.responses[0]
	m.responses = m.responses[1:]
	return inference.NewTextResponse(content, inference.Usage{Requests: 1})
}

func (g *judgeStub) Generate(_ context.Context, _ locomo.JudgeInput) (inference.GenerationResult[locomo.JudgeOutput], error) {
	g.calls++
	return inference.GenerationResult[locomo.JudgeOutput]{
		Output: locomo.JudgeOutput{Label: g.label}, Usage: testUsage(5, 1),
	}, nil
}

func testUsage(input, output int64) inference.Usage {
	return inference.Usage{Requests: 1, InputTokens: &input, OutputTokens: &output}
}

func readJSONObjectForTest(t *testing.T, path string) map[string]any {
	t.Helper()
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Split(strings.TrimSpace(string(encoded)), "\n")[0]
	var result map[string]any
	if err := json.Unmarshal([]byte(line), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func readJSONMap(t *testing.T, path string) map[string]any {
	t.Helper()
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
