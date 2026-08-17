package sqlstore

import (
	"context"
	"errors"

	"github.com/thunguo/powercontext-go/artifact/experience"
	"github.com/thunguo/powercontext-go/review"
	"github.com/thunguo/powercontext-go/source"
	"github.com/thunguo/powercontext-go/trigger"
)

// ExperienceIncubationStore keeps the authoritative window snapshot separate
// from model inference, then commits all pending Candidates and the cursor CAS
// in one transaction.
type ExperienceIncubationStore struct {
	database   *Database
	scopeID    string
	sources    *SourceRepository
	candidates *CandidateRepository
	cursors    SourceCursorRepository
	policy     trigger.SourceWindowPolicy
}

func NewExperienceIncubationStore(
	database *Database,
	scopeID string,
	sources *SourceRepository,
	candidates *CandidateRepository,
) (*ExperienceIncubationStore, error) {
	if database == nil || sources == nil || candidates == nil {
		return nil, errors.New("sqlstore: Experience incubation dependencies must not be nil")
	}
	if err := requireScope(scopeID); err != nil {
		return nil, err
	}
	return &ExperienceIncubationStore{
		database: database, scopeID: scopeID, sources: sources, candidates: candidates,
	}, nil
}

func (s *ExperienceIncubationStore) ObserveWindow(
	ctx context.Context,
	bindingName string,
	limit int64,
) (
	previous source.Cursor,
	next source.Cursor,
	generation *int64,
	highWatermark int64,
	values []source.Value,
	available []source.Ref,
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
			available = []source.Ref{}
			return nil
		}
		windowLimit := int(actions[0].Through() - actions[0].After())
		rows, listErr := s.sources.List(ctx, tx, s.scopeID, actions[0].After(), &windowLimit)
		if listErr != nil {
			return listErr
		}
		values = make([]source.Value, len(rows))
		available = make([]source.Ref, len(rows))
		for index, row := range rows {
			values[index] = row.Value
			available[index] = row.Ref
		}
		return nil
	})
	return previous, next, generation, highWatermark, values, available, err
}

func (s *ExperienceIncubationStore) ApplyWindow(
	ctx context.Context,
	bindingName string,
	candidateIDs []string,
	plans []experience.CandidateInput,
	next source.Cursor,
	expectedGeneration *int64,
) error {
	if len(candidateIDs) != len(plans) {
		return &InvalidRepositoryArgumentError{
			Field: "candidate_ids", Detail: "must correspond exactly to Experience plans",
		}
	}
	return s.database.Transaction(ctx, func(tx DBTX) error {
		for index, plan := range plans {
			refs := plan.Sources()
			for _, ref := range refs {
				if _, err := s.sources.Get(ctx, tx, s.scopeID, ref); err != nil {
					return &review.InvalidCandidateError{
						Field: "evidence", Detail: "reference is not available in this scope",
					}
				}
			}
			reason := plan.Reason()
			if _, err := s.candidates.Create(
				ctx, tx, s.scopeID, candidateIDs[index], experience.Family,
				plan.Proposal(), refs, nil, nil, &reason,
			); err != nil {
				return err
			}
		}
		_, err := s.cursors.Save(ctx, tx, s.scopeID, bindingName, next, expectedGeneration)
		return err
	})
}
