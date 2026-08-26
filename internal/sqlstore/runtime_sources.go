package sqlstore

import (
	"context"
	"errors"

	"github.com/ob-labs/powercontext-go/source"
)

// RuntimeSourceBackend is the use-case-shaped adapter consumed by
// runtime.SourceApplication. Source resolution remains outside the SQL
// transaction; journal allocation and idempotent insertion are atomic.
type RuntimeSourceBackend struct {
	database   *Database
	repository *SourceRepository
	adapter    source.ContentAdapter
}

func NewRuntimeSourceBackend(database *Database, repository *SourceRepository) (*RuntimeSourceBackend, error) {
	if database == nil || repository == nil {
		return nil, errors.New("sqlstore: Runtime Source dependencies must not be nil")
	}
	return &RuntimeSourceBackend{database: database, repository: repository}, nil
}

func (b *RuntimeSourceBackend) Capture(
	ctx context.Context,
	scopeID string,
	capture source.ContentCapture,
) (source.Ref, int64, error) {
	value, err := b.adapter.Resolve(ctx, capture)
	if err != nil {
		return source.Ref{}, 0, err
	}
	var stored StoredSource
	err = b.database.Transaction(ctx, func(tx DBTX) error {
		var addErr error
		stored, addErr = b.repository.Add(ctx, tx, scopeID, value)
		return addErr
	})
	if err != nil {
		var conflict *StoredPayloadConflictError
		if errors.As(err, &conflict) {
			return source.Ref{}, 0, &source.ConflictError{Field: "source_id", Value: capture.ID()}
		}
		return source.Ref{}, 0, err
	}
	return stored.Ref, stored.JournalPosition, nil
}

// ScopeIDs returns only partitions that own a Source journal, in deterministic
// database byte order. It intentionally does not infer Scopes from Artifacts
// or configuration.
func (b *RuntimeSourceBackend) ScopeIDs(ctx context.Context) ([]string, error) {
	var result []string
	err := b.database.Transaction(ctx, func(tx DBTX) error {
		rows, err := tx.QueryContext(ctx,
			"SELECT scope_id FROM pc_source_journal_heads ORDER BY scope_id",
		)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var scopeID string
			if err := rows.Scan(&scopeID); err != nil {
				return err
			}
			result = append(result, scopeID)
		}
		return rows.Err()
	})
	return result, err
}
