package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/ob-labs/powercontext-go/inference"
	"github.com/ob-labs/powercontext-go/internal/benchmark/locomo"
	benchmarkprompts "github.com/ob-labs/powercontext-go/internal/benchmark/locomo/prompts"
)

func rejudgeCommand(ctx context.Context, arguments []string) error {
	flags := flag.NewFlagSet("locomo rejudge", flag.ContinueOnError)
	datasetPath := flags.String("dataset", defaultDataset, "LoCoMo dataset path")
	environmentPath := flags.String("env-file", ".env", "PowerContext environment file")
	sourceDirectory := flags.String("source-directory", "", "frozen answer result directory")
	outputDirectory := flags.String("output-directory", "", "independent judge result directory")
	runID := flags.String("run-id", "", "stable independent-judge identity")
	judgeModel := flags.String("judge-model", "", "explicit independent judge model")
	judgeProfile := flags.String("judge-profile", string(benchmarkprompts.TopicalJudge), "strict or topical")
	concurrency := flags.Int("concurrency", 8, "judge workers")
	operationRetries := flags.Int("operation-retries", 3, "transient inference attempts")
	keepErrors := flags.Bool("keep-errors", false, "do not retry checkpointed errors")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("rejudge accepts no positional arguments")
	}
	if *sourceDirectory == "" || *outputDirectory == "" || *runID == "" || *judgeModel == "" {
		return errors.New("source-directory, output-directory, run-id, and judge-model are required")
	}
	if *concurrency < 1 || *operationRetries < 1 {
		return errors.New("concurrency and operation-retries must be positive")
	}
	resolvedSource, err := filepath.Abs(*sourceDirectory)
	if err != nil {
		return err
	}
	resolvedOutput, err := filepath.Abs(*outputDirectory)
	if err != nil {
		return err
	}
	dataset, err := locomo.Load(*datasetPath)
	if err != nil {
		return err
	}
	config, err := loadConfiguration(*environmentPath)
	if err != nil {
		return err
	}
	profile := benchmarkprompts.JudgeProfile(*judgeProfile)
	manifest, err := locomo.PrepareRejudge(dataset, locomo.RejudgeOptions{
		SourceDirectory: resolvedSource, OutputDirectory: resolvedOutput, RunID: *runID,
		JudgeModel: *judgeModel, JudgeProfile: profile, OperationRetries: *operationRetries,
	})
	if err != nil {
		return err
	}
	if err := writeStdoutJSON(map[string]any{"run": manifest, "output_directory": resolvedOutput}); err != nil {
		return err
	}
	generator := &lazyJudgeGenerator{open: func() (inference.StructuredGenerator[locomo.JudgeInput, locomo.JudgeOutput], error) {
		model, err := textModel(*judgeModel, nil)
		if err != nil {
			return nil, err
		}
		limits, err := inference.NewLimits(config.Inference.GenerationTimeout, config.Inference.GenerationMaxRequests)
		if err != nil {
			return nil, err
		}
		return locomo.NewJudgeGenerator(model, &limits, profile)
	}}
	progress := func(value string) { fmt.Fprintln(os.Stderr, value) }
	report, err := locomo.Rejudge(ctx, dataset, locomo.RejudgeRunOptions{
		SourceDirectory: resolvedSource, OutputDirectory: resolvedOutput, RunID: *runID,
		JudgeModel: *judgeModel, JudgeProfile: profile, Concurrency: *concurrency,
		OperationRetries: *operationRetries, RetryErrors: !*keepErrors,
		JudgeGenerator: generator, Progress: progress,
	})
	if err != nil {
		return err
	}
	return writeStdoutJSON(map[string]any{"summary": report})
}

type lazyJudgeGenerator struct {
	once      sync.Once
	open      func() (inference.StructuredGenerator[locomo.JudgeInput, locomo.JudgeOutput], error)
	generator inference.StructuredGenerator[locomo.JudgeInput, locomo.JudgeOutput]
	err       error
}

func (g *lazyJudgeGenerator) Generate(
	ctx context.Context,
	input locomo.JudgeInput,
) (inference.GenerationResult[locomo.JudgeOutput], error) {
	g.once.Do(func() { g.generator, g.err = g.open() })
	if g.err != nil {
		return inference.GenerationResult[locomo.JudgeOutput]{}, g.err
	}
	return g.generator.Generate(ctx, input)
}
