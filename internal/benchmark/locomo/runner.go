package locomo

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ob-labs/powercontext-go/artifact/memory"
	"github.com/ob-labs/powercontext-go/inference"
	benchmarkprompts "github.com/ob-labs/powercontext-go/internal/benchmark/locomo/prompts"
	pcruntime "github.com/ob-labs/powercontext-go/runtime"
)

const maxObservationBytes = 16 << 20

type Progress func(string)

// Operations is the narrow use-case boundary required by the benchmark. The
// production composition uses the same Runtime operations; tests can provide a
// deterministic implementation without a database or model provider.
type Operations interface {
	Capture(context.Context, string, string, string, map[string]any) (int64, error)
	Flush(context.Context, string) (pcruntime.MemoryFlushResult, error)
	List(context.Context, string) (pcruntime.MemoryEntriesPage, error)
	Search(context.Context, string, string, int, memory.SearchMode) (pcruntime.MemorySearchPage, error)
}

type IngestOptions struct {
	RunID             string
	OutputDirectory   string
	DatabaseKind      string
	ConversationLimit *int
	Concurrency       int
	OperationRetries  int
	Clock             func() time.Time
	Progress          Progress
}

type ConversationIngestion struct {
	ScopeID           string   `json:"scope_id"`
	SessionCount      int      `json:"session_count"`
	MemoryEntryCount  int      `json:"memory_entry_count"`
	MemoryRevision    *int64   `json:"memory_revision"`
	FlushLatencyMSP50 *float64 `json:"flush_latency_ms_p50"`
	FlushLatencyMSP95 *float64 `json:"flush_latency_ms_p95"`
	resumedSessions   int
	unchangedFlushes  int
	transientRetries  int
}

type IngestionReport struct {
	Schema                     string                           `json:"schema"`
	RunID                      string                           `json:"run_id"`
	CompletedAt                string                           `json:"completed_at"`
	DatabaseKind               string                           `json:"database_kind"`
	ConversationCount          int                              `json:"conversation_count"`
	SessionCount               int                              `json:"session_count"`
	ResumedSessionCount        int                              `json:"resumed_session_count"`
	NewlyProcessedSessionCount int                              `json:"newly_processed_session_count"`
	NoMemoryChangeFlushCount   int                              `json:"no_memory_change_flush_count"`
	TransientRetryCount        int                              `json:"transient_retry_count"`
	MemoryEntryCount           int                              `json:"memory_entry_count"`
	DurationSeconds            float64                          `json:"duration_seconds"`
	Conversations              map[string]ConversationIngestion `json:"conversations"`
}

func Ingest(ctx context.Context, dataset Dataset, operations Operations, options IngestOptions) (IngestionReport, error) {
	if operations == nil {
		return IngestionReport{}, fmt.Errorf("benchmark operations must not be nil")
	}
	if options.Concurrency < 1 {
		return IngestionReport{}, fmt.Errorf("ingestion concurrency must be positive")
	}
	if options.OperationRetries < 1 {
		return IngestionReport{}, fmt.Errorf("operation_retries must be positive")
	}
	runID, err := NormalizeRunID(options.RunID)
	if err != nil {
		return IngestionReport{}, err
	}
	conversations := dataset.Conversations()
	if options.ConversationLimit != nil && *options.ConversationLimit < len(conversations) {
		conversations = conversations[:max(*options.ConversationLimit, 0)]
	}
	clock := options.Clock
	if clock == nil {
		clock = time.Now
	}
	progress := options.Progress
	if progress == nil {
		progress = func(string) {}
	}
	started := time.Now()
	results, err := parallelConversations(ctx, conversations, options.Concurrency, func(ctx context.Context, conversation Conversation) (ConversationIngestion, error) {
		return ingestConversation(ctx, dataset, conversation, operations, runID, options.OperationRetries, progress)
	})
	if err != nil {
		return IngestionReport{}, err
	}

	report := IngestionReport{
		Schema: "powercontext.benchmark.locomo.ingestion.v1", RunID: runID,
		CompletedAt: pythonUTC(clock()), DatabaseKind: options.DatabaseKind,
		ConversationCount: len(conversations), DurationSeconds: time.Since(started).Seconds(),
		Conversations: make(map[string]ConversationIngestion, len(results)),
	}
	for index, result := range results {
		report.Conversations[conversations[index].SampleID()] = result
		report.SessionCount += result.SessionCount
		report.ResumedSessionCount += result.resumedSessions
		report.NoMemoryChangeFlushCount += result.unchangedFlushes
		report.TransientRetryCount += result.transientRetries
		report.MemoryEntryCount += result.MemoryEntryCount
	}
	report.NewlyProcessedSessionCount = report.SessionCount - report.ResumedSessionCount
	if err := writeJSON(filepath.Join(options.OutputDirectory, "ingestion.json"), report); err != nil {
		return IngestionReport{}, err
	}
	return report, nil
}

