package locomo_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ob-labs/powercontext-go/artifact/memory/prompts"
	"github.com/ob-labs/powercontext-go/internal/benchmark/locomo"
	benchmarkprompts "github.com/ob-labs/powercontext-go/internal/benchmark/locomo/prompts"
)

func loadDataset(t *testing.T) locomo.Dataset {
	t.Helper()
	dataset, err := locomo.Load(filepath.Join("testdata", "locomo10.json"))
	if err != nil {
		t.Fatal(err)
	}
	return dataset
}

func publicConfiguration() locomo.PublicConfiguration {
	return locomo.PublicConfiguration{
		DatabaseKind: "sqlite", GenerationModel: "openai:test-chat", EmbeddingModel: "openai:test-embedding",
		EmbeddingProfileID: "test-3-unit", EmbeddingDimension: 3, EmbeddingNormalization: "unit",
		EmbeddingBatchSize: 10, MemoryExtractionProfile: "conversation",
		MemoryExtractionInstructions: prompts.ConversationVersion,
	}
}

func TestCanonicalLoCoMoDatasetHasExpectedShapeAndScoredSelection(t *testing.T) {
	dataset := loadDataset(t)
	if dataset.SHA256() != "4448275ea2c5cd0af5774d80aea7b05b5a16e1b996caf8554ca3d762a301ae84" {
		t.Fatalf("unexpected dataset digest %q", dataset.SHA256())
	}
	if len(dataset.Conversations()) != 10 || len(dataset.Sessions()) != 272 || len(dataset.Questions()) != 1_986 {
		t.Fatalf("unexpected shape: conversations=%d sessions=%d questions=%d", len(dataset.Conversations()), len(dataset.Sessions()), len(dataset.Questions()))
	}
	turns := 0
	for _, session := range dataset.Sessions() {
		turns += len(session.Turns())
	}
	if turns != 5_882 {
		t.Fatalf("unexpected turn count %d", turns)
	}
	if got := len(dataset.SelectedQuestions(locomo.DefaultSelection())); got != 1_540 {
		t.Fatalf("unexpected scored question count %d", got)
	}
	if got, err := locomo.PendingEvaluationCount(dataset, t.TempDir(), nil, nil, nil, false); err != nil || got != 1_540 {
		t.Fatalf("default pending evaluation count = %d, %v", got, err)
	}
	for _, question := range dataset.Questions() {
		if question.Text() == "What did Melanie paint recently?" {
			if !reflect.DeepEqual(question.EvidenceRaw(), []string{"D8:6; D9:17"}) || !reflect.DeepEqual(question.Evidence(), []string{"D8:6", "D9:17"}) {
				t.Fatalf("compound evidence mismatch: raw=%v normalized=%v", question.EvidenceRaw(), question.Evidence())
			}
			return
		}
	}
	t.Fatal("canonical composite question not found")
}

func TestSessionSourceContainsDialogueAndDateWithoutQAAnnotations(t *testing.T) {
	dataset := loadDataset(t)
	conversation := dataset.Conversations()[0]
	content := locomo.RenderSession(conversation, conversation.Sessions()[0])
	for _, expected := range []string{
		"Date and time: 1:56 pm on 8 May, 2023",
		"[D1:3] Caroline: I went to a LGBTQ support group yesterday",
	} {
		if !strings.Contains(content, expected) {
			t.Fatalf("rendered Source does not contain %q", expected)
		}
	}
	for _, forbidden := range []string{"When did Caroline go to the LGBTQ support group?", "Gold answer"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("rendered Source leaked %q", forbidden)
		}
	}
	if strings.Contains(strings.ToLower(content), "evidence") {
		t.Fatal("rendered Source leaked evidence annotation")
	}
}

