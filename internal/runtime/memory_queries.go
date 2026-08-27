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
	"context"
	"errors"

	"github.com/ob-labs/powercontext-go/artifact"
	"github.com/ob-labs/powercontext-go/artifact/memory"
	"github.com/ob-labs/powercontext-go/internal/stats"
)

func (a *MemoryApplication) Search(
	ctx context.Context,
	scopeID, query string,
	limit int,
	mode memory.SearchMode,
) (MemorySearchPage, error) {
	var result MemorySearchPage
	err := a.runtime.ScopedRead(ctx, scopeID, func(ctx context.Context, scope string) error {
		ctx = a.runtime.withModelUsage(ctx, scope, stats.MemoryRecall, stats.MemoryRecall)
		var err error
		result, err = a.search(ctx, scope, query, limit, mode)
		return err
	})
	return cloneSearchPage(result), err
}

// search performs an already-admitted scoped read. It is intentionally kept
// inside the runtime package so composite operations can share one lifecycle
// admission and one per-scope gate without recursively admitting work during
// shutdown.
func (a *MemoryApplication) search(
	ctx context.Context,
	scope, query string,
	limit int,
	mode memory.SearchMode,
) (MemorySearchPage, error) {
	service, err := a.service(scope)
	if err != nil {
		return MemorySearchPage{}, err
	}
	var page MemorySearchPage
	err = a.runtime.runStage(ctx, "memory.search", map[string]TraceAttribute{
		"powercontext.memory.search.requested_mode": string(mode),
		"powercontext.memory.search.limit":          limit,
	}, func(stageContext context.Context, span StageSpan) error {
		var searchErr error
		page, searchErr = a.searchWithService(stageContext, service, query, limit, mode)
		if searchErr == nil {
			attributes := map[string]TraceAttribute{
				"powercontext.memory.search.memory_present": page.MemoryRef != nil,
				"powercontext.memory.search.result_count":   len(page.Hits),
			}
			if page.Mode != nil {
				attributes["powercontext.memory.search.mode"] = string(*page.Mode)
			}
			setStageAttributes(span, attributes)
		}
		return searchErr
	})
	return page, err
}

func (a *MemoryApplication) searchWithService(
	ctx context.Context,
	service *memory.Service,
	query string,
	limit int,
	mode memory.SearchMode,
) (MemorySearchPage, error) {
	for attempt := 1; ; attempt++ {
		current, err := a.headOrNone(ctx, service)
		if err != nil || current == nil {
			return MemorySearchPage{}, err
		}
		search, err := service.Search(ctx, query, []memory.Memory{*current}, limit, mode)
		if err == nil {
			ref := current.Ref()
			usedMode := search.Mode
			return MemorySearchPage{
				MemoryRef: &ref,
				Mode:      &usedMode,
				Hits:      cloneHits(search.Hits),
				Rerank:    cloneRerankTrace(search.Rerank),
			}, nil
		}
		latest, latestErr := a.headOrNone(ctx, service)
		if latestErr != nil {
			return MemorySearchPage{}, latestErr
		}
		if !isStaleMemorySearch(err) || latest == nil || latest.Ref() == current.Ref() {
			return MemorySearchPage{}, err
		}
		if attempt == memorySearchAttempts {
			return MemorySearchPage{}, &artifact.RevisionConflictError{
				Requested: current.Ref(), Current: latest.Ref(),
			}
		}
	}
}

func isStaleMemorySearch(err error) bool {
	var capability *memory.CapabilityNotSupportedError
	if errors.As(err, &capability) && capability.Capability == "head" {
		return true
	}
	var citation *memory.InvalidCitationError
	return errors.As(err, &citation) && citation.Code == "memory-mismatch"
}

func (a *MemoryApplication) List(
	ctx context.Context,
	scopeID string,
	includeInactive bool,
) (MemoryEntriesPage, error) {
	var result MemoryEntriesPage
	err := a.runtime.ScopedRead(ctx, scopeID, func(ctx context.Context, scope string) error {
		service, err := a.service(scope)
		if err != nil {
			return err
		}
		current, err := a.headOrNone(ctx, service)
		if err != nil || current == nil {
			return err
		}
		entries, err := service.Entries(ctx, *current)
		if err != nil {
			return err
		}
		ref := current.Ref()
		result.MemoryRef = &ref
		for _, entry := range entries {
			record, err := entryRecord(*current, entry)
			if err != nil {
				return err
			}
			if includeInactive || record.State == memory.Active {
				result.Entries = append(result.Entries, record)
			}
		}
		return nil
	})
	return cloneEntriesPage(result), err
}

