// Package prompts owns the frozen LoCoMo benchmark instructions.
package prompts

import (
	_ "embed"
	"fmt"
	"strings"
)

const (
	AnswerVersion                      = "powercontext.benchmark.locomo.answer.v1"
	AnswerSourceVersion                = "powercontext.benchmark.locomo.answer.source.v1"
	AnswerSourceInferenceVersion       = "powercontext.benchmark.locomo.answer.source.inference.v1"
	AnswerSourceUnknownFallbackVersion = "powercontext.benchmark.locomo.answer.source.unknown_fallback.v1"
	JudgeVersion                       = "powercontext.benchmark.locomo.judge.v1"
	TopicalJudgeVersion                = "powercontext.benchmark.locomo.judge.topical.v1"
)

//go:embed answer.txt
var answer string

//go:embed answer_source.txt
var answerSource string

//go:embed answer_source_inference.txt
var answerSourceInference string

//go:embed judge.txt
var judge string

//go:embed judge_topical.txt
var topicalJudge string

type JudgeProfile string

const (
	StrictJudge  JudgeProfile = "strict"
	TopicalJudge JudgeProfile = "topical"
)

func JudgeInstructions(profile JudgeProfile) (string, string, error) {
	switch profile {
	case StrictJudge:
		return trim(judge), JudgeVersion, nil
	case TopicalJudge:
		return trim(topicalJudge), TopicalJudgeVersion, nil
	default:
		return "", "", fmt.Errorf("unsupported LoCoMo judge profile %q", profile)
	}
}

func AnswerInstructions(sourceContent, inferenceAware bool) (string, string, error) {
	if inferenceAware {
		if !sourceContent {
			return "", "", fmt.Errorf("inference-aware answering requires Source expansion")
		}
		return trim(answerSourceInference), AnswerSourceInferenceVersion, nil
	}
	if sourceContent {
		return trim(answerSource), AnswerSourceVersion, nil
	}
	return trim(answer), AnswerVersion, nil
}

func AnswerPolicyVersion(sourceContent, inferenceAware, unknownFallbackInference bool) (string, error) {
	if inferenceAware && unknownFallbackInference {
		return "", fmt.Errorf("Answer treatment modes are mutually exclusive")
	}
	if unknownFallbackInference {
		if !sourceContent {
			return "", fmt.Errorf("Unknown-fallback inference answering requires Source expansion")
		}
		return AnswerSourceUnknownFallbackVersion, nil
	}
	_, version, err := AnswerInstructions(sourceContent, inferenceAware)
	return version, err
}

func Answer() string                { return trim(answer) }
func AnswerSource() string          { return trim(answerSource) }
func AnswerSourceInference() string { return trim(answerSourceInference) }
func Judge() string                 { return trim(judge) }
func JudgeTopical() string          { return trim(topicalJudge) }

func trim(value string) string { return strings.TrimSuffix(value, "\n") }