func ingestConversation(
	ctx context.Context,
	dataset Dataset,
	conversation Conversation,
	operations Operations,
	runID string,
	attempts int,
	progress Progress,
) (ConversationIngestion, error) {
	scopeID, err := ScopeID(runID, conversation.SampleID())
	if err != nil {
		return ConversationIngestion{}, err
	}
	sessions := conversation.Sessions()
	for _, session := range sessions {
		metadata := map[string]any{
			"benchmark": "locomo", "dataset_sha256": dataset.SHA256(),
			"sample_id": conversation.SampleID(), "session_id": session.ID(), "date_time": session.DateTime(),
		}
		if _, err := operations.Capture(ctx, scopeID, session.ID(), RenderSession(conversation, session), metadata); err != nil {
			return ConversationIngestion{}, err
		}
	}
	page, err := operations.List(ctx, scopeID)
	if err != nil {
		return ConversationIngestion{}, err
	}
	var previousRevision *int64
	if page.MemoryRef != nil {
		revision := page.MemoryRef.Revision()
		previousRevision = &revision
	}
	result := ConversationIngestion{ScopeID: scopeID, SessionCount: len(sessions)}
	latencies := make([]float64, 0, len(sessions))
	cursor := int64(-1)
	for {
		flushStarted := time.Now()
		flush, retries, err := retryTransient(ctx, attempts, func(ctx context.Context) (pcruntime.MemoryFlushResult, error) {
			return operations.Flush(ctx, scopeID)
		})
		if err != nil {
			return ConversationIngestion{}, err
		}
		result.transientRetries += retries
		if cursor < 0 {
			cursor = flush.PreviousCursor
			result.resumedSessions = int(cursor)
		}
		if flush.PreviousCursor != cursor || flush.HighWatermark > int64(len(sessions)) {
			return ConversationIngestion{}, fmt.Errorf("scope %s contains an unexpected Source cursor", scopeID)
		}
		if flush.ProcessedSourceCount == 0 {
			if flush.CurrentCursor != flush.HighWatermark {
				return ConversationIngestion{}, fmt.Errorf("scope %s did not advance its pending Source", scopeID)
			}
			break
		}
		if flush.ProcessedSourceCount != 1 || flush.CurrentCursor != cursor+1 {
			return ConversationIngestion{}, fmt.Errorf("scope %s did not advance exactly one Source", scopeID)
		}
		latencies = append(latencies, float64(time.Since(flushStarted))/float64(time.Millisecond))
		currentRevision := (*int64)(nil)
		if flush.MemoryRef != nil {
			revision := flush.MemoryRef.Revision()
			currentRevision = &revision
		}
		if equalInt64Pointer(currentRevision, previousRevision) {
			result.unchangedFlushes++
		}
		previousRevision = currentRevision
		cursor = flush.CurrentCursor
		progress(fmt.Sprintf("[ingest] %s %d/%d", conversation.SampleID(), cursor, len(sessions)))
		if cursor == flush.HighWatermark {
			break
		}
	}
	page, err = operations.List(ctx, scopeID)
	if err != nil {
		return ConversationIngestion{}, err
	}
	result.MemoryEntryCount = len(page.Entries)
	if page.MemoryRef != nil {
		revision := page.MemoryRef.Revision()
		result.MemoryRevision = &revision
	}
	result.FlushLatencyMSP50 = Percentile(latencies, .5)
	result.FlushLatencyMSP95 = Percentile(latencies, .95)
	return result, nil
}

type EvaluateOptions struct {
	RunID                          string
	OutputDirectory                string
	TopK                           int
	AnswerK                        int
	RerankMode                     RerankMode
	AnswerSourceContent            bool
	AnswerInferenceAware           bool
	AnswerUnknownFallbackInference bool
	JudgeProfile                   benchmarkprompts.JudgeProfile
	Categories                     []int
	ConversationLimit              *int
	QuestionLimit                  *int
	Concurrency                    int
	OperationRetries               int
	RetryErrors                    bool
	AnswerGenerator                inference.StructuredGenerator[AnswerInput, AnswerOutput]
	FallbackAnswerGenerator        inference.StructuredGenerator[AnswerInput, AnswerOutput]
	JudgeGenerator                 inference.StructuredGenerator[JudgeInput, JudgeOutput]
	Clock                          func() time.Time
	Progress                       Progress
}

type RerankMetadata struct {
	Mode                   RerankMode `json:"mode"`
	PolicyID               *string    `json:"policy_id"`
	CandidateCount         int        `json:"candidate_count"`
	AnswerCount            int        `json:"answer_count"`
	SelectedRetrievalRanks []int      `json:"selected_retrieval_ranks"`
	DiscardedRankCount     int        `json:"discarded_rank_count"`
	UsedFallback           bool       `json:"used_fallback"`
}