func TestAnswerMetricsAndSessionProvenanceAreDeterministic(t *testing.T) {
	if locomo.ExactMatch("The shell necklace.", "shell necklace") != 1 {
		t.Fatal("exact match differs from Oracle")
	}
	if math.Abs(locomo.TokenF1("a shell necklace from Hawaii", "shell necklace")-2.0/3.0) > 1e-12 {
		t.Fatal("token F1 differs from Oracle")
	}
	if locomo.SetTokenF1("red red dress", "red dress") != 1 {
		t.Fatal("set token F1 differs from Oracle")
	}
	if locomo.BLEU1("shell necklace", "a shell necklace") <= .5 {
		t.Fatal("BLEU-1 differs from Oracle")
	}
	if got := locomo.NormalizeAnswer("A\u001cTHE\u00a0café!"); got != "café" {
		t.Fatalf("Unicode normalization = %q, want café", got)
	}
	got := locomo.ScoreRetrieval([]string{"D1", "D3"}, [][]string{{"D8"}, {"D3"}, {"D1"}})
	want := locomo.RetrievalMetrics{EvidenceHit: 1, EvidenceRecall: 1, EvidenceMRR: .5}
	if got != want {
		t.Fatalf("retrieval metrics = %+v, want %+v", got, want)
	}
}

func TestSummaryKeepsErrorsInAccuracyDenominator(t *testing.T) {
	perfect := map[string]float64{
		"exact_match": 1, "token_f1": 1, "reference_set_f1": 1, "bleu1": 1, "llm_judge": 1,
		"evidence_hit": 1, "evidence_recall": 1, "evidence_mrr": 1,
		"candidate_evidence_hit": 1, "candidate_evidence_recall": 1, "candidate_evidence_mrr": 1,
	}
	observations := []locomo.Observation{
		{Category: 1, Status: "ok", Metrics: perfect, LatencyMS: map[string]float64{"search": 10, "rerank": 1, "answer": 20, "judge": 30, "total": 60}},
		{Category: 1, Status: "error", LatencyMS: map[string]float64{"total": 40}},
	}
	summary := locomo.SummarizeObservations(observations)["overall"]
	if summary.QuestionCount != 2 || summary.ErrorCount != 1 || summary.Metrics["llm_judge"] != .5 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if summary.LatencyP50["total"] == nil || *summary.LatencyP50["total"] != 60 {
		t.Fatalf("unexpected total p50: %v", summary.LatencyP50["total"])
	}
	diagnostics := locomo.DiagnoseObservations(observations)
	if diagnostics.RetrievalConditioned["hit"].LLMJudgeAccuracy != 1 || diagnostics.WrongAnswerCount != 0 {
		t.Fatalf("unexpected diagnostics: %+v", diagnostics)
	}
}

func TestDiagnosticsReportUnknownFallbackQualityAndCost(t *testing.T) {
	observations := []locomo.Observation{
		{
			Status: "ok", GeneratedAnswer: "supported conclusion", AnswerFallback: &locomo.AnswerFallback{Triggered: true, InitialAnswer: "Unknown"},
			Metrics:          map[string]float64{"evidence_hit": 1, "evidence_mrr": 1, "llm_judge": 1},
			Usage:            map[string]locomo.Usage{"answer_fallback": {Requests: 1, InputTokens: 20, OutputTokens: 2}},
			TransientRetries: map[string]int{"answer_fallback": 1},
		},
		{
			Status: "ok", GeneratedAnswer: "Unknownish", AnswerFallback: &locomo.AnswerFallback{InitialAnswer: "Unknownish"},
			Metrics: map[string]float64{"evidence_hit": 0, "evidence_mrr": 0, "llm_judge": 0},
		},
	}
	diagnostics := locomo.DiagnoseObservations(observations)
	want := &locomo.FallbackDiagnostics{
		Trigger: "normalized-answer-equals-unknown", TriggeredCount: 1, TriggeredRate: .5,
		ResolvedCount: 1, ResolvedRate: 1, LLMJudgeAccuracy: 1,
	}
	if !reflect.DeepEqual(diagnostics.AnswerFallback, want) {
		t.Fatalf("fallback diagnostics = %+v, want %+v", diagnostics.AnswerFallback, want)
	}
	if got := diagnostics.ModelUsage["answer_fallback"]; got != (locomo.Usage{Requests: 1, InputTokens: 20, OutputTokens: 2}) {
		t.Fatalf("fallback usage = %+v", got)
	}
	if diagnostics.TransientRetries["answer_fallback"] != 1 {
		t.Fatalf("fallback retry count = %d", diagnostics.TransientRetries["answer_fallback"])
	}
}

