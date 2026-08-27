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
	"fmt"
	"strings"
)

func RenderSummary(report EvaluationReport) string {
	overall := report.Metrics["overall"]
	lines := []string{
		"# PowerContext LoCoMo benchmark result",
		"",
		fmt.Sprintf("- Run: `%s`", report.RunID),
		fmt.Sprintf("- Questions: `%d` (errors: `%d`)", overall.QuestionCount, overall.ErrorCount),
		fmt.Sprintf("- LLM-judge accuracy: `%.4f`", overall.Metrics["llm_judge"]),
		fmt.Sprintf("- Reference-compatible set-token F1: `%.4f`", overall.Metrics["reference_set_f1"]),
		fmt.Sprintf("- Normalized token F1: `%.4f`", overall.Metrics["token_f1"]),
		fmt.Sprintf("- Exact match: `%.4f`", overall.Metrics["exact_match"]),
		fmt.Sprintf("- BLEU-1: `%.4f`", overall.Metrics["bleu1"]),
		fmt.Sprintf("- Coarse candidates / Answer context: `%d` / `%d`", report.CandidateK, report.AnswerK),
		fmt.Sprintf("- Rerank mode: `%s`", report.RerankMode),
		fmt.Sprintf("- Exact cited Source expansion: `%s`", pythonBool(report.AnswerSourceContent)),
		fmt.Sprintf("- Inference-aware answering: `%s`", pythonBool(report.AnswerInferenceAware)),
		fmt.Sprintf("- Unknown-fallback inference: `%s`", pythonBool(report.AnswerUnknownFallbackInference)),
		fmt.Sprintf("- Judge profile: `%s`", report.JudgeProfile),
		fmt.Sprintf("- Answer-context evidence Hit: `%.4f`", overall.Metrics["evidence_hit"]),
		fmt.Sprintf("- Answer-context evidence Recall: `%.4f`", overall.Metrics["evidence_recall"]),
		fmt.Sprintf("- Answer-context evidence MRR: `%.4f`", overall.Metrics["evidence_mrr"]),
		fmt.Sprintf("- Candidate evidence Hit@%d: `%.4f`", report.CandidateK, overall.Metrics["candidate_evidence_hit"]),
		"",
		"| Category | Questions | Judge accuracy | Set F1 | Evidence hit | Evidence recall |",
		"| --- | ---: | ---: | ---: | ---: | ---: |",
	}
	for _, name := range sortedMetricGroups(report.Metrics) {
		value := report.Metrics[name]
		lines = append(lines, fmt.Sprintf(
			"| %s | %d | %.4f | %.4f | %.4f | %.4f |",
			strings.TrimPrefix(name, "category_"), value.QuestionCount, value.Metrics["llm_judge"],
			value.Metrics["reference_set_f1"], value.Metrics["evidence_hit"], value.Metrics["evidence_recall"],
		))
	}
	lines = append(lines,
		"", "## Interpretation boundaries", "",
		"- "+report.MetricNotes["llm_judge"],
		"- "+report.MetricNotes["evidence"],
		"- "+report.MetricNotes["candidate_evidence"],
		"- "+report.MetricNotes["errors"],
		"- "+report.MetricNotes["category_5"], "",
	)
	return strings.Join(lines, "\n")
}

func pythonBool(value bool) string {
	if value {
		return "True"
	}
	return "False"
}