type AnswerContext struct {
	SourceContent bool     `json:"source_content"`
	SourceIDs     []string `json:"source_ids"`
}

type AnswerFallbackRecord struct {
	Trigger       string `json:"trigger"`
	Triggered     bool   `json:"triggered"`
	InitialAnswer string `json:"initial_answer"`
	Instructions  string `json:"instructions"`
}

type ObservationRecord struct {
	Schema           string                `json:"schema"`
	QuestionID       string                `json:"question_id"`
	SampleID         string                `json:"sample_id"`
	Category         int                   `json:"category"`
	Question         string                `json:"question"`
	GoldAnswer       string                `json:"gold_answer"`
	GeneratedAnswer  string                `json:"generated_answer,omitempty"`
	EvidenceRaw      []string              `json:"evidence_raw,omitempty"`
	Evidence         []string              `json:"evidence,omitempty"`
	EvidenceSessions []string              `json:"evidence_sessions,omitempty"`
	Status           string                `json:"status"`
	RetrievalMode    string                `json:"retrieval_mode,omitempty"`
	CandidateHits    []RetrievedMemory     `json:"candidate_hits,omitempty"`
	Hits             []RetrievedMemory     `json:"hits,omitempty"`
	Rerank           *RerankMetadata       `json:"rerank,omitempty"`
	AnswerContext    *AnswerContext        `json:"answer_context,omitempty"`
	AnswerFallback   *AnswerFallbackRecord `json:"answer_fallback,omitempty"`
	Metrics          map[string]float64    `json:"metrics,omitempty"`
	LatencyMS        map[string]float64    `json:"latency_ms"`
	Usage            map[string]Usage      `json:"usage,omitempty"`
	TransientRetries map[string]int        `json:"transient_retries,omitempty"`
	ErrorType        string                `json:"error_type,omitempty"`
	ErrorStage       string                `json:"error_stage,omitempty"`
}

func (r ObservationRecord) MarshalJSON() ([]byte, error) {
	// Successful Python observations contain every collection, including an
	// empty list. Error observations omit those fields. omitempty alone cannot
	// preserve that distinction.
	type observationAlias ObservationRecord
	encoded, err := json.Marshal(observationAlias(r))
	if err != nil || r.Status != "ok" {
		return encoded, err
	}
	var value map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(encoded)))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if len(r.EvidenceRaw) == 0 {
		value["evidence_raw"] = []any{}
	}
	if len(r.Evidence) == 0 {
		value["evidence"] = []any{}
	}
	if len(r.EvidenceSessions) == 0 {
		value["evidence_sessions"] = []any{}
	}
	if len(r.CandidateHits) == 0 {
		value["candidate_hits"] = []any{}
	}
	if len(r.Hits) == 0 {
		value["hits"] = []any{}
	}
	return json.Marshal(value)
}

func (r ObservationRecord) summaryObservation() Observation {
	fallback := (*AnswerFallback)(nil)
	if r.AnswerFallback != nil {
		fallback = &AnswerFallback{Triggered: r.AnswerFallback.Triggered, InitialAnswer: r.AnswerFallback.InitialAnswer}
	}
	return Observation{
		Category: r.Category, Status: r.Status, GeneratedAnswer: r.GeneratedAnswer,
		Metrics: r.Metrics, LatencyMS: r.LatencyMS, Usage: r.Usage,
		TransientRetries: r.TransientRetries, AnswerFallback: fallback,
	}
}

type EvaluationReport struct {
	Schema                         string                        `json:"schema"`
	RunID                          string                        `json:"run_id"`
	CompletedAt                    string                        `json:"completed_at"`
	QuestionCount                  int                           `json:"question_count"`
	TopK                           int                           `json:"top_k"`
	CandidateK                     int                           `json:"candidate_k"`
	AnswerK                        int                           `json:"answer_k"`
	RerankMode                     RerankMode                    `json:"rerank_mode"`
	AnswerSourceContent            bool                          `json:"answer_source_content"`
	AnswerInferenceAware           bool                          `json:"answer_inference_aware,omitempty"`
	AnswerUnknownFallbackInference bool                          `json:"answer_unknown_fallback_inference,omitempty"`
	JudgeProfile                   benchmarkprompts.JudgeProfile `json:"judge_profile"`
	Categories                     []int                         `json:"categories"`
	Metrics                        map[string]Summary            `json:"metrics"`
	Diagnostics                    Diagnostics                   `json:"diagnostics"`
	MetricNotes                    map[string]string             `json:"metric_notes"`
}

