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

package handoffreport

import (
	"context"
	"fmt"
	"slices"

	"github.com/ob-labs/powercontext-go/artifact"
	"github.com/ob-labs/powercontext-go/artifact/handoff"
	"github.com/ob-labs/powercontext-go/internal/work"
)

type HandoffReader interface {
	Latest(context.Context, string) (handoff.Handoff, bool, error)
	Get(context.Context, string, artifact.Ref) (handoff.Handoff, error)
	Revisions(context.Context, string) ([]handoff.Handoff, error)
	CheckEvidence(context.Context, string, artifact.Ref) ([]handoff.EvidenceCheck, error)
}

// WorkContinuityReader is the optional high-level Work projection consumed by
// reports. It deliberately belongs to the report package: Runtime can satisfy
// it directly without extending Work persistence contracts.
type WorkContinuityReader interface {
	Continuity(context.Context, string, *artifact.Ref) (work.Continuity, error)
}

func SelectOptimisticStable(
	ctx context.Context,
	reader HandoffReader,
	workstreams []WorkstreamDescriptor,
	attempts int,
) ([]SelectionEntry, error) {
	if reader == nil {
		return nil, fmt.Errorf("Handoff reader must not be nil")
	}
	if attempts < 1 || attempts > MaxSelectionAttempts {
		return nil, fieldError("attempts", fmt.Sprintf("must be between 1 and %d", MaxSelectionAttempts))
	}
	ordered := slices.Clone(workstreams)
	for _, descriptor := range ordered {
		if err := descriptor.Validate(); err != nil {
			return nil, err
		}
	}
	slices.SortFunc(ordered, func(left, right WorkstreamDescriptor) int {
		if left.ScopeID() < right.ScopeID() {
			return -1
		}
		if left.ScopeID() > right.ScopeID() {
			return 1
		}
		return 0
	})
	for index := 1; index < len(ordered); index++ {
		if ordered[index-1].ScopeID() == ordered[index].ScopeID() {
			return nil, fieldError("scope_id", "Workstream values must be unique")
		}
	}
	for range attempts {
		first, err := readHeadVector(ctx, reader, ordered)
		if err != nil {
			return nil, err
		}
		second, err := readHeadVector(ctx, reader, ordered)
		if err != nil {
			return nil, err
		}
		if equalHeadVectors(first, second) {
			result := make([]SelectionEntry, len(ordered))
			for index, descriptor := range ordered {
				status := SelectionNoHandoff
				if second[index] != nil {
					status = SelectionSelected
				}
				entry, err := NewSelectionEntry(descriptor.ScopeID(), descriptor.Version(), status, second[index])
				if err != nil {
					return nil, err
				}
				result[index] = entry
			}
			return result, nil
		}
	}
	return nil, &BusyError{Attempts: attempts}
}

func readHeadVector(ctx context.Context, reader HandoffReader, workstreams []WorkstreamDescriptor) ([]*artifact.Ref, error) {
	result := make([]*artifact.Ref, len(workstreams))
	for index, descriptor := range workstreams {
		value, found, err := reader.Latest(ctx, descriptor.ScopeID())
		if err != nil {
			return nil, err
		}
		if found {
			ref := value.Ref()
			result[index] = &ref
		}
	}
	return result, nil
}

func equalHeadVectors(left, right []*artifact.Ref) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if (left[index] == nil) != (right[index] == nil) {
			return false
		}
		if left[index] != nil && *left[index] != *right[index] {
			return false
		}
	}
	return true
}
