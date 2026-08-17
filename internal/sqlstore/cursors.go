package sqlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/thunguo/powercontext-go/source"
)

type StoredSourceCursor struct {
	ScopeID     string
	BindingName string
	Cursor      source.Cursor
	Generation  int64
}

type SourceCursorRepository struct{}

func (SourceCursorRepository) Load(
	ctx context.Context,
	db DBTX,
	scopeID, bindingName string,
) (StoredSourceCursor, bool, error) {
	if err := requireIdentifier("scope_id", scopeID, 256); err != nil {
		return StoredSourceCursor{}, false, err
	}
	if err := requireIdentifier("binding_name", bindingName, source.MaxBindingNameLength); err != nil {
		return StoredSourceCursor{}, false, err
	}
	var rowScope, rowBinding string
	var payload, generation any
	err := db.QueryRowContext(ctx, `SELECT scope_id, binding_name, cursor, generation
        FROM pc_source_cursors WHERE scope_id = ? AND binding_name = ?`, scopeID, bindingName).Scan(
		&rowScope, &rowBinding, &payload, &generation,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return StoredSourceCursor{}, false, nil
	}
	if err != nil {
		return StoredSourceCursor{}, false, err
	}
	bytes, err := storedBytes(payload, "cursor")
	if err != nil {
		return StoredSourceCursor{}, false, err
	}
	cursor, err := decodeSourceCursor(bytes)
	if err != nil {
		return StoredSourceCursor{}, false, &InvalidStoredPayloadError{
			Kind: "source-cursor", Name: rowBinding, Issue: "payload does not match the model",
		}
	}
	decodedGeneration, ok := integer(generation)
	if !ok {
		return StoredSourceCursor{}, false, &InvalidStoredColumnError{Column: "generation", Expected: "an integer"}
	}
	return StoredSourceCursor{
		ScopeID: rowScope, BindingName: rowBinding, Cursor: cursor, Generation: decodedGeneration,
	}, true, nil
}

func (repository SourceCursorRepository) Save(
	ctx context.Context,
	db DBTX,
	scopeID, bindingName string,
	cursor source.Cursor,
	expectedGeneration *int64,
) (StoredSourceCursor, error) {
	if err := requireIdentifier("scope_id", scopeID, 256); err != nil {
		return StoredSourceCursor{}, err
	}
	if err := requireIdentifier("binding_name", bindingName, source.MaxBindingNameLength); err != nil {
		return StoredSourceCursor{}, err
	}
	if expectedGeneration != nil && *expectedGeneration < 1 {
		return StoredSourceCursor{}, &InvalidRepositoryArgumentError{
			Field: "expected_generation", Detail: "must be positive",
		}
	}
	payload, err := marshalJSON(struct {
		Sequence int64 `json:"sequence"`
	}{Sequence: cursor.Sequence()})
	if err != nil {
		return StoredSourceCursor{}, err
	}
	var generation int64
	if expectedGeneration == nil {
		existing, found, err := repository.Load(ctx, db, scopeID, bindingName)
		if err != nil {
			return StoredSourceCursor{}, err
		}
		if found {
			actual := existing.Generation
			return StoredSourceCursor{}, &GenerationConflictError{
				BindingName: bindingName, Actual: &actual,
			}
		}
		generation = 1
		if _, err := db.ExecContext(ctx, `INSERT INTO pc_source_cursors
            (scope_id, binding_name, cursor, generation) VALUES (?, ?, ?, ?)`,
			scopeID, bindingName, payload, generation); err != nil {
			existing, found, readErr := repository.Load(ctx, db, scopeID, bindingName)
			if readErr != nil || !found {
				return StoredSourceCursor{}, errors.Join(err, readErr)
			}
			actual := existing.Generation
			return StoredSourceCursor{}, &GenerationConflictError{BindingName: bindingName, Actual: &actual}
		}
	} else {
		generation = *expectedGeneration + 1
		result, err := db.ExecContext(ctx, `UPDATE pc_source_cursors SET cursor = ?, generation = ?
            WHERE scope_id = ? AND binding_name = ? AND generation = ?`,
			payload, generation, scopeID, bindingName, *expectedGeneration)
		if err != nil {
			return StoredSourceCursor{}, err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return StoredSourceCursor{}, err
		}
		if affected != 1 {
			existing, found, readErr := repository.Load(ctx, db, scopeID, bindingName)
			if readErr != nil {
				return StoredSourceCursor{}, readErr
			}
			var actual *int64
			if found {
				value := existing.Generation
				actual = &value
			}
			expected := *expectedGeneration
			return StoredSourceCursor{}, &GenerationConflictError{
				BindingName: bindingName, Expected: &expected, Actual: actual,
			}
		}
	}
	return StoredSourceCursor{
		ScopeID: scopeID, BindingName: bindingName, Cursor: cursor, Generation: generation,
	}, nil
}

func decodeSourceCursor(payload []byte) (source.Cursor, error) {
	var fields map[string]json.RawMessage
	if err := unmarshalJSON(payload, &fields); err != nil {
		return source.Cursor{}, err
	}
	sequence := int64(0)
	if raw, ok := fields["sequence"]; ok {
		if string(raw) == "null" {
			return source.Cursor{}, fmt.Errorf("cursor sequence cannot be null")
		}
		if err := unmarshalJSON(raw, &sequence); err != nil {
			return source.Cursor{}, err
		}
	}
	return source.NewCursor(sequence), nil
}

func requireIdentifier(field, value string, maximum int) error {
	if !utf8.ValidString(value) || strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
		return &InvalidRepositoryArgumentError{Field: field, Detail: "must be a non-empty trimmed string"}
	}
	if utf8.RuneCountInString(value) > maximum {
		return &InvalidRepositoryArgumentError{
			Field: field, Detail: fmt.Sprintf("must not exceed %d characters", maximum),
		}
	}
	return nil
}