func Evaluate(ctx context.Context, dataset Dataset, operations Operations, options EvaluateOptions) (EvaluationReport, error) {
	if options.TopK < 1 || options.TopK > 50 || options.AnswerK < 1 || options.AnswerK > options.TopK {
		return EvaluationReport{}, fmt.Errorf("top_k and answer_k are invalid")
	}
	if options.Concurrency < 1 || options.OperationRetries < 1 {
		return EvaluationReport{}, fmt.Errorf("evaluation concurrency and operation_retries must be positive")
	}
	if _, err := benchmarkprompts.AnswerPolicyVersion(
		options.AnswerSourceContent, options.AnswerInferenceAware, options.AnswerUnknownFallbackInference,
	); err != nil {
		return EvaluationReport{}, err
	}
	runID, err := NormalizeRunID(options.RunID)
	if err != nil {
		return EvaluationReport{}, err
	}
	categories := slices.Clone(options.Categories)
	if len(categories) == 0 {
		categories = []int{1, 2, 3, 4}
	}
	selected := dataset.SelectedQuestions(Selection{
		Categories: categories, ConversationLimit: cloneInt(options.ConversationLimit), QuestionLimit: cloneInt(options.QuestionLimit),
	})
	if len(selected) == 0 {
		return EvaluationReport{}, fmt.Errorf("LoCoMo selection contains no questions")
	}
	observationsPath := filepath.Join(options.OutputDirectory, "observations.jsonl")
	observed, err := readObservationRecords(observationsPath)
	if err != nil {
		return EvaluationReport{}, err
	}
	pending := make([]Question, 0, len(selected))
	for _, question := range selected {
		value, exists := observed[question.ID()]
		if !exists || (options.RetryErrors && value.Status != "ok") {
			pending = append(pending, question)
		}
	}
	progress := options.Progress
	if progress == nil {
		progress = func(string) {}
	}
	progress(fmt.Sprintf("[evaluate] selected=%d resumed=%d pending=%d", len(selected), len(selected)-len(pending), len(pending)))
	if len(pending) > 0 {
		if operations == nil || options.AnswerGenerator == nil || options.JudgeGenerator == nil {
			return EvaluationReport{}, fmt.Errorf("benchmark operations and generators must not be nil while questions are pending")
		}
		if options.AnswerUnknownFallbackInference && options.FallbackAnswerGenerator == nil {
			return EvaluationReport{}, fmt.Errorf("unknown fallback requires a fallback answer generator")
		}
		entrySources, err := loadEntrySources(ctx, dataset, operations, runID, options.ConversationLimit)
		if err != nil {
			return EvaluationReport{}, err
		}
		conversations := make(map[string]Conversation)
		for _, conversation := range dataset.Conversations() {
			conversations[conversation.SampleID()] = conversation
		}
		results, err := parallelQuestions(ctx, pending, options.Concurrency, func(ctx context.Context, question Question) ObservationRecord {
			return evaluateQuestion(ctx, operations, question, conversations[question.SampleID()], entrySources[question.SampleID()], runID, options)
		})
		if err != nil {
			return EvaluationReport{}, err
		}
		for _, record := range results {
			if err := appendJSONLine(observationsPath, record); err != nil {
				return EvaluationReport{}, err
			}
			observed[record.QuestionID] = record
			progress(fmt.Sprintf("[evaluate] recorded %s status=%s", record.QuestionID, record.Status))
		}
	}
	ordered := make([]ObservationRecord, 0, len(selected))
	summaryValues := make([]Observation, 0, len(selected))
	for _, question := range selected {
		record, ok := observed[question.ID()]
		if !ok {
			return EvaluationReport{}, fmt.Errorf("evaluation did not produce every selected observation")
		}
		ordered = append(ordered, record)
		summaryValues = append(summaryValues, record.summaryObservation())
	}
	clock := options.Clock
	if clock == nil {
		clock = time.Now
	}
	report := EvaluationReport{
		Schema: "powercontext.benchmark.locomo.summary.v1", RunID: runID,
		CompletedAt: pythonUTC(clock()), QuestionCount: len(ordered),
		TopK: options.TopK, CandidateK: options.TopK, AnswerK: options.AnswerK,
		RerankMode: options.RerankMode, AnswerSourceContent: options.AnswerSourceContent,
		AnswerInferenceAware:           options.AnswerInferenceAware,
		AnswerUnknownFallbackInference: options.AnswerUnknownFallbackInference,
		JudgeProfile:                   options.JudgeProfile, Categories: categories,
		Metrics: SummarizeObservations(summaryValues), Diagnostics: DiagnoseObservations(summaryValues),
		MetricNotes: map[string]string{
			"llm_judge":          "Same configured model answers and judges; this is not an independent human label.",
			"evidence":           "Session-level Source provenance (D1, D2, ...), which is looser than LoCoMo turn-level evidence.",
			"candidate_evidence": "Candidate evidence scores the coarse retrieval pool before reranking or truncation.",
			"errors":             "Failed questions remain in the denominator and score zero.",
			"category_5":         "Excluded by the scored-set contract, which includes categories 1-4.",
		},
	}
	if err := writeJSON(filepath.Join(options.OutputDirectory, "summary.json"), report); err != nil {
		return EvaluationReport{}, err
	}
	if err := writeFileAtomic(filepath.Join(options.OutputDirectory, "summary.md"), []byte(RenderSummary(report))); err != nil {
		return EvaluationReport{}, err
	}
	return report, nil
}

