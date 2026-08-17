package runtime

import (
	"context"
	"errors"

	"github.com/thunguo/powercontext-go/artifact"
	"github.com/thunguo/powercontext-go/artifact/memory"
	"github.com/thunguo/powercontext-go/source"
	"github.com/thunguo/powercontext-go/stats"
	"github.com/thunguo/powercontext-go/trigger"
)

const (
	DefaultMemoryArtifactID  = "memory"
	DefaultSourceWindowLimit = int64(100)
)

type MemoryServiceFactory func(string) (*memory.Service, error)

// MemoryFlushBackend is the use-case-shaped persistence port for the two
// transaction boundaries surrounding extraction. ObserveWindow returns one
// relational snapshot; ApplyWindow must atomically apply the Memory plan and
// cursor CAS.
type MemoryFlushBackend interface {
	ObserveWindow(context.Context, string, int64) (
		previous source.Cursor,
		next source.Cursor,
		generation *int64,
		highWatermark int64,
		values []source.Value,
		err error,
	)
	ApplyWindow(context.Context, string, memory.WritePlan, source.Cursor, *int64) (*memory.Memory, error)
}

type MemoryFlushBackendFactory func(string) (MemoryFlushBackend, error)

type MemoryApplication struct {
	runtime           *Runtime
	services          MemoryServiceFactory
	flushes           MemoryFlushBackendFactory
	memoryArtifactID  string
	sourceWindowLimit int64
}

func NewMemoryApplication(runtime *Runtime, services MemoryServiceFactory, memoryArtifactID string) (*MemoryApplication, error) {
	if runtime == nil || services == nil {
		return nil, errors.New("runtime: Memory application dependencies must not be nil")
	}
	if memoryArtifactID == "" {
		memoryArtifactID = DefaultMemoryArtifactID
	}
	if _, err := artifact.NewRef(memory.Family, memoryArtifactID, 1); err != nil {
		return nil, err
	}
	return &MemoryApplication{
		runtime: runtime, services: services, memoryArtifactID: memoryArtifactID,
		sourceWindowLimit: DefaultSourceWindowLimit,
	}, nil
}

// NewMemoryApplicationWithFlush constructs the complete Memory application.
// The simpler constructor remains useful for deployments that intentionally
// expose only explicit Memory operations.
func NewMemoryApplicationWithFlush(
	runtime *Runtime,
	services MemoryServiceFactory,
	flushes MemoryFlushBackendFactory,
	memoryArtifactID string,
	sourceWindowLimit int64,
) (*MemoryApplication, error) {
	if flushes == nil {
		return nil, errors.New("runtime: Memory Flush backend factory must not be nil")
	}
	if sourceWindowLimit < 1 {
		return nil, errors.New("runtime: source_window_limit must be positive")
	}
	application, err := NewMemoryApplication(runtime, services, memoryArtifactID)
	if err != nil {
		return nil, err
	}
	application.flushes = flushes
	application.sourceWindowLimit = sourceWindowLimit
	return application, nil
}

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

// Flush advances the stable Memory Source window. Planning may invoke models
// and is intentionally performed after ObserveWindow has committed and before
// ApplyWindow opens the final transaction.
func (a *MemoryApplication) Flush(ctx context.Context, scopeID string) (MemoryFlushResult, error) {
	var result MemoryFlushResult
	err := a.runtime.ScopedWrite(ctx, scopeID, func(ctx context.Context, scope string) error {
		var operationErr error
		result, operationErr = a.flush(ctx, scope)
		return operationErr
	})
	return cloneFlushResult(result), err
}

// flush performs an already-admitted, already-serialized Source window. It is
// shared with the scheduled processor so shutdown never requires recursive
// lifecycle admission.
func (a *MemoryApplication) flush(ctx context.Context, scope string) (MemoryFlushResult, error) {
	ctx = a.runtime.withModelUsage(ctx, scope, stats.MemoryExtraction, stats.MemoryIndexing)
	if a.flushes == nil {
		return MemoryFlushResult{}, &StateError{Code: "memory-flush"}
	}
	backend, err := a.flushes(scope)
	if err != nil {
		return MemoryFlushResult{}, err
	}
	if backend == nil {
		return MemoryFlushResult{}, &StateError{Code: "memory-flush"}
	}
	previous, next, generation, highWatermark, sources, err := backend.ObserveWindow(
		ctx, trigger.SourceWindowName, a.sourceWindowLimit,
	)
	if err != nil {
		return MemoryFlushResult{}, err
	}
	result := MemoryFlushResult{
		PreviousCursor: previous.Sequence(), CurrentCursor: next.Sequence(),
		HighWatermark: highWatermark, ProcessedSourceCount: len(sources),
	}
	if next.Sequence() == previous.Sequence() {
		return result, nil
	}

	service, err := a.service(scope)
	if err != nil {
		return result, err
	}
	current, err := a.headOrNone(ctx, service)
	if err != nil {
		return result, err
	}
	plan, err := service.PlanRemember(ctx, current, sources, nil, nil, memory.RememberExtract)
	if err != nil {
		return result, err
	}
	updated, err := backend.ApplyWindow(ctx, trigger.SourceWindowName, plan, next, generation)
	if err != nil {
		return result, err
	}
	if updated != nil {
		ref := updated.Ref()
		result.MemoryRef = &ref
	}
	return result, nil
}

