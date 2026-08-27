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

package locomo

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ob-labs/powercontext-go/artifact/memory"
	"github.com/ob-labs/powercontext-go/inference"
	benchmarkprompts "github.com/ob-labs/powercontext-go/internal/benchmark/locomo/prompts"
	pcruntime "github.com/ob-labs/powercontext-go/internal/runtime"
)

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