func TestRunManifestIsStableAndExcludesDatabaseCredentials(t *testing.T) {
	dataset := loadDataset(t)
	options := locomo.RunOptions{
		RunID: "smoke / test", OutputDirectory: t.TempDir(), TopK: 30,
		RerankMode: locomo.RerankNone, JudgeProfile: benchmarkprompts.StrictJudge,
		Categories: []int{1, 2, 3, 4}, ConversationLimit: intPointer(1), QuestionLimit: intPointer(5),
		OperationRetries: 3, Configuration: publicConfiguration(),
	}
	first, err := locomo.PrepareRun(dataset, options)
	if err != nil {
		t.Fatal(err)
	}
	second, err := locomo.PrepareRun(dataset, options)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("repeated manifest preparation is not stable")
	}
	if first.RunID != "smoke-test" || first.Configuration.MemoryExtractionProfile != "conversation" || first.Configuration.MemoryExtractionInstructions != prompts.ConversationVersion {
		t.Fatalf("unexpected public identity: %+v", first)
	}
	if first.CandidateK != 30 || first.AnswerK != 30 || first.RerankMode != locomo.RerankNone || first.AnswerSourceContent || first.Schema != "powercontext.benchmark.locomo.run.v5" || first.GenerationTemperature != 0 {
		t.Fatalf("unexpected baseline policy: %+v", first)
	}
	payload, _ := json.Marshal(first)
	if strings.Contains(string(payload), "secret") || strings.Contains(string(payload), "database_url") {
		t.Fatalf("manifest leaked database credentials: %s", payload)
	}
	normalized, err := locomo.NormalizeRunID("  a/b c  ")
	if err != nil || normalized != "a-b-c" {
		t.Fatalf("normalize run ID = %q, %v", normalized, err)
	}
	scope, err := locomo.ScopeID("smoke / test", "conv-26")
	if err != nil || scope != "benchmark:locomo:smoke-test:conv-26" {
		t.Fatalf("scope = %q, %v", scope, err)
	}
}

func TestRunManifestRecordsInferenceAwareAnswerPolicy(t *testing.T) {
	dataset := loadDataset(t)
	options := locomo.RunOptions{
		RunID: "inference-aware", OutputDirectory: t.TempDir(), TopK: 30, AnswerK: intPointer(10),
		RerankMode: locomo.RerankNone, AnswerSourceContent: true, AnswerInferenceAware: true,
		JudgeProfile: benchmarkprompts.StrictJudge, Categories: []int{3}, OperationRetries: 3,
		Configuration: publicConfiguration(),
	}
	manifest, err := locomo.PrepareRun(dataset, options)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Schema != "powercontext.benchmark.locomo.run.v6" || !manifest.AnswerInferenceAware || manifest.AnswerInstructions != benchmarkprompts.AnswerSourceInferenceVersion {
		t.Fatalf("unexpected inference-aware manifest: %+v", manifest)
	}
	options.OutputDirectory = t.TempDir()
	options.AnswerSourceContent = false
	if _, err := locomo.PrepareRun(dataset, options); err == nil || !strings.Contains(err.Error(), "requires Source expansion") {
		t.Fatalf("invalid inference-aware mode error = %v", err)
	}
}

