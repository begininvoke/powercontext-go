package runtime

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
)

type recordedRuntimeStage struct {
	owner      *recordingStageTracing
	name       string
	attributes map[string]TraceAttribute
	outcome    string
	err        error
}

type recordingStageTracing struct {
	mu     sync.Mutex
	stages []*recordedRuntimeStage
	panic  bool
}

func (t *recordingStageTracing) StartStage(
	ctx context.Context,
	name string,
	attributes map[string]TraceAttribute,
) (context.Context, StageSpan) {
	if t.panic {
		panic("tracer unavailable")
	}
	stage := &recordedRuntimeStage{owner: t, name: name, attributes: cloneTraceAttributes(attributes)}
	t.mu.Lock()
	t.stages = append(t.stages, stage)
	t.mu.Unlock()
	return ctx, stage
}

func (s *recordedRuntimeStage) SetAttributes(attributes map[string]TraceAttribute) {
	s.owner.mu.Lock()
	defer s.owner.mu.Unlock()
	if s.attributes == nil {
		s.attributes = make(map[string]TraceAttribute)
	}
	for key, value := range attributes {
		s.attributes[key] = value
	}
}

func (s *recordedRuntimeStage) Finish(outcome string, err error) {
	s.owner.mu.Lock()
	defer s.owner.mu.Unlock()
	s.outcome, s.err = outcome, err
}

func TestScopedStagesAreBoundedAndLockStageEndsBeforeCriticalSection(t *testing.T) {
	t.Parallel()
	tracing := &recordingStageTracing{}
	runtime, err := NewConfigured(RuntimeOptions{Tracing: tracing}, nil)
	if err != nil {
		t.Fatal(err)
	}
	operationError := errors.New("critical section failed")
	err = runtime.ScopedWrite(t.Context(), "private-scope", func(context.Context, string) error {
		return operationError
	})
	if !errors.Is(err, operationError) {
		t.Fatalf("ScopedWrite error = %v", err)
	}
	tracing.mu.Lock()
	defer tracing.mu.Unlock()
	if len(tracing.stages) != 2 {
		t.Fatalf("stages = %v", tracing.stages)
	}
	if tracing.stages[0].name != "scope.context" || tracing.stages[0].outcome != "success" {
		t.Fatalf("context stage = %#v", tracing.stages[0])
	}
	lock := tracing.stages[1]
	if lock.name != "scope.lock" || lock.outcome != "success" || lock.err != nil ||
		!reflect.DeepEqual(lock.attributes, map[string]TraceAttribute{"powercontext.scope.lock.contended": false}) {
		t.Fatalf("lock stage = %#v", lock)
	}
	for _, stage := range tracing.stages {
		for _, value := range stage.attributes {
			if value == "private-scope" {
				t.Fatal("stage leaked raw Scope")
			}
		}
	}
}

func TestRuntimeStageClassifiesFailureAndCancellation(t *testing.T) {
	t.Parallel()
	tracing := &recordingStageTracing{}
	runtime, err := NewConfigured(RuntimeOptions{Tracing: tracing}, nil)
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("sensitive failure")
	if err := runtime.runStage(t.Context(), "memory.search", nil, func(context.Context, StageSpan) error {
		return failure
	}); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := runtime.runStage(canceled, "memory.search", nil, func(context.Context, StageSpan) error {
		return context.Canceled
	}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	tracing.mu.Lock()
	defer tracing.mu.Unlock()
	if tracing.stages[0].outcome != "failure" || tracing.stages[1].outcome != "cancelled" {
		t.Fatalf("stage outcomes = %q, %q", tracing.stages[0].outcome, tracing.stages[1].outcome)
	}
}

func TestRuntimeStageTracerFailureDoesNotChangeOperation(t *testing.T) {
	t.Parallel()
	runtime, err := NewConfigured(RuntimeOptions{Tracing: &recordingStageTracing{panic: true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	if err := runtime.ScopedRead(t.Context(), "scope", func(context.Context, string) error {
		called = true
		return nil
	}); err != nil || !called {
		t.Fatalf("operation changed by tracer failure: called=%t err=%v", called, err)
	}
}
