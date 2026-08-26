package sqlstore

import (
	"context"
	"errors"
	"testing"

	mysql "github.com/go-sql-driver/mysql"
	"github.com/mattn/go-sqlite3"
	"github.com/ob-labs/powercontext-go/artifact"
	"github.com/ob-labs/powercontext-go/artifact/experience"
)

func TestArtifactCreateIntegrityNormalizesOnlyCommittedLifecycle(t *testing.T) {
	ctx := context.Background()
	database, err := OpenSQLite(ctx, DefaultSQLiteConfig(":memory:"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(context.Background()); err != nil {
			t.Errorf("close database: %v", err)
		}
	})
	repository, err := NewArtifactRepository(SQLiteDialect, ExperienceArtifactCodec())
	if err != nil {
		t.Fatal(err)
	}
	content, err := experience.NewContent("s", "a", "o", "l")
	if err != nil {
		t.Fatal(err)
	}
	draft, err := experience.NewDraft(content, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var created artifact.Snapshot
	if err := database.Transaction(ctx, func(tx DBTX) error {
		var createErr error
		created, createErr = repository.Create(ctx, tx, "scope", "experience-1", draft)
		return createErr
	}); err != nil {
		t.Fatal(err)
	}
	constraint := sqlite3.Error{Code: sqlite3.ErrConstraint, ExtendedCode: sqlite3.ErrConstraintUnique}
	if err := database.Transaction(ctx, func(tx DBTX) error {
		normalized := repository.normalizeCreateIntegrity(ctx, tx, "scope", created.Ref(), constraint)
		var conflict *artifact.RevisionConflictError
		if !errors.As(normalized, &conflict) || conflict.Current != created.Ref() {
			t.Fatalf("normalized error = %#v", normalized)
		}
		missing, refErr := artifact.NewRef(experience.Family, "missing", 1)
		if refErr != nil {
			return refErr
		}
		if got := repository.normalizeCreateIntegrity(ctx, tx, "scope", missing, constraint); !errors.Is(got, constraint) {
			t.Fatalf("unrelated constraint = %#v", got)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestIntegrityConstraintClassification(t *testing.T) {
	for _, value := range []error{
		sqlite3.Error{Code: sqlite3.ErrConstraint, ExtendedCode: sqlite3.ErrConstraintPrimaryKey},
		&mysql.MySQLError{Number: 1062},
		&mysql.MySQLError{Number: 1452},
	} {
		if !isIntegrityConstraint(value) {
			t.Errorf("constraint %T(%v) was not classified", value, value)
		}
	}
	for _, value := range []error{errors.New("plain"), sqlite3.Error{Code: sqlite3.ErrBusy}, &mysql.MySQLError{Number: 2013}} {
		if isIntegrityConstraint(value) {
			t.Errorf("non-constraint %T(%v) was classified", value, value)
		}
	}
}