func (a *MemoryApplication) Remember(
	ctx context.Context,
	scopeID string,
	request RememberMemoryRequest,
) (MemoryMutationResult, error) {
	var result MemoryMutationResult
	err := a.runtime.ScopedWrite(ctx, scopeID, func(ctx context.Context, scope string) error {
		ctx = a.runtime.withModelUsage(ctx, scope, "", stats.MemoryIndexing)
		service, err := a.service(scope)
		if err != nil {
			return err
		}
		current, err := a.headOrNone(ctx, service)
		if err != nil {
			return err
		}
		if err := validateExpectedRevision(a.memoryArtifactID, current, request.ExpectedRevision); err != nil {
			return err
		}
		input := memory.NewEntryInput(nil, request.Kind, request.Text, nil, nil, request.Reason)
		updated, err := service.Remember(ctx, current, nil, nil, []memory.EntryInput{input}, memory.RememberAppend)
		if err != nil {
			return err
		}
		if updated == nil {
			return &StateError{Code: "empty-write"}
		}
		result.MemoryRef = updated.Ref()
		if current != nil {
			previous := current.Revision()
			result.PreviousRevision = &previous
		}
		if current == nil || current.Ref() != updated.Ref() {
			result.Entry, err = lastChangedEntry(ctx, service, *updated)
		}
		return err
	})
	return cloneMutation(result), err
}

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
	current, err := a.headOrNone(ctx, service)
	if err != nil || current == nil {
		return MemorySearchPage{}, err
	}
	search, err := service.Search(ctx, query, []memory.Memory{*current}, limit, mode)
	if err != nil {
		return MemorySearchPage{}, err
	}
	ref := current.Ref()
	usedMode := search.Mode
	return MemorySearchPage{MemoryRef: &ref, Mode: &usedMode, Hits: cloneHits(search.Hits)}, nil
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

func (a *MemoryApplication) Revise(
	ctx context.Context,
	scopeID string,
	citation memory.Citation,
	kind, text string,
	reason *string,
) (MemoryMutationResult, error) {
	var result MemoryMutationResult
	err := a.runtime.ScopedWrite(ctx, scopeID, func(ctx context.Context, scope string) error {
		ctx = a.runtime.withModelUsage(ctx, scope, "", stats.MemoryIndexing)
		service, err := a.service(scope)
		if err != nil {
			return err
		}
		current, entry, err := a.currentCitation(ctx, service, citation)
		if err != nil {
			return err
		}
		input := memory.NewEntryInput(&entry, kind, text, nil, nil, reason)
		updated, err := service.Remember(ctx, &current, nil, nil, []memory.EntryInput{input}, memory.RememberAppend)
		if err != nil {
			return err
		}
		if updated == nil {
			return &StateError{Code: "empty-write"}
		}
		revised, err := logicalEntry(ctx, service, *updated, entry.EntryID)
		if err != nil {
			return err
		}
		record, err := entryRecord(*updated, revised)
		if err != nil {
			return err
		}
		previous := current.Revision()
		result = MemoryMutationResult{PreviousRevision: &previous, MemoryRef: updated.Ref(), Entry: &record}
		return nil
	})
	return cloneMutation(result), err
}

func (a *MemoryApplication) Retire(
	ctx context.Context,
	scopeID string,
	citation memory.Citation,
	reason *string,
) (MemoryMutationResult, error) {
	var result MemoryMutationResult
	err := a.runtime.ScopedWrite(ctx, scopeID, func(ctx context.Context, scope string) error {
		ctx = a.runtime.withModelUsage(ctx, scope, "", stats.MemoryIndexing)
		service, err := a.service(scope)
		if err != nil {
			return err
		}
		current, entry, err := a.currentCitation(ctx, service, citation)
		if err != nil {
			return err
		}
		updated, err := service.Forget(ctx, current, []memory.EntryVersion{entry}, reason)
		if err != nil {
			return err
		}
		retired, err := logicalEntry(ctx, service, updated, entry.EntryID)
		if err != nil {
			return err
		}
		record, err := entryRecord(updated, retired)
		if err != nil {
			return err
		}
		previous := current.Revision()
		result = MemoryMutationResult{PreviousRevision: &previous, MemoryRef: updated.Ref(), Entry: &record}
		return nil
	})
	return cloneMutation(result), err
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

func (a *MemoryApplication) service(scope string) (*memory.Service, error) {
	service, err := a.services(scope)
	if err != nil {
		return nil, err
	}
	if service == nil {
		return nil, &StateError{Code: "memory"}
	}
	return service, nil
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

func (a *MemoryApplication) currentCitation(
	ctx context.Context,
	service *memory.Service,
	citation memory.Citation,
) (memory.Memory, memory.EntryVersion, error) {
	current, err := service.Head(ctx, a.memoryArtifactID)
	if err != nil {
		return memory.Memory{}, memory.EntryVersion{}, err
	}
	if citation.MemoryRef.ID() != current.ID() {
		return memory.Memory{}, memory.EntryVersion{}, &artifact.NotFoundError{Ref: citation.MemoryRef}
	}
	if citation.MemoryRef != current.Ref() {
		return memory.Memory{}, memory.EntryVersion{}, &artifact.RevisionConflictError{
			Requested: citation.MemoryRef, Current: current.Ref(),
		}
	}
	entry, err := citedEntry(ctx, service, current, citation)
	return current, entry, err
}

func validateExpectedRevision(memoryArtifactID string, current *memory.Memory, expected *int64) error {
	if expected == nil {
		return nil
	}
	requested, err := artifact.NewRef(memory.Family, memoryArtifactID, *expected)
	if err != nil {
		return err
	}
	if current == nil {
		return &artifact.NotFoundError{Ref: requested}
	}
	if current.Revision() != *expected {
		return &artifact.RevisionConflictError{Requested: requested, Current: current.Ref()}
	}
	return nil
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
	return value
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