func TestRunManifestRecordsUnknownFallbackInferencePolicy(t *testing.T) {
	dataset := loadDataset(t)
	options := locomo.RunOptions{
		RunID: "unknown-fallback-inference", OutputDirectory: t.TempDir(), TopK: 30, AnswerK: intPointer(10),
		RerankMode: locomo.RerankNone, AnswerSourceContent: true, AnswerUnknownFallbackInference: true,
		JudgeProfile: benchmarkprompts.StrictJudge, Categories: []int{1, 2, 3, 4}, OperationRetries: 3,
		Configuration: publicConfiguration(),
	}
	manifest, err := locomo.PrepareRun(dataset, options)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Schema != "powercontext.benchmark.locomo.run.v7" || !manifest.AnswerUnknownFallbackInference || manifest.AnswerInstructions != benchmarkprompts.AnswerSourceUnknownFallbackVersion || manifest.AnswerFallbackTrigger != "normalized-answer-equals-unknown" || manifest.DirectAnswerInstructions != benchmarkprompts.AnswerSourceVersion || manifest.FallbackAnswerInstructions != benchmarkprompts.AnswerSourceInferenceVersion {
		t.Fatalf("unexpected fallback manifest: %+v", manifest)
	}
	options.OutputDirectory = t.TempDir()
	options.AnswerSourceContent = false
	if _, err := locomo.PrepareRun(dataset, options); err == nil || !strings.Contains(err.Error(), "requires Source expansion") {
		t.Fatalf("invalid fallback mode error = %v", err)
	}
}

