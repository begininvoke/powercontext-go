package sqlstore

import (
	"context"
	"errors"

	"github.com/ob-labs/powercontext-go/artifact/memory"
	"github.com/ob-labs/powercontext-go/source"
	"github.com/ob-labs/powercontext-go/trigger"
)

// MemoryFlushStore implements the two short relational stages around Memory
// extraction for one Scope. ObserveWindow owns one consistent read snapshot;
// ApplyWindow atomically commits authoritative Memory state and cursor CAS.
type MemoryFlushStore struct {
	database *Database
	scopeID  string
	sources  *SourceRepository
	memory   *MemoryRepository
	cursors  SourceCursorRepository
	policy   trigger.SourceWindowPolicy
}

func NewMemoryFlushStore(
	database *Database,
	scopeID string,
	sources *SourceRepository,
	memoryRepository *MemoryRepository,
) (*MemoryFlushStore, error) {
	if database == nil || sources == nil || memoryRepository == nil {
		return nil, errors.New("sqlstore: Memory Flush dependencies must not be nil")
	}
	if err := requireScope(scopeID); err != nil {
		return nil, err
	}
	if memoryRepository.database != database || memoryRepository.scopeID != scopeID {
		return nil, errors.New("sqlstore: Memory Flush repository must use the same database and Scope")
	}
	return &MemoryFlushStore{
		database: database, scopeID: scopeID, sources: sources, memory: memoryRepository,
	}, nil
}

func (s *MemoryFlushStore) ObserveWindow(
	ctx context.Context,
	bindingName string,
	limit int64,
) (
	previous source.Cursor,
	next source.Cursor,
	generation *int64,
	highWatermark int64,
	values []source.Value,
	err error,
) {
	previous = s.policy.InitialState()
	next = previous
	err = s.database.Transaction(ctx, func(tx DBTX) error {
		state, found, loadErr := s.cursors.Load(ctx, tx, s.scopeID, bindingName)
		if loadErr != nil {
			return loadErr
		}
		if found {
			previous = state.Cursor
			next = previous
			value := state.Generation
			generation = &value
		}
		highWatermark, loadErr = s.sources.JournalPosition(ctx, tx, s.scopeID)
		if loadErr != nil {
			return loadErr
		}
		signal, signalErr := trigger.NewSourceHighWatermark(highWatermark, limit)
		if signalErr != nil {
			return signalErr
		}
		transition := s.policy.Activate(signal, previous)
		next = transition.State()
		actions := transition.Actions()
		if len(actions) == 0 {
			values = []source.Value{}
			return nil
		}
		rows, listErr := s.sources.List(ctx, tx, s.scopeID, actions[0].After(), nil)
		if listErr != nil {
			return listErr
		}
		values = make([]source.Value, 0, len(rows))
		for _, row := range rows {
			if row.JournalPosition > actions[0].Through() {
				break
			}
			values = append(values, row.Value)
		}
		return nil
	})
	return previous, next, generation, highWatermark, values, err
}

func (s *MemoryFlushStore) ApplyWindow(
	ctx context.Context,
	bindingName string,
	plan memory.WritePlan,
	next source.Cursor,
	expectedGeneration *int64,
) (*memory.Memory, error) {
	var result *memory.Memory
	err := s.database.Transaction(ctx, func(tx DBTX) error {
		var applyErr error
		result, applyErr = s.memory.applyPlan(ctx, tx, plan)
		if applyErr != nil {
			return applyErr
		}
		_, applyErr = s.cursors.Save(ctx, tx, s.scopeID, bindingName, next, expectedGeneration)
		return applyErr
	})
	return result, err
}
