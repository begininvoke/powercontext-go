package locomo

import (
	"math"
	"slices"
	"sort"
	"strings"
	"unicode"
)

func NormalizeAnswer(value string) string {
	var normalized strings.Builder
	for _, character := range strings.ToLower(value) {
		if unicode.IsLetter(character) || unicode.IsNumber(character) || unicode.IsSpace(character) {
			normalized.WriteRune(character)
		} else {
			normalized.WriteByte(' ')
		}
	}
	words := strings.Fields(normalized.String())
	result := words[:0]
	for _, word := range words {
		if word != "a" && word != "an" && word != "the" {
			result = append(result, word)
		}
	}
	return strings.Join(result, " ")
}

func TokenF1(prediction, reference string) float64 {
	predicted := strings.Fields(NormalizeAnswer(prediction))
	expected := strings.Fields(NormalizeAnswer(reference))
	if len(predicted) == 0 || len(expected) == 0 {
		if len(predicted) == len(expected) {
			return 1
		}
		return 0
	}
	overlap := multisetOverlap(predicted, expected)
	if overlap == 0 {
		return 0
	}
	precision := float64(overlap) / float64(len(predicted))
	recall := float64(overlap) / float64(len(expected))
	return 2 * precision * recall / (precision + recall)
}

func SetTokenF1(prediction, reference string) float64 {
	predicted := stringSet(simpleTokens(prediction))
	expected := stringSet(simpleTokens(reference))
	if len(predicted) == 0 || len(expected) == 0 {
		if len(predicted) == len(expected) {
			return 1
		}
		return 0
	}
	overlap := 0
	for token := range predicted {
		if _, ok := expected[token]; ok {
			overlap++
		}
	}
	if overlap == 0 {
		return 0
	}
	precision := float64(overlap) / float64(len(predicted))
	recall := float64(overlap) / float64(len(expected))
	return 2 * precision * recall / (precision + recall)
}

func ExactMatch(prediction, reference string) float64 {
	if NormalizeAnswer(prediction) == NormalizeAnswer(reference) {
		return 1
	}
	return 0
}

func BLEU1(prediction, reference string) float64 {
	predicted := wordTokens(prediction)
	expected := wordTokens(reference)
	if len(predicted) == 0 || len(expected) == 0 {
		return 0
	}
	precision := float64(multisetOverlap(predicted, expected)) / float64(len(predicted))
	if precision == 0 {
		return 0
	}
	brevityPenalty := 1.0
	if len(predicted) <= len(expected) {
		brevityPenalty = math.Exp(1 - float64(len(expected))/float64(len(predicted)))
	}
	return brevityPenalty * precision
}

type RetrievalMetrics struct {
	EvidenceHit    float64 `json:"evidence_hit"`
	EvidenceRecall float64 `json:"evidence_recall"`
	EvidenceMRR    float64 `json:"evidence_mrr"`
}

func ScoreRetrieval(evidenceSessions []string, hitSourceIDs [][]string) RetrievalMetrics {
	expected := stringSet(evidenceSessions)
	if len(expected) == 0 {
		return RetrievalMetrics{}
	}
	observed := make(map[string]struct{})
	reciprocalRank := 0.0
	for index, sourceIDs := range hitSourceIDs {
		matched := false
		for _, sourceID := range sourceIDs {
			session := sourceSession(sourceID)
			if _, ok := expected[session]; ok {
				observed[session] = struct{}{}
				matched = true
			}
		}
		if matched && reciprocalRank == 0 {
			reciprocalRank = 1 / float64(index+1)
		}
	}
	hit := 0.0
	if len(observed) != 0 {
		hit = 1
	}
	return RetrievalMetrics{
		EvidenceHit: hit, EvidenceRecall: float64(len(observed)) / float64(len(expected)), EvidenceMRR: reciprocalRank,
	}
}

