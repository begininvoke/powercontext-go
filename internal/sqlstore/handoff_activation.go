package sqlstore

import (
	"context"
	"errors"

	"github.com/thunguo/powercontext-go/artifact/handoff"
	"github.com/thunguo/powercontext-go/source"
)

// HandoffActivationStore owns the two short transactional stages surrounding
// Handoff generation: observing a durable Source boundary and CAS-advancing
// the stable trigger cursor after generation succeeds.
type HandoffActivationStore struct {
	database *Database
	sources  *SourceRepository
	cursors  SourceCursorRepository
}

func NewHandoffActivationStore(
	database *Database,
	sources *SourceRepository,
) (*HandoffActivationStore, error) {
	if database == nil || sources == nil {
		return nil, errors.New("sqlstore: Handoff activation dependencies must not be nil")
	}
	return &HandoffActivationStore{database: database, sources: sources}, nil
}

func (s *HandoffActivationStore) LoadBoundary(
	ctx context.Context,
	scopeID string,
	boundary source.Ref,
	bindingName string,
) (position int64, cursor source.Cursor, generation *int64, err error) {
	cursor = source.NewCursor(0)
	err = s.database.Transaction(ctx, func(tx DBTX) error {
		stored, getErr := s.sources.Get(ctx, tx, scopeID, boundary)
		if getErr != nil {
			var missing *RepositoryNotFoundError
			if errors.As(getErr, &missing) {
				citation, citationErr := handoff.NewSourceCitation(boundary)
				if citationErr != nil {
					return citationErr
				}
				return &handoff.EvidenceUnavailableError{Citation: citation}
			}
			return getErr
		}
		position = stored.JournalPosition
		state, found, loadErr := s.cursors.Load(ctx, tx, scopeID, bindingName)
		if loadErr != nil {
			return loadErr
		}
		if found {
			cursor = state.Cursor
			value := state.Generation
			generation = &value
		}
		return nil
	})
	return position, cursor, generation, err
}

func (s *HandoffActivationStore) SaveBoundary(
	ctx context.Context,
	scopeID, bindingName string,
	cursor source.Cursor,
	expectedGeneration *int64,
) error {
	return s.database.Transaction(ctx, func(tx DBTX) error {
		_, err := s.cursors.Save(ctx, tx, scopeID, bindingName, cursor, expectedGeneration)
		return err
	})
}
