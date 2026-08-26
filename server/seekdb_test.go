package server

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type trackedSeekDBResource struct {
	name   string
	events *[]string
	err    error
}

func (r *trackedSeekDBResource) Close(ctx context.Context) error {
	state := "live"
	if context.Cause(ctx) != nil {
		state = "canceled"
	}
	*r.events = append(*r.events, r.name+":"+state)
	return r.err
}

func TestSeekDBInstanceClosesPoolBeforeNativeRuntime(t *testing.T) {
	t.Parallel()
	var events []string
	resource := &seekDBInstance{
		database: &trackedSeekDBResource{name: "database", events: &events},
		value:    &trackedSeekDBResource{name: "instance", events: &events},
	}
	if err := resource.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if want := []string{"database:live", "instance:live"}; !reflect.DeepEqual(events, want) {
		t.Fatalf("close events = %v, want %v", events, want)
	}
}

func TestSeekDBInstanceFinishesCleanupBeforePropagatingCancellation(t *testing.T) {
	t.Parallel()
	var events []string
	ctx, cancel := context.WithCancelCause(context.Background())
	cause := errors.New("shutdown canceled")
	cancel(cause)
	resource := &seekDBInstance{
		database: &trackedSeekDBResource{name: "database", events: &events},
		value:    &trackedSeekDBResource{name: "instance", events: &events},
	}
	err := resource.Close(ctx)
	if !errors.Is(err, cause) {
		t.Fatalf("close error = %v, want cancellation", err)
	}
	if want := []string{"database:live", "instance:live"}; !reflect.DeepEqual(events, want) {
		t.Fatalf("close events = %v, want %v", events, want)
	}
}

func TestSeekDBInstanceStillClosesNativeRuntimeAfterPoolError(t *testing.T) {
	t.Parallel()
	var events []string
	poolError := errors.New("pool close failed")
	resource := &seekDBInstance{
		database: &trackedSeekDBResource{name: "database", events: &events, err: poolError},
		value:    &trackedSeekDBResource{name: "instance", events: &events},
	}
	if err := resource.Close(context.Background()); !errors.Is(err, poolError) {
		t.Fatalf("close error = %v, want pool error", err)
	}
	if want := []string{"database:live", "instance:live"}; !reflect.DeepEqual(events, want) {
		t.Fatalf("close events = %v, want %v", events, want)
	}
}