type Usage struct {
	Requests     int `json:"requests"`
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type AnswerFallback struct {
	Triggered     bool   `json:"triggered"`
	InitialAnswer string `json:"initial_answer"`
}

type Observation struct {
	Category         int
	Status           string
	GeneratedAnswer  string
	Metrics          map[string]float64
	LatencyMS        map[string]float64
	Usage            map[string]Usage
	TransientRetries map[string]int
	AnswerFallback   *AnswerFallback
}

type Summary struct {
	QuestionCount  int                 `json:"question_count"`
	CompletedCount int                 `json:"completed_count"`
	ErrorCount     int                 `json:"error_count"`
	Metrics        map[string]float64  `json:"metrics"`
	LatencyP50     map[string]*float64 `json:"latency_ms_p50"`
	LatencyP95     map[string]*float64 `json:"latency_ms_p95"`
}

var summaryMetricNames = []string{
	"exact_match", "token_f1", "reference_set_f1", "bleu1", "llm_judge",
	"evidence_hit", "evidence_recall", "evidence_mrr",
	"candidate_evidence_hit", "candidate_evidence_recall", "candidate_evidence_mrr",
}

func SummarizeObservations(observations []Observation) map[string]Summary {
	values := slices.Clone(observations)
	result := map[string]Summary{"overall": summarizeGroup(values)}
	categories := make(map[int]struct{})
	for _, observation := range values {
		categories[observation.Category] = struct{}{}
	}
	ordered := make([]int, 0, len(categories))
	for category := range categories {
		ordered = append(ordered, category)
	}
	sort.Ints(ordered)
	for _, category := range ordered {
		group := make([]Observation, 0)
		for _, observation := range values {
			if observation.Category == category {
				group = append(group, observation)
			}
		}
		result["category_"+itoa(category)] = summarizeGroup(group)
	}
	return result
}

type ConditionSummary struct {
	Count             int     `json:"count"`
	LLMJudgeAccuracy  float64 `json:"llm_judge_accuracy"`
	UnknownAnswerRate float64 `json:"unknown_answer_rate"`
}

type FallbackDiagnostics struct {
	Trigger          string  `json:"trigger"`
	TriggeredCount   int     `json:"triggered_count"`
	TriggeredRate    float64 `json:"triggered_rate"`
	ResolvedCount    int     `json:"resolved_count"`
	ResolvedRate     float64 `json:"resolved_rate"`
	LLMJudgeAccuracy float64 `json:"llm_judge_accuracy"`
}

type Diagnostics struct {
	UnknownAnswerCount        int                         `json:"unknown_answer_count"`
	UnknownAnswerRate         float64                     `json:"unknown_answer_rate"`
	RetrievalConditioned      map[string]ConditionSummary `json:"retrieval_conditioned"`
	WrongAnswerCount          int                         `json:"wrong_answer_count"`
	WrongWithEvidenceHitCount int                         `json:"wrong_with_evidence_hit_count"`
	WrongWithEvidenceHitRate  float64                     `json:"wrong_with_evidence_hit_rate"`
	EvidenceRankBuckets       map[string]ConditionSummary `json:"evidence_rank_buckets"`
	TransientRetries          map[string]int              `json:"transient_retries"`
	ModelUsage                map[string]Usage            `json:"model_usage"`
	AnswerFallback            *FallbackDiagnostics        `json:"answer_fallback,omitempty"`
}

func DiagnoseObservations(observations []Observation) Diagnostics {
	completed := make([]Observation, 0, len(observations))
	for _, observation := range observations {
		if observation.Status == "ok" {
			completed = append(completed, observation)
		}
	}
	hit, miss, wrong := make([]Observation, 0), make([]Observation, 0), make([]Observation, 0)
	hasFallback := false
	unknownCount := 0
	for _, observation := range completed {
		if observation.Metrics["evidence_hit"] == 1 {
			hit = append(hit, observation)
		} else if observation.Metrics["evidence_hit"] == 0 {
			miss = append(miss, observation)
		}
		if observation.Metrics["llm_judge"] == 0 {
			wrong = append(wrong, observation)
		}
		if NormalizeAnswer(observation.GeneratedAnswer) == "unknown" {
			unknownCount++
		}
		hasFallback = hasFallback || observation.AnswerFallback != nil
	}
	retryPhases := []string{"search", "rerank", "answer", "judge"}
	usageStages := []string{"rerank", "answer", "judge"}
	if hasFallback {
		retryPhases = append(retryPhases, "answer_fallback")
		usageStages = append(usageStages, "answer_fallback")
	}
	retries := make(map[string]int, len(retryPhases))
	for _, phase := range retryPhases {
		for _, observation := range completed {
			retries[phase] += observation.TransientRetries[phase]
		}
	}
	usage := make(map[string]Usage, len(usageStages))
	for _, stage := range usageStages {
		for _, observation := range completed {
			value := usage[stage]
			current := observation.Usage[stage]
			value.Requests += current.Requests
			value.InputTokens += current.InputTokens
			value.OutputTokens += current.OutputTokens
			usage[stage] = value
		}
	}
	buckets := map[string]func(float64) bool{
		"rank_1":     func(score float64) bool { return score == 1 },
		"rank_2_5":   func(score float64) bool { return score >= .2 && score < 1 },
		"rank_6_10":  func(score float64) bool { return score >= .1 && score < .2 },
		"rank_11_30": func(score float64) bool { return score > 0 && score < .1 },
		"miss":       func(score float64) bool { return score == 0 },
	}
	rankBuckets := make(map[string]ConditionSummary, len(buckets))
	for name, predicate := range buckets {
		group := make([]Observation, 0)
		for _, observation := range completed {
			if predicate(observation.Metrics["evidence_mrr"]) {
				group = append(group, observation)
			}
		}
		rankBuckets[name] = conditionSummary(group)
	}
	wrongWithHit := 0
	for _, observation := range wrong {
		if observation.Metrics["evidence_hit"] == 1 {
			wrongWithHit++
		}
	}
	result := Diagnostics{
		UnknownAnswerCount:   unknownCount,
		UnknownAnswerRate:    rate(float64(unknownCount), len(observations)),
		RetrievalConditioned: map[string]ConditionSummary{"hit": conditionSummary(hit), "miss": conditionSummary(miss)},
		WrongAnswerCount:     len(wrong), WrongWithEvidenceHitCount: wrongWithHit,
		WrongWithEvidenceHitRate: rate(float64(wrongWithHit), len(wrong)),
		EvidenceRankBuckets:      rankBuckets, TransientRetries: retries, ModelUsage: usage,
	}
	if hasFallback {
		triggered := make([]Observation, 0)
		for _, observation := range completed {
			if observation.AnswerFallback != nil && observation.AnswerFallback.Triggered {
				triggered = append(triggered, observation)
			}
		}
		resolved := 0
		judgeScore := 0.0
		for _, observation := range triggered {
			if NormalizeAnswer(observation.GeneratedAnswer) != "unknown" {
				resolved++
			}
			judgeScore += observation.Metrics["llm_judge"]
		}
		result.AnswerFallback = &FallbackDiagnostics{
			Trigger: "normalized-answer-equals-unknown", TriggeredCount: len(triggered),
			TriggeredRate: rate(float64(len(triggered)), len(completed)), ResolvedCount: resolved,
			ResolvedRate: rate(float64(resolved), len(triggered)), LLMJudgeAccuracy: rate(judgeScore, len(triggered)),
		}
	}
	return result
}

func Percentile(values []float64, percentage float64) *float64 {
	if len(values) == 0 {
		return nil
	}
	ordered := slices.Clone(values)
	sort.Float64s(ordered)
	if len(ordered) == 1 {
		return &ordered[0]
	}
	position := float64(len(ordered)-1) * percentage
	lower := int(math.Floor(position))
	upper := int(math.Ceil(position))
	result := ordered[lower]
	if lower != upper {
		result += (ordered[upper] - ordered[lower]) * (position - float64(lower))
	}
	return &result
}

func summarizeGroup(values []Observation) Summary {
	completed := make([]Observation, 0, len(values))
	for _, observation := range values {
		if observation.Status == "ok" {
			completed = append(completed, observation)
		}
	}
	metrics := make(map[string]float64, len(summaryMetricNames))
	for _, name := range summaryMetricNames {
		for _, observation := range values {
			if observation.Status == "ok" {
				metrics[name] += observation.Metrics[name]
			}
		}
		if len(values) != 0 {
			metrics[name] /= float64(len(values))
		}
	}
	phases := []string{"search", "rerank", "answer", "judge", "total"}
	for _, observation := range completed {
		if _, ok := observation.LatencyMS["answer_fallback"]; ok {
			phases = append([]string{"answer_fallback"}, phases...)
			break
		}
	}
	p50 := make(map[string]*float64, len(phases))
	p95 := make(map[string]*float64, len(phases))
	for _, phase := range phases {
		latencies := make([]float64, 0)
		for _, observation := range completed {
			if latency, ok := observation.LatencyMS[phase]; ok {
				latencies = append(latencies, latency)
			}
		}
		p50[phase] = Percentile(latencies, .5)
		p95[phase] = Percentile(latencies, .95)
	}
	return Summary{
		QuestionCount: len(values), CompletedCount: len(completed), ErrorCount: len(values) - len(completed),
		Metrics: metrics, LatencyP50: p50, LatencyP95: p95,
	}
}

func conditionSummary(values []Observation) ConditionSummary {
	judgeScore := 0.0
	unknown := 0
	for _, observation := range values {
		judgeScore += observation.Metrics["llm_judge"]
		if NormalizeAnswer(observation.GeneratedAnswer) == "unknown" {
			unknown++
		}
	}
	return ConditionSummary{
		Count: len(values), LLMJudgeAccuracy: rate(judgeScore, len(values)),
		UnknownAnswerRate: rate(float64(unknown), len(values)),
	}
}

func rate(numerator float64, denominator int) float64 {
	if denominator == 0 {
		return 0
	}
	return numerator / float64(denominator)
}

func simpleTokens(value string) []string {
	replacer := strings.NewReplacer(".", " ", ",", " ", "!", " ", "?", " ", ":", " ", ";", " ")
	return strings.Fields(strings.ToLower(replacer.Replace(value)))
}

func wordTokens(value string) []string {
	result := make([]string, 0)
	var current strings.Builder
	flush := func() {
		if current.Len() != 0 {
			result = append(result, current.String())
			current.Reset()
		}
	}
	for _, character := range strings.ToLower(value) {
		if unicode.IsLetter(character) || unicode.IsNumber(character) || character == '_' {
			current.WriteRune(character)
		} else {
			flush()
		}
	}
	flush()
	return result
}

func multisetOverlap(left, right []string) int {
	counts := make(map[string]int, len(left))
	for _, value := range left {
		counts[value]++
	}
	overlap := 0
	for _, value := range right {
		if counts[value] > 0 {
			overlap++
			counts[value]--
		}
	}
	return overlap
}

func stringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func sourceSession(sourceID string) string {
	index := strings.LastIndexByte(sourceID, ':')
	candidate := sourceID
	if index >= 0 {
		candidate = sourceID[index+1:]
	}
	if len(candidate) < 2 || candidate[0] != 'D' {
		return ""
	}
	for _, value := range candidate[1:] {
		if value < '0' || value > '9' {
			return ""
		}
	}
	return candidate
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	position := len(digits)
	for value > 0 {
		position--
		digits[position] = byte(value%10) + '0'
		value /= 10
	}
	return string(digits[position:])
}
