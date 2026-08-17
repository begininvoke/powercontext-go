package runtime

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/thunguo/powercontext-go/artifact/experience"
	"github.com/thunguo/powercontext-go/inference"
	"github.com/thunguo/powercontext-go/source"
)

func TestExperienceIncubationUsesTwoStagesAndExactWindowEvidence(t *testing.T) {
	t.Parallel()

	ref, _ := source.NewRef(source.ContentType, "task-1")
	plan := incubationPlan(t, ref)
	backend := &fakeIncubationBackend{
		previous: source.NewCursor(2), next: source.NewCursor(3), high: 4,
		values: []source.Value{incubationSource(t, "task-1")}, available: []source.Ref{ref},
	}
	pipeline := incubationPipelineFunc(func(context.Context, []source.Value) ([]experience.CandidateInput, error) {
		if backend.applied {
			t.Fatal("pipeline ran after persistence")
		}
		return []experience.CandidateInput{plan}, nil
	})
	application, err := NewExperienceIncubationApplication(
		New(), func(string) (ExperienceIncubationBackend, error) { return backend, nil }, pipeline,
		func(string) (string, error) { return "cand-1", nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := application.Incubate(context.Background(), "scope", experience.IncubationWindowLimit)
	if err != nil {
		t.Fatal(err)
	}
	if !backend.applied || result.PreviousCursor != 2 || result.CurrentCursor != 3 ||
		result.HighWatermark != 4 || result.ProcessedSourceCount != 1 || result.CandidateCount != 1 {
		t.Fatalf("incubation result = %#v, applied=%v", result, backend.applied)
	}
	if len(backend.candidateIDs) != 1 || backend.candidateIDs[0] != "cand-1" ||
		len(backend.plans) != 1 || backend.binding != experience.IncubationCursorName {
		t.Fatalf("applied write = ids:%v plans:%d binding:%q", backend.candidateIDs, len(backend.plans), backend.binding)
	}
}

func TestExperienceIncubationRejectsPipelineEvidenceOutsideWindow(t *testing.T) {
	t.Parallel()

	available, _ := source.NewRef(source.ContentType, "task-1")
	foreign, _ := source.NewRef(source.ContentType, "task-2")
	backend := &fakeIncubationBackend{
		previous: source.NewCursor(0), next: source.NewCursor(1), high: 1,
		values: []source.Value{incubationSource(t, "task-1")}, available: []source.Ref{available},
	}
	application, err := NewExperienceIncubationApplication(
		New(), func(string) (ExperienceIncubationBackend, error) { return backend, nil },
		incubationPipelineFunc(func(context.Context, []source.Value) ([]experience.CandidateInput, error) {
			return []experience.CandidateInput{incubationPlan(t, foreign)}, nil
		}),
		func(string) (string, error) { return "cand-1", nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = application.Incubate(context.Background(), "scope", experience.IncubationWindowLimit)
	var invalid *inference.InvalidOutputError
	if !errors.As(err, &invalid) || backend.applied {
		t.Fatalf("Incubate() error = %T %v, applied=%v", err, err, backend.applied)
	}
}

func TestScheduledExperienceProcessorIsolatesScopeFailure(t *testing.T) {
	t.Parallel()

	runtime := New()
	ref, _ := source.NewRef(source.ContentType, "task")
	backends := map[string]*fakeIncubationBackend{}
	for _, scope := range []string{"scope-a", "scope-b"} {
		backends[scope] = &fakeIncubationBackend{
			previous: source.NewCursor(0), next: source.NewCursor(1), high: 1,
			values: []source.Value{incubationSource(t, "task")}, available: []source.Ref{ref},
		}
	}
	backends["scope-a"].applyErr = errors.New("isolated")
	application, err := NewExperienceIncubationApplication(
		runtime, func(scope string) (ExperienceIncubationBackend, error) { return backends[scope], nil },
		incubationPipelineFunc(func(context.Context, []source.Value) ([]experience.CandidateInput, error) {
			return []experience.CandidateInput{incubationPlan(t, ref)}, nil
		}),
		func(string) (string, error) { return "cand", nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	var observations []ScheduledObservation
	processor, err := NewScheduledProcessor(
		runtime, staticScopes{"scope-a", "scope-b"}, nil, application,
		func(_ context.Context, value ScheduledObservation) { observations = append(observations, value) }, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := processor.IncubateExperiences(context.Background()); err != nil {
		t.Fatalf("scope failure escaped processor: %v", err)
	}
	if !backends["scope-a"].applied || !backends["scope-b"].applied || len(observations) != 2 ||
		observations[0].Outcome != ScheduledProcessingFailure || observations[1].Outcome != ScheduledProcessingSuccess {
		t.Fatalf("observations = %#v", observations)
	}
}

type incubationPipelineFunc func(context.Context, []source.Value) ([]experience.CandidateInput, error)

func (f incubationPipelineFunc) Incubate(ctx context.Context, values []source.Value) ([]experience.CandidateInput, error) {
	return f(ctx, values)
}

type fakeIncubationBackend struct {
	previous, next source.Cursor
	high           int64
	values         []source.Value
	available      []source.Ref
	applied        bool
	binding        string
	candidateIDs   []string
	plans          []experience.CandidateInput
	applyErr       error
}

func (b *fakeIncubationBackend) ObserveWindow(
	context.Context, string, int64,
) (source.Cursor, source.Cursor, *int64, int64, []source.Value, []source.Ref, error) {
	return b.previous, b.next, nil, b.high, b.values, b.available, nil
}

func (b *fakeIncubationBackend) ApplyWindow(
	_ context.Context,
	binding string,
	candidateIDs []string,
	plans []experience.CandidateInput,
	_ source.Cursor,
	_ *int64,
) error {
	b.applied = true
	b.binding = binding
	b.candidateIDs = append([]string(nil), candidateIDs...)
	b.plans = append([]experience.CandidateInput(nil), plans...)
	return b.applyErr
}

type staticScopes []string

func (s staticScopes) ScopeIDs(context.Context) ([]string, error) {
	return append([]string(nil), s...), nil
}

func incubationPlan(t *testing.T, ref source.Ref) experience.CandidateInput {
	t.Helper()
	content, err := experience.NewContent("situation", "action", "outcome", "lesson")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := experience.NewCandidateInput(content, []source.Ref{ref})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func incubationSource(t *testing.T, id string) source.ContentSource {
	t.Helper()
	value, err := source.RestoreContentSource(
		id, source.Captured, nil, fmt.Sprintf("outcome %s", id), map[string]any{"kind": experience.TaskOutcomeSourceKind},
	)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
