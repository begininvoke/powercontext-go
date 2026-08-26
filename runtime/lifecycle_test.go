package runtime

import (
	"context"
	"errors"
	goruntime "runtime"
	"sync"
	"sync/atomic"
	"testing"
)

func TestScopedWriteSerializesExactScopeAndReclaimsGate(t *testing.T) {
	t.Parallel()
	runtime := New()
	entered := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- runtime.ScopedWrite(context.Background(), "scope-a", func(context.Context, string) error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered

	var secondEntered atomic.Bool
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- runtime.ScopedWrite(context.Background(), "scope-a", func(context.Context, string) error {
			secondEntered.Store(true)
			return nil
		})
	}()
	for index := 0; index < 1000; index++ {
		goruntime.Gosched()
	}
	if secondEntered.Load() {
		t.Fatal("same-Scope writer overlapped")
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}

	runtime.scopes.mu.Lock()
	remaining := len(runtime.scopes.entries)
	runtime.scopes.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("idle keyed gates retained: %d", remaining)
	}
}

func TestScopedWritesForDifferentScopesCanOverlap(t *testing.T) {
	t.Parallel()
	runtime := New()
	entered := make(chan string, 2)
	release := make(chan struct{})
	var group sync.WaitGroup
	for _, scope := range []string{"scope-a", "scope-b"} {
		scope := scope
		group.Add(1)
		go func() {
			defer group.Done()
			if err := runtime.ScopedWrite(context.Background(), scope, func(context.Context, string) error {
				entered <- scope
				<-release
				return nil
			}); err != nil {
				t.Error(err)
			}
		}()
	}
	<-entered
	<-entered
	close(release)
	group.Wait()
}

func TestScopedReadsForSameScopeCanOverlap(t *testing.T) {
	t.Parallel()
	runtime := New()
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	errors := make(chan error, 2)
	for range 2 {
		go func() {
			errors <- runtime.ScopedRead(context.Background(), "scope-a", func(context.Context, string) error {
				entered <- struct{}{}
				<-release
				return nil
			})
		}()
	}

	// Both callbacks must enter before either is released. A keyed read lock or
	// accidental reuse of the write gate would deadlock this assertion.
	<-entered
	<-entered
	close(release)
	for range 2 {
		if err := <-errors; err != nil {
			t.Fatal(err)
		}
	}
}

func TestCloseRejectsNewWorkAndWaitsForAdmittedOperation(t *testing.T) {
	t.Parallel()
	runtime := New()
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- runtime.Operation(context.Background(), func(context.Context) error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered
	closed := make(chan error, 1)
	go func() { closed <- runtime.Close(context.Background()) }()
	waitForClosing(t, runtime)
	err := runtime.Operation(context.Background(), func(context.Context) error { return nil })
	var state *StateError
	if !errors.As(err, &state) {
		t.Fatalf("expected closed StateError, got %v", err)
	}
	select {
	case err := <-closed:
		t.Fatalf("Close returned before drain: %v", err)
	default:
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
}

func TestCanceledCloseRestoresAdmissionAndCanceledWaiterIsReclaimed(t *testing.T) {
	t.Parallel()
	runtime := New()
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- runtime.ScopedWrite(context.Background(), "scope-a", func(context.Context, string) error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered
	waiterContext, cancelWaiter := context.WithCancel(context.Background())
	waiterDone := make(chan error, 1)
	var canceledWaiterEntered atomic.Bool
	go func() {
		waiterDone <- runtime.ScopedWrite(waiterContext, "scope-a", func(context.Context, string) error {
			canceledWaiterEntered.Store(true)
			return nil
		})
	}()
	cancelWaiter()
	if err := <-waiterDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("waiter error = %v", err)
	}
	if canceledWaiterEntered.Load() {
		t.Fatal("canceled waiter entered")
	}

	closeContext, cancelClose := context.WithCancel(context.Background())
	cancelClose()
	if err := runtime.Close(closeContext); !errors.Is(err, context.Canceled) {
		t.Fatalf("close error = %v", err)
	}
	if err := runtime.ScopedRead(context.Background(), "scope-b", func(context.Context, string) error { return nil }); err != nil {
		t.Fatalf("admission was not restored: %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	runtime.scopes.mu.Lock()
	remaining := len(runtime.scopes.entries)
	runtime.scopes.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("canceled waiter retained keyed gate: %d", remaining)
	}
}

func TestValidateScopeMatchesFrozenRuntimeBoundary(t *testing.T) {
	t.Parallel()
	if got, err := ValidateScopeID(" scope "); err != nil || got != " scope " {
		t.Fatalf("opaque untrimmed Scope changed: %q %v", got, err)
	}
	if _, err := ValidateScopeID(" \t "); err == nil {
		t.Fatal("blank Scope accepted")
	}
}

func TestCloseOrdersSchedulerBeforeOwnedResourcesInReverseOrder(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var events []string
	record := func(value string) { mu.Lock(); events = append(events, value); mu.Unlock() }
	runtime := New(
		closeRecorder{close: func(context.Context) error { record("resource-1"); return nil }},
		closeRecorder{close: func(context.Context) error { record("resource-2"); return nil }},
	)
	scheduler := &schedulerRecorder{
		pause: func() { record("scheduler-pause") },
		close: func(context.Context) error { record("scheduler-close"); return nil },
	}
	if err := runtime.AttachScheduler(scheduler); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []string{"scheduler-pause", "scheduler-close", "resource-2", "resource-1"}
	if len(events) != len(want) {
		t.Fatalf("close events = %v", events)
	}
	for index := range want {
		if events[index] != want[index] {
			t.Fatalf("close events = %v", events)
		}
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(events) != len(want) {
		t.Fatalf("idempotent Close repeated work: %v", events)
	}
}

type closeRecorder struct{ close func(context.Context) error }

func (r closeRecorder) Close(ctx context.Context) error { return r.close(ctx) }

type schedulerRecorder struct {
	pause func()
	close func(context.Context) error
}

func (r *schedulerRecorder) Pause()                          { r.pause() }
func (r *schedulerRecorder) Close(ctx context.Context) error { return r.close(ctx) }

func waitForClosing(t *testing.T, runtime *Runtime) {
	t.Helper()
	for index := 0; index < 100_000; index++ {
		runtime.stateMu.Lock()
		closing := runtime.closing
		runtime.stateMu.Unlock()
		if closing {
			return
		}
		goruntime.Gosched()
	}
	t.Fatal("Runtime did not enter closing state")
}