// PendingEvaluationCount inspects only the immutable dataset selection and
// JSONL checkpoint. A completed run can therefore be summarized without
// opening its database or provider, matching the Python runner's resume path.
func PendingEvaluationCount(
	dataset Dataset,
	outputDirectory string,
	categories []int,
	conversationLimit, questionLimit *int,
	retryErrors bool,
) (int, error) {
	selectedCategories := slices.Clone(categories)
	if len(selectedCategories) == 0 {
		selectedCategories = DefaultSelection().Categories
	}
	selected := dataset.SelectedQuestions(Selection{
		Categories: selectedCategories, ConversationLimit: cloneInt(conversationLimit), QuestionLimit: cloneInt(questionLimit),
	})
	if len(selected) == 0 {
		return 0, fmt.Errorf("LoCoMo selection contains no questions")
	}
	observed, err := readObservationRecords(filepath.Join(outputDirectory, "observations.jsonl"))
	if err != nil {
		return 0, err
	}
	pending := 0
	for _, question := range selected {
		value, exists := observed[question.ID()]
		if !exists || (retryErrors && value.Status != "ok") {
			pending++
		}
	}
	return pending, nil
}

func evaluateQuestion(
	ctx context.Context,
	operations Operations,
	question Question,
	conversation Conversation,
	entrySources map[string][]string,
	runID string,
	options EvaluateOptions,
) (record ObservationRecord) {
	started := time.Now()
	phase := "search"
	record = ObservationRecord{
		Schema: "powercontext.benchmark.locomo.observation.v2", QuestionID: question.ID(),
		SampleID: question.SampleID(), Category: question.Category(), Question: question.Text(), GoldAnswer: question.Answer(),
		Status: "error", LatencyMS: map[string]float64{},
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			record.ErrorType = "panic"
			record.ErrorStage = phase
			record.LatencyMS["total"] = milliseconds(time.Since(started))
		}
	}()
	scopeID, err := ScopeID(runID, question.SampleID())
	if err != nil {
		return errorObservation(record, phase, started, err)
	}
	searchLimit := options.TopK
	if options.RerankMode == RerankLLM {
		searchLimit = options.AnswerK
	}
	searchStarted := time.Now()
	search, searchRetries, err := retryTransient(ctx, options.OperationRetries, func(ctx context.Context) (pcruntime.MemorySearchPage, error) {
		return operations.Search(ctx, scopeID, question.Text(), searchLimit, memory.SearchHybrid)
	})
	if err != nil {
		return errorObservation(record, phase, started, err)
	}
	searchAndRerankLatency := milliseconds(time.Since(searchStarted))
	candidateHits := search.Hits
	if search.Rerank != nil {
		candidateHits = search.Rerank.CandidateHits
	}
	dates := make(map[string]string)
	for _, session := range conversation.Sessions() {
		dates[session.ID()] = session.DateTime()
	}
	candidates := retrievedMemories(candidateHits, candidateHits, entrySources, dates)
	selectedHits := search.Hits
	if search.Rerank == nil && len(selectedHits) > options.AnswerK {
		selectedHits = selectedHits[:options.AnswerK]
	}
	memories := retrievedMemories(selectedHits, candidateHits, entrySources, dates)
	rerankLatency := 0.0
	rerankUsage := Usage{}
	if search.Rerank != nil {
		rerankLatency = search.Rerank.LatencyMS
		rerankUsage = benchmarkUsage(search.Rerank.Usage)
	}
	sourceSessions := []AnswerSourceSession{}
	if options.AnswerSourceContent {
		sourceSessions = answerSourceSessions(conversation, memories)
	}
	answerInput := AnswerInput{
		SpeakerA: conversation.SpeakerA(), SpeakerB: conversation.SpeakerB(), Question: question.Text(),
		Memories: memories, SourceSessions: sourceSessions,
	}
	phase = "answer"
	answerStarted := time.Now()
	answer, answerRetries, err := retryTransient(ctx, options.OperationRetries, func(ctx context.Context) (inference.GenerationResult[AnswerOutput], error) {
		return options.AnswerGenerator.Generate(ctx, answerInput)
	})
	if err != nil {
		return errorObservation(record, phase, started, err)
	}
	initialAnswer := strings.TrimSpace(answer.Output.Answer)
	generatedAnswer := initialAnswer
	answerLatency := milliseconds(time.Since(answerStarted))
	fallbackRetries := 0
	fallbackLatency := 0.0
	fallbackUsage := Usage{}
	fallbackTriggered := false
	if options.FallbackAnswerGenerator != nil && NormalizeAnswer(initialAnswer) == "unknown" {
		fallbackTriggered = true
		phase = "answer_fallback"
		fallbackStarted := time.Now()
		fallback, retries, fallbackErr := retryTransient(ctx, options.OperationRetries, func(ctx context.Context) (inference.GenerationResult[AnswerOutput], error) {
			return options.FallbackAnswerGenerator.Generate(ctx, answerInput)
		})
		if fallbackErr != nil {
			return errorObservation(record, phase, started, fallbackErr)
		}
		fallbackRetries = retries
		fallbackLatency = milliseconds(time.Since(fallbackStarted))
		fallbackUsage = benchmarkUsage(fallback.Usage)
		generatedAnswer = strings.TrimSpace(fallback.Output.Answer)
	}
	phase = "judge"
	judgeStarted := time.Now()
	judge, judgeRetries, err := retryTransient(ctx, options.OperationRetries, func(ctx context.Context) (inference.GenerationResult[JudgeOutput], error) {
		return options.JudgeGenerator.Generate(ctx, JudgeInput{
			Question: question.Text(), GoldAnswer: question.Answer(), GeneratedAnswer: generatedAnswer,
		})
	})
	if err != nil {
		return errorObservation(record, phase, started, err)
	}
	judgeLatency := milliseconds(time.Since(judgeStarted))
	evidence := ScoreRetrieval(question.EvidenceSessions(), sourceIDs(memories))
	candidateEvidence := ScoreRetrieval(question.EvidenceSessions(), sourceIDs(candidates))
	record.Status = "ok"
	record.GeneratedAnswer = generatedAnswer
	record.EvidenceRaw = question.EvidenceRaw()
	record.Evidence = question.Evidence()
	record.EvidenceSessions = question.EvidenceSessions()
	if search.Mode != nil {
		record.RetrievalMode = string(*search.Mode)
	}
	record.CandidateHits = candidates
	record.Hits = memories
	record.Rerank = rerankMetadata(options.RerankMode, search.Rerank, len(candidates), len(memories))
	record.AnswerContext = &AnswerContext{SourceContent: options.AnswerSourceContent, SourceIDs: answerSourceIDs(sourceSessions)}
	if options.FallbackAnswerGenerator != nil {
		record.AnswerFallback = &AnswerFallbackRecord{
			Trigger: "normalized-answer-equals-unknown", Triggered: fallbackTriggered,
			InitialAnswer: initialAnswer, Instructions: benchmarkprompts.AnswerSourceInferenceVersion,
		}
	}
	judgeScore := 0.0
	if judge.Output.Label == "CORRECT" {
		judgeScore = 1
	}
	record.Metrics = map[string]float64{
		"exact_match":      ExactMatch(generatedAnswer, question.Answer()),
		"token_f1":         TokenF1(generatedAnswer, question.Answer()),
		"reference_set_f1": SetTokenF1(generatedAnswer, question.Answer()),
		"bleu1":            BLEU1(generatedAnswer, question.Answer()), "llm_judge": judgeScore,
		"evidence_hit": evidence.EvidenceHit, "evidence_recall": evidence.EvidenceRecall, "evidence_mrr": evidence.EvidenceMRR,
		"candidate_evidence_hit":    candidateEvidence.EvidenceHit,
		"candidate_evidence_recall": candidateEvidence.EvidenceRecall,
		"candidate_evidence_mrr":    candidateEvidence.EvidenceMRR,
	}
	record.LatencyMS = map[string]float64{
		"search": max(searchAndRerankLatency-rerankLatency, 0), "rerank": rerankLatency,
		"answer": answerLatency, "judge": judgeLatency, "total": milliseconds(time.Since(started)),
	}
	if record.AnswerFallback != nil && record.AnswerFallback.Triggered {
		record.LatencyMS["answer_fallback"] = fallbackLatency
	}
	record.Usage = map[string]Usage{
		"rerank": rerankUsage, "answer": benchmarkUsage(answer.Usage), "judge": benchmarkUsage(judge.Usage),
	}
	record.TransientRetries = map[string]int{
		"search": searchRetries, "rerank": 0, "answer": answerRetries, "judge": judgeRetries,
	}
	if record.AnswerFallback != nil && record.AnswerFallback.Triggered {
		record.Usage["answer_fallback"] = fallbackUsage
		record.TransientRetries["answer_fallback"] = fallbackRetries
	}
	return record
}