func TestRejudgeManifestFreezesAnswersAndRecordsIndependentJudge(t *testing.T) {
	dataset := loadDataset(t)
	question := dataset.SelectedQuestions(locomo.Selection{Categories: []int{1, 2, 3, 4}, QuestionLimit: intPointer(1)})[0]
	root := t.TempDir()
	sourceDirectory := filepath.Join(root, "source")
	outputDirectory := filepath.Join(root, "rejudge")
	if err := os.Mkdir(sourceDirectory, 0o750); err != nil {
		t.Fatal(err)
	}
	sourceManifest := map[string]any{
		"schema": "powercontext.benchmark.locomo.run.v5", "run_id": "source-run", "dataset_sha256": dataset.SHA256(),
		"selected_question_count": 1, "categories": []int{1, 2, 3, 4}, "conversation_limit": nil, "question_limit": 1,
		"top_k": 30, "candidate_k": 30, "answer_k": 10, "rerank_mode": "none", "rerank_instructions": nil,
		"answer_source_content": true, "answer_instructions": benchmarkprompts.AnswerSourceVersion,
		"judge_profile": "strict", "judge_instructions": "old-judge",
		"configuration": map[string]any{"generation_model": "openai:answer-model", "memory_extraction_profile": "conversation"},
	}
	writeJSON(t, filepath.Join(sourceDirectory, "run.json"), sourceManifest)
	observation := map[string]any{
		"schema": "powercontext.benchmark.locomo.observation.v2", "question_id": question.ID(),
		"sample_id": question.SampleID(), "category": question.Category(), "question": question.Text(),
		"gold_answer": question.Answer(), "generated_answer": "frozen answer", "status": "ok",
		"metrics": map[string]float64{"llm_judge": 0},
	}
	payload, _ := json.Marshal(observation)
	if err := os.WriteFile(filepath.Join(sourceDirectory, "observations.jsonl"), append(payload, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	options := locomo.RejudgeOptions{
		SourceDirectory: sourceDirectory, OutputDirectory: outputDirectory, RunID: "qwen topical judge",
		JudgeModel: "openai:qwen3.7-plus", JudgeProfile: benchmarkprompts.TopicalJudge, OperationRetries: 3,
	}
	manifest, err := locomo.PrepareRejudge(dataset, options)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.RunID != "qwen-topical-judge" || manifest.Source.AnswerModel != "openai:answer-model" || manifest.Source.AnswerContract["answer_k"] != json.Number("10") || manifest.Source.AnswerContract["answer_source_content"] != true {
		t.Fatalf("unexpected frozen answer contract: %+v", manifest)
	}
	if _, ok := manifest.Source.AnswerContract["answer_inference_aware"]; ok {
		t.Fatal("absent inference-aware field was synthesized")
	}
	if _, ok := manifest.Source.AnswerContract["answer_unknown_fallback_inference"]; ok {
		t.Fatal("absent fallback field was synthesized")
	}
	if manifest.JudgeModel != "openai:qwen3.7-plus" || manifest.JudgeProfile != benchmarkprompts.TopicalJudge || manifest.JudgeInstructions != benchmarkprompts.TopicalJudgeVersion || len(manifest.Source.ObservationsSHA256) != 64 {
		t.Fatalf("unexpected independent judge identity: %+v", manifest)
	}
	options.JudgeModel = "openai:different-judge"
	if _, err := locomo.PrepareRejudge(dataset, options); err == nil || !strings.Contains(err.Error(), "rejudge manifest does not match") {
		t.Fatalf("rejudge drift error = %v", err)
	}
}

func TestInferenceAwareAnswerPolicyIsExplicitAndRequiresSourceContent(t *testing.T) {
	prompt, version, err := benchmarkprompts.AnswerInstructions(true, true)
	if err != nil {
		t.Fatal(err)
	}
	if version != benchmarkprompts.AnswerSourceInferenceVersion || !strings.Contains(prompt, `Do not answer "Unknown"`) || !strings.Contains(prompt, "because the conclusion is implicit") || !strings.Contains(prompt, "one decisive supporting fact") {
		t.Fatal("inference-aware answer Prompt differs from frozen Oracle")
	}
	if _, _, err := benchmarkprompts.AnswerInstructions(false, true); err == nil || !strings.Contains(err.Error(), "requires Source expansion") {
		t.Fatalf("invalid inference policy error = %v", err)
	}
}

func TestUnknownFallbackAnswerPolicyIsExplicitAndMutuallyExclusive(t *testing.T) {
	version, err := benchmarkprompts.AnswerPolicyVersion(true, false, true)
	if err != nil || version != benchmarkprompts.AnswerSourceUnknownFallbackVersion {
		t.Fatalf("fallback version = %q, %v", version, err)
	}
	if _, err := benchmarkprompts.AnswerPolicyVersion(false, false, true); err == nil || !strings.Contains(err.Error(), "requires Source expansion") {
		t.Fatalf("invalid fallback policy error = %v", err)
	}
	if _, err := benchmarkprompts.AnswerPolicyVersion(true, true, true); err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("mutually exclusive policy error = %v", err)
	}
}

func TestJudgeProfilesKeepStrictAndTopicalContractsDistinct(t *testing.T) {
	strict, strictVersion, err := benchmarkprompts.JudgeInstructions(benchmarkprompts.StrictJudge)
	if err != nil {
		t.Fatal(err)
	}
	topical, topicalVersion, err := benchmarkprompts.JudgeInstructions(benchmarkprompts.TopicalJudge)
	if err != nil {
		t.Fatal(err)
	}
	if strictVersion != benchmarkprompts.JudgeVersion || topicalVersion != benchmarkprompts.TopicalJudgeVersion || !strings.Contains(strict, "unsupported additions") || !strings.Contains(topical, "touches on the same answer topic") || strict == topical {
		t.Fatal("judge Prompt profiles do not preserve the frozen distinction")
	}
}

func TestFrozenLoCoMoPromptHashes(t *testing.T) {
	tests := map[string]struct {
		value string
		want  string
	}{
		"answer":                  {benchmarkprompts.Answer(), "1bc206f38cbf3ca53a0cd6a7ff3b4fb419ee8f83bc1ab6b11ecc704e65f68e77"},
		"answer_source":           {benchmarkprompts.AnswerSource(), "72258d88e03ce50877fd606b2b55be4f8c84069b7c31aa84452636b5f7ea64b8"},
		"answer_source_inference": {benchmarkprompts.AnswerSourceInference(), "bdd49aa6e8adc54a2e1fe1911ef374b4bfeae7f2715b9029ab62622a37136cc2"},
		"judge":                   {benchmarkprompts.Judge(), "6f57a1f530380e3ce357b5a2f72e6bc45a1cb2c380875f30f4b15870c0bcf780"},
		"judge_topical":           {benchmarkprompts.JudgeTopical(), "6ce3ccd9fc54eb005557c3272ceafc6078ca534e440eae82604aea5888f1afe4"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			digest := sha256.Sum256([]byte(test.value))
			if got := hex.EncodeToString(digest[:]); got != test.want {
				t.Fatalf("Prompt digest = %s, want %s", got, test.want)
			}
		})
	}
}

func intPointer(value int) *int { return &value }

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
}