func (a *MemoryApplication) Get(
	ctx context.Context,
	scopeID string,
	citation memory.Citation,
) (MemoryEntryRecord, error) {
	var result MemoryEntryRecord
	err := a.runtime.ScopedRead(ctx, scopeID, func(ctx context.Context, scope string) error {
		service, err := a.service(scope)
		if err != nil {
			return err
		}
		value, err := service.Revision(ctx, citation.MemoryRef)
		if err != nil {
			return err
		}
		if value.ID() != a.memoryArtifactID {
			return &artifact.NotFoundError{Ref: citation.MemoryRef}
		}
		entry, err := citedEntry(ctx, service, value, citation)
		if err != nil {
			return err
		}
		result, err = entryRecord(value, entry)
		return err
	})
	return result.Clone(), err
}

func (a *MemoryApplication) Changes(
	ctx context.Context,
	scopeID string,
	sinceRevision *int64,
) (MemoryChangesPage, error) {
	var result MemoryChangesPage
	err := a.runtime.ScopedRead(ctx, scopeID, func(ctx context.Context, scope string) error {
		service, err := a.service(scope)
		if err != nil {
			return err
		}
		current, err := a.headOrNone(ctx, service)
		if err != nil || current == nil {
			return err
		}
		revisions, err := service.Changes(ctx, *current, sinceRevision)
		if err != nil {
			return err
		}
		ref := current.Ref()
		result = MemoryChangesPage{MemoryRef: &ref, Revisions: cloneRevisionChanges(revisions)}
		return nil
	})
	return cloneChangesPage(result), err
}

func (a *MemoryApplication) headOrNone(ctx context.Context, service *memory.Service) (*memory.Memory, error) {
	value, err := service.Head(ctx, a.memoryArtifactID)
	if err != nil {
		var missing *artifact.NotFoundError
		if errors.As(err, &missing) {
			return nil, nil
		}
		return nil, err
	}
	return &value, nil
}

func citedEntry(ctx context.Context, service *memory.Service, value memory.Memory, citation memory.Citation) (memory.EntryVersion, error) {
	found := false
	for _, manifest := range value.Content().Manifest().Entries() {
		if manifest.EntryID() == citation.EntryID && manifest.EntryVersionID() == citation.EntryVersionID {
			found = true
			break
		}
	}
	if !found {
		return memory.EntryVersion{}, &memory.EntryNotFoundError{EntryID: citation.EntryID}
	}
	return service.ValidateCitation(ctx, citation)
}

func logicalEntry(ctx context.Context, service *memory.Service, value memory.Memory, entryID string) (memory.EntryVersion, error) {
	entries, err := service.Entries(ctx, value)
	if err != nil {
		return memory.EntryVersion{}, err
	}
	for _, entry := range entries {
		if entry.EntryID == entryID {
			return entry, nil
		}
	}
	return memory.EntryVersion{}, &memory.EntryNotFoundError{EntryID: entryID}
}

func lastChangedEntry(ctx context.Context, service *memory.Service, value memory.Memory) (*MemoryEntryRecord, error) {
	changes := value.Content().Changes()
	if len(changes) == 0 {
		return nil, nil
	}
	entry, err := logicalEntry(ctx, service, value, changes[len(changes)-1].EntryID())
	if err != nil {
		return nil, err
	}
	record, err := entryRecord(value, entry)
	return &record, err
}

func entryRecord(value memory.Memory, entry memory.EntryVersion) (MemoryEntryRecord, error) {
	for _, manifest := range value.Content().Manifest().Entries() {
		if manifest.EntryID() == entry.EntryID && manifest.EntryVersionID() == entry.EntryVersionID {
			return MemoryEntryRecord{MemoryRef: value.Ref(), State: manifest.State(), Entry: entry.Clone()}, nil
		}
	}
	return MemoryEntryRecord{}, &memory.EntryNotFoundError{EntryID: entry.EntryID}
}
