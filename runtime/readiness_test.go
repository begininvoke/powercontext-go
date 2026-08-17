package runtime

import (
	"context"
	"errors"
	"maps"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/thunguo/powercontext-go/inference"
)

func TestReadinessChecksAggregateAndIsolate(t *testing.T) {
	t.Parallel()
	checks, err := NewReadinessChecks([]ProbeDefinition{
		{Name: "database", Blocking: true, Probe: fixedProbe(CheckReady)},
		{Name: "inference.embedding", Probe: fixedProbe(CheckMisconfigured)},
		{Name: "panics", Probe: func(context.Context) (CheckStatus, error) { panic("secret") }},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := checks.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status() != Degraded {
		t.Fatalf("status = %q, want degraded", result.Status())
	}
	want := map[string]CheckStatus{
		"database": CheckReady, "inference.embedding": CheckMisconfigured, "panics": CheckUnavailable,
	}
	if got := result.Checks(); !maps.Equal(got, want) {
		t.Fatalf("checks = %#v, want %#v", got, want)
	}

	blocking, err := NewReadinessChecks([]ProbeDefinition{{
		Name: "database", Blocking: true, Probe: fixedProbe(CheckUnavailable),
	}})
	if err != nil {
		t.Fatal(err)
	}
	result, err = blocking.Run(context.Background())
	if err != nil || result.Status() != NotReady {
		t.Fatalf("blocking result = %#v, %v", result, err)
	}
}

func TestReadinessChecksRunConcurrently(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	started := make(chan struct{}, 2)
	probe := func(ctx context.Context) (CheckStatus, error) {
		started <- struct{}{}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-release:
			return CheckReady, nil
		}
	}
	checks, err := NewReadinessChecks([]ProbeDefinition{{Name: "a", Probe: probe}, {Name: "b", Probe: probe}})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, runErr := checks.Run(context.Background())
		done <- runErr
	}()
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("probes did not start concurrently")
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestDependencyProbeClassification(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		err  error
		want CheckStatus
	}{
		{name: "ready", want: CheckReady},
		{name: "configuration", err: inference.NewConfigurationError("model", "secret"), want: CheckMisconfigured},
		{name: "timeout", err: inference.NewTimeoutError("embedding", time.Second), want: CheckTimeout},
		{name: "unavailable", err: errors.New("secret provider response"), want: CheckUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			probe := DependencyProbe(func(context.Context) error { return test.err }, time.Second)
			got, err := probe(context.Background())
			if err != nil || got != test.want {
				t.Fatalf("probe = %q, %v; want %q", got, err, test.want)
			}
		})
	}
}

func TestCachedProbeCollapsesRefreshAndUsesTransientTTL(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	var clockMu sync.Mutex
	clock := func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		return now
	}
	advance := func(duration time.Duration) {
		clockMu.Lock()
		now = now.Add(duration)
		clockMu.Unlock()
	}

	var calls atomic.Int64
	release := make(chan struct{})
	started := make(chan struct{})
	cached, err := NewCachedProbe(func(context.Context) (CheckStatus, error) {
		call := calls.Add(1)
		if call == 1 {
			close(started)
			<-release
			return CheckUnavailable, nil
		}
		return CheckReady, nil
	}, time.Hour, time.Minute, clock)
	if err != nil {
		t.Fatal(err)
	}

	const callers = 8
	results := make(chan CheckStatus, callers)
	for range callers {
		go func() {
			value, _ := cached.Probe(context.Background())
			results <- value
		}()
	}
	<-started
	close(release)
	for range callers {
		if got := <-results; got != CheckUnavailable {
			t.Fatalf("cached result = %q", got)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("refresh calls = %d, want 1", calls.Load())
	}

	advance(59 * time.Second)
	if got, _ := cached.Probe(context.Background()); got != CheckUnavailable || calls.Load() != 1 {
		t.Fatalf("fresh transient cache = %q, calls=%d", got, calls.Load())
	}
	advance(2 * time.Second)
	if got, _ := cached.Probe(context.Background()); got != CheckReady || calls.Load() != 2 {
		t.Fatalf("refreshed cache = %q, calls=%d", got, calls.Load())
	}
}

func TestReadinessCancellationPropagates(t *testing.T) {
	t.Parallel()
	checks, err := NewReadinessChecks([]ProbeDefinition{{
		Name: "blocked",
		Probe: func(ctx context.Context) (CheckStatus, error) {
			<-ctx.Done()
			return "", ctx.Err()
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := checks.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want cancellation", err)
	}
}

func fixedProbe(status CheckStatus) Probe {
	return func(context.Context) (CheckStatus, error) { return status, nil }
}
