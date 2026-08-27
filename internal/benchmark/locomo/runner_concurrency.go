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
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ob-labs/powercontext-go/inference"
)

func retryTransient[T any](ctx context.Context, attempts int, operation func(context.Context) (T, error)) (T, int, error) {
	var zero T
	for attempt := 1; attempt <= attempts; attempt++ {
		value, err := operation(ctx)
		if err == nil {
			return value, attempt - 1, nil
		}
		if !isTransientInference(err) || attempt == attempts {
			return zero, attempt - 1, err
		}
		delay := time.Second << (attempt - 1)
		if delay > 8*time.Second {
			delay = 8 * time.Second
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return zero, attempt - 1, ctx.Err()
		case <-timer.C:
		}
	}
	return zero, attempts, errors.New("unreachable benchmark retry state")
}

func isTransientInference(err error) bool {
	var unavailable *inference.UnavailableError
	var timeout *inference.TimeoutError
	return errors.As(err, &unavailable) || errors.As(err, &timeout)
}

func parallelConversations(
	ctx context.Context,
	values []Conversation,
	concurrency int,
	operation func(context.Context, Conversation) (ConversationIngestion, error),
) ([]ConversationIngestion, error) {
	type job struct {
		index int
		value Conversation
	}
	jobs := make(chan job)
	result := make([]ConversationIngestion, len(values))
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	var workers sync.WaitGroup
	for range min(concurrency, max(len(values), 1)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			defer func() {
				if recovered := recover(); recovered != nil {
					cancel(fmt.Errorf("LoCoMo ingestion worker panicked: %v", recovered))
				}
			}()
			for item := range jobs {
				value, err := operation(ctx, item.value)
				if err != nil {
					cancel(err)
					return
				}
				result[item.index] = value
			}
		}()
	}
sendConversations:
	for index, value := range values {
		select {
		case jobs <- job{index: index, value: value}:
		case <-ctx.Done():
			break sendConversations
		}
	}
	close(jobs)
	workers.Wait()
	if err := context.Cause(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

func parallelQuestions(
	ctx context.Context,
	values []Question,
	concurrency int,
	operation func(context.Context, Question) ObservationRecord,
) ([]ObservationRecord, error) {
	type job struct {
		index int
		value Question
	}
	jobs := make(chan job)
	result := make([]ObservationRecord, len(values))
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	var workers sync.WaitGroup
	for range min(concurrency, max(len(values), 1)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			defer func() {
				if recovered := recover(); recovered != nil {
					cancel(fmt.Errorf("LoCoMo evaluation worker panicked: %v", recovered))
				}
			}()
			for item := range jobs {
				result[item.index] = operation(ctx, item.value)
			}
		}()
	}
sendQuestions:
	for index, value := range values {
		select {
		case jobs <- job{index: index, value: value}:
		case <-ctx.Done():
			break sendQuestions
		}
	}
	close(jobs)
	workers.Wait()
	if err := context.Cause(ctx); err != nil {
		return nil, err
	}
	return result, nil
}
