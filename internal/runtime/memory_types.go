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

package runtime

import (
	"slices"

	"github.com/ob-labs/powercontext-go/artifact"
	"github.com/ob-labs/powercontext-go/artifact/memory"
)

type RememberMemoryRequest struct {
	Kind             string
	Text             string
	Reason           *string
	ExpectedRevision *int64
}

type MemoryMutationResult struct {
	PreviousRevision *int64
	MemoryRef        artifact.Ref
	Entry            *MemoryEntryRecord
}

type MemoryEntryRecord struct {
	MemoryRef artifact.Ref
	State     memory.EntryState
	Entry     memory.EntryVersion
}

func (r MemoryEntryRecord) Clone() MemoryEntryRecord {
	r.Entry = r.Entry.Clone()
	return r
}

type MemoryEntriesPage struct {
	MemoryRef *artifact.Ref
	Entries   []MemoryEntryRecord
}

type MemorySearchPage struct {
	MemoryRef *artifact.Ref
	Mode      *memory.SearchMode
	Hits      []memory.Hit
	Rerank    *memory.RerankTrace
}

type MemoryChangesPage struct {
	MemoryRef *artifact.Ref
	Revisions []memory.RevisionChanges
}

type MemoryFlushResult struct {
	PreviousCursor       int64
	CurrentCursor        int64
	HighWatermark        int64
	ProcessedSourceCount int
	MemoryRef            *artifact.Ref
}

func (r MemoryFlushResult) Processed() bool { return r.CurrentCursor > r.PreviousCursor }

func cloneMutation(value MemoryMutationResult) MemoryMutationResult {
	if value.PreviousRevision != nil {
		copy := *value.PreviousRevision
		value.PreviousRevision = &copy
	}
	if value.Entry != nil {
		copy := value.Entry.Clone()
		value.Entry = &copy
	}
	return value
}

func cloneEntriesPage(value MemoryEntriesPage) MemoryEntriesPage {
	if value.MemoryRef != nil {
		copy := *value.MemoryRef
		value.MemoryRef = &copy
	}
	entries := make([]MemoryEntryRecord, len(value.Entries))
	for index, entry := range value.Entries {
		entries[index] = entry.Clone()
	}
	value.Entries = entries
	return value
}

func cloneSearchPage(value MemorySearchPage) MemorySearchPage {
	if value.MemoryRef != nil {
		copy := *value.MemoryRef
		value.MemoryRef = &copy
	}
	if value.Mode != nil {
		copy := *value.Mode
		value.Mode = &copy
	}
	value.Hits = cloneHits(value.Hits)
	value.Rerank = cloneRerankTrace(value.Rerank)
	return value
}

func cloneRerankTrace(value *memory.RerankTrace) *memory.RerankTrace {
	if value == nil {
		return nil
	}
	copy := *value
	copy.CandidateHits = cloneHits(value.CandidateHits)
	copy.SelectedRanks = slices.Clone(value.SelectedRanks)
	if value.Usage.InputTokens != nil {
		inputTokens := *value.Usage.InputTokens
		copy.Usage.InputTokens = &inputTokens
	}
	if value.Usage.OutputTokens != nil {
		outputTokens := *value.Usage.OutputTokens
		copy.Usage.OutputTokens = &outputTokens
	}
	return &copy
}

func cloneChangesPage(value MemoryChangesPage) MemoryChangesPage {
	if value.MemoryRef != nil {
		copy := *value.MemoryRef
		value.MemoryRef = &copy
	}
	value.Revisions = cloneRevisionChanges(value.Revisions)
	return value
}

func cloneFlushResult(value MemoryFlushResult) MemoryFlushResult {
	if value.MemoryRef != nil {
		copy := *value.MemoryRef
		value.MemoryRef = &copy
	}
	return value
}

func cloneHits(values []memory.Hit) []memory.Hit {
	result := make([]memory.Hit, len(values))
	for index, value := range values {
		result[index] = value.Clone()
	}
	return result
}

func cloneRevisionChanges(values []memory.RevisionChanges) []memory.RevisionChanges {
	result := make([]memory.RevisionChanges, len(values))
	for index, value := range values {
		result[index] = value.Clone()
	}
	return result
}
