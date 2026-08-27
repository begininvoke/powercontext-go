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
