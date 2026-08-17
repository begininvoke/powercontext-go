package sqlstore

import (
	"context"
	"errors"
	"reflect"

	"github.com/thunguo/powercontext-go/source"
)

// MemorySourceResolver exposes the exact Source catalog semantics required by
// memory.Service for one Scope. Each canonicalization is a short read
// transaction; callers therefore never retain a SQL transaction across model
// inference.
type MemorySourceResolver struct {
	database   *Database
	scopeID    string
	repository *SourceRepository
}

func NewMemorySourceResolver(
	database *Database,
	scopeID string,
	repository *SourceRepository,
) (*MemorySourceResolver, error) {
	if database == nil || repository == nil {
		return nil, errors.New("sqlstore: Memory Source resolver dependencies must not be nil")
	}
	if err := requireScope(scopeID); err != nil {
		return nil, err
	}
	return &MemorySourceResolver{database: database, scopeID: scopeID, repository: repository}, nil
}

func (r *MemorySourceResolver) Ref(value source.Value) (source.Ref, error) {
	return r.repository.Ref(value)
}

func (r *MemorySourceResolver) Get(ctx context.Context, value source.Value) (source.Value, error) {
	ref, err := r.Ref(value)
	if err != nil {
		return nil, err
	}
	var stored source.Value
	err = r.database.Transaction(ctx, func(tx DBTX) error {
		row, getErr := r.repository.Get(ctx, tx, r.scopeID, ref)
		if getErr != nil {
			var missing *RepositoryNotFoundError
			if errors.As(getErr, &missing) {
				return &source.NotFoundError{Source: value}
			}
			return getErr
		}
		stored = row.Value
		return nil
	})
	if err != nil {
		return nil, err
	}
	if reflect.TypeOf(stored) != reflect.TypeOf(value) || !reflect.DeepEqual(stored, value) {
		return nil, &source.NotFoundError{Source: value}
	}
	return stored, nil
}