func errorObservation(record ObservationRecord, phase string, started time.Time, err error) ObservationRecord {
	record.Status = "error"
	record.ErrorType = errorType(err)
	record.ErrorStage = phase
	record.LatencyMS = map[string]float64{"total": milliseconds(time.Since(started))}
	return record
}

func retrievedMemories(
	hits, candidates []memory.Hit,
	entrySources map[string][]string,
	dates map[string]string,
) []RetrievedMemory {
	ranks := make(map[string]int, len(candidates))
	for index, hit := range candidates {
		ranks[hit.EntryID+"\x00"+hit.EntryVersionID] = index + 1
	}
	result := make([]RetrievedMemory, len(hits))
	for index, hit := range hits {
		sources := slices.Clone(entrySources[hit.EntryID+"\x00"+hit.EntryVersionID])
		sourceDates := make([]string, 0, len(sources))
		for _, sourceID := range sources {
			if date, ok := dates[sourceSuffix(sourceID)]; ok {
				sourceDates = append(sourceDates, date)
			}
		}
		matched := make([]string, len(hit.MatchedBy))
		for channel, value := range hit.MatchedBy {
			matched[channel] = string(value)
		}
		result[index] = RetrievedMemory{
			Rank: index + 1, RetrievalRank: ranks[hit.EntryID+"\x00"+hit.EntryVersionID], Text: hit.Text,
			Score: hit.Score, MatchedBy: matched, SourceIDs: sources, SourceDates: sourceDates,
		}
	}
	return result
}

func rerankMetadata(mode RerankMode, trace *memory.RerankTrace, candidates, answers int) *RerankMetadata {
	result := &RerankMetadata{Mode: mode, CandidateCount: candidates, AnswerCount: answers}
	if trace == nil {
		result.SelectedRetrievalRanks = make([]int, answers)
		for index := range answers {
			result.SelectedRetrievalRanks[index] = index + 1
		}
		return result
	}
	policy := trace.PolicyID
	result.PolicyID = &policy
	result.SelectedRetrievalRanks = slices.Clone(trace.SelectedRanks)
	result.DiscardedRankCount = trace.DiscardedRankCount
	result.UsedFallback = trace.UsedFallback
	return result
}

func answerSourceSessions(conversation Conversation, memories []RetrievedMemory) []AnswerSourceSession {
	sessions := make(map[string]Session)
	for _, session := range conversation.Sessions() {
		sessions[session.ID()] = session
	}
	seen := make(map[string]struct{})
	result := make([]AnswerSourceSession, 0)
	for _, item := range memories {
		for _, sourceID := range item.SourceIDs {
			sessionID := sourceSuffix(sourceID)
			session, ok := sessions[sessionID]
			if !ok {
				continue
			}
			if _, exists := seen[sessionID]; exists {
				continue
			}
			seen[sessionID] = struct{}{}
			result = append(result, AnswerSourceSession{
				SourceID: sessionID, DateTime: session.DateTime(), Content: RenderSession(conversation, session),
			})
		}
	}
	return result
}

func loadEntrySources(
	ctx context.Context, dataset Dataset, operations Operations, runID string, conversationLimit *int,
) (map[string]map[string][]string, error) {
	conversations := dataset.Conversations()
	if conversationLimit != nil && *conversationLimit < len(conversations) {
		conversations = conversations[:max(*conversationLimit, 0)]
	}
	result := make(map[string]map[string][]string, len(conversations))
	for _, conversation := range conversations {
		scopeID, err := ScopeID(runID, conversation.SampleID())
		if err != nil {
			return nil, err
		}
		page, err := operations.List(ctx, scopeID)
		if err != nil {
			return nil, err
		}
		entries := make(map[string][]string, len(page.Entries))
		for _, record := range page.Entries {
			sources := make([]string, len(record.Entry.Sources))
			for index, ref := range record.Entry.Sources {
				// Python's benchmark freezes SourceRef.source_id, not its
				// display form ("type:id"). Keeping the bare ID also makes
				// observation files portable across Source adapter names.
				sources[index] = ref.ID()
			}
			entries[record.Entry.EntryID+"\x00"+record.Entry.EntryVersionID] = sources
		}
		result[conversation.SampleID()] = entries
	}
	return result, nil
}

func retryTransient[T any](ctx context.Context, attempts int, operation func(context.Context) (T, error)) (T, int, error) {
	var zero T
	for attempt := 1; attempt <= attempts; attempt++ {
		value, err := operation(ctx)
		if err == nil {
			return value, attempt - 1, nil
		}
		if !isTransientInference(err) || attempt == attempts {
			return zero, attempt - 1, err
		}
		delay := time.Second << (attempt - 1)
		if delay > 8*time.Second {
			delay = 8 * time.Second
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return zero, attempt - 1, ctx.Err()
		case <-timer.C:
		}
	}
	return zero, attempts, errors.New("unreachable benchmark retry state")
}

func isTransientInference(err error) bool {
	var unavailable *inference.UnavailableError
	var timeout *inference.TimeoutError
	return errors.As(err, &unavailable) || errors.As(err, &timeout)
}

func parallelConversations(
	ctx context.Context,
	values []Conversation,
	concurrency int,
	operation func(context.Context, Conversation) (ConversationIngestion, error),
) ([]ConversationIngestion, error) {
	type job struct {
		index int
		value Conversation
	}
	jobs := make(chan job)
	result := make([]ConversationIngestion, len(values))
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	var workers sync.WaitGroup
	for range min(concurrency, max(len(values), 1)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			defer func() {
				if recovered := recover(); recovered != nil {
					cancel(fmt.Errorf("LoCoMo ingestion worker panicked: %v", recovered))
				}
			}()
			for item := range jobs {
				value, err := operation(ctx, item.value)
				if err != nil {
					cancel(err)
					return
				}
				result[item.index] = value
			}
		}()
	}
sendConversations:
	for index, value := range values {
		select {
		case jobs <- job{index: index, value: value}:
		case <-ctx.Done():
			break sendConversations
		}
	}
	close(jobs)
	workers.Wait()
	if err := context.Cause(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

func parallelQuestions(
	ctx context.Context,
	values []Question,
	concurrency int,
	operation func(context.Context, Question) ObservationRecord,
) ([]ObservationRecord, error) {
	type job struct {
		index int
		value Question
	}
	jobs := make(chan job)
	result := make([]ObservationRecord, len(values))
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	var workers sync.WaitGroup
	for range min(concurrency, max(len(values), 1)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			defer func() {
				if recovered := recover(); recovered != nil {
					cancel(fmt.Errorf("LoCoMo evaluation worker panicked: %v", recovered))
				}
			}()
			for item := range jobs {
				result[item.index] = operation(ctx, item.value)
			}
		}()
	}
sendQuestions:
	for index, value := range values {
		select {
		case jobs <- job{index: index, value: value}:
		case <-ctx.Done():
			break sendQuestions
		}
	}
	close(jobs)
	workers.Wait()
	if err := context.Cause(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

func readObservationRecords(path string) (map[string]ObservationRecord, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]ObservationRecord{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open benchmark observations: %w", err)
	}
	defer file.Close()
	result := make(map[string]ObservationRecord)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), maxObservationBytes)
	for line := 1; scanner.Scan(); line++ {
		if len(strings.TrimSpace(scanner.Text())) == 0 {
			continue
		}
		var value ObservationRecord
		if err := json.Unmarshal(scanner.Bytes(), &value); err != nil {
			return nil, fmt.Errorf("decode benchmark observation line %d: %w", line, err)
		}
		if value.QuestionID == "" {
			return nil, fmt.Errorf("benchmark observation line %d has no question_id", line)
		}
		result[value.QuestionID] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read benchmark observations: %w", err)
	}
	return result, nil
}

func appendJSONLine(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	encoded, err := compactJSON(value)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		return err
	}
	return file.Sync()
}

func compactJSON(value any) ([]byte, error) {
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(output.Bytes(), []byte{'\n'}), nil
}

func writeJSON(path string, value any) error {
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return err
	}
	return writeFileAtomic(path, output.Bytes())
}

func writeFileAtomic(path string, contents []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".powercontext-locomo-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func benchmarkUsage(value inference.Usage) Usage {
	result := Usage{Requests: value.Requests}
	if value.InputTokens != nil {
		result.InputTokens = int(*value.InputTokens)
	}
	if value.OutputTokens != nil {
		result.OutputTokens = int(*value.OutputTokens)
	}
	return result
}

func sourceIDs(values []RetrievedMemory) [][]string {
	result := make([][]string, len(values))
	for index, value := range values {
		result[index] = slices.Clone(value.SourceIDs)
	}
	return result
}

func answerSourceIDs(values []AnswerSourceSession) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = value.SourceID
	}
	return result
}

func sourceSuffix(value string) string {
	if index := strings.LastIndexByte(value, ':'); index >= 0 {
		return value[index+1:]
	}
	return value
}

func errorType(err error) string {
	if err == nil {
		return ""
	}
	typeOf := reflect.TypeOf(err)
	for typeOf.Kind() == reflect.Pointer {
		typeOf = typeOf.Elem()
	}
	if typeOf.Name() != "" {
		return typeOf.Name()
	}
	return "error"
}

func equalInt64Pointer(left, right *int64) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func milliseconds(value time.Duration) float64 { return float64(value) / float64(time.Millisecond) }

func pythonUTC(value time.Time) string {
	value = value.UTC().Truncate(time.Microsecond)
	if value.Nanosecond() == 0 {
		return value.Format("2006-01-02T15:04:05+00:00")
	}
	return value.Format("2006-01-02T15:04:05.000000+00:00")
}

func sortedMetricGroups(values map[string]Summary) []string {
	result := make([]string, 0, len(values))
	for name := range values {
		if strings.HasPrefix(name, "category_") {
			result = append(result, name)
		}
	}
	sort.Strings(result)
	return result
}
