package sqlstore_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"testing"

	"github.com/thunguo/powercontext-go/internal/sqlstore"
	"github.com/thunguo/powercontext-go/source"
)

func TestSourceRepositoryPythonPayloadAndIdempotence(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database := openTestDatabase(t)
	repository, err := sqlstore.NewSourceRepository(sqlstore.SQLiteDialect, sqlstore.ContentSourceCodec())
	if err != nil {
		t.Fatal(err)
	}
	value := contentSource(t, "capture-1", "hello <world>", map[string]any{
		"nested": map[string]any{"x": "y"},
		"a":      1,
	})

	var first sqlstore.StoredSource
	if err := database.Transaction(ctx, func(tx sqlstore.DBTX) error {
		var addErr error
		first, addErr = repository.Add(ctx, tx, "scope-a", value)
		return addErr
	}); err != nil {
		t.Fatal(err)
	}
	if first.JournalPosition != 1 {
		t.Fatalf("journal position = %d", first.JournalPosition)
	}
	var payload []byte
	if err := database.SQLDB().QueryRowContext(ctx,
		"SELECT payload FROM pc_sources WHERE scope_id = ? AND source_type = ? AND source_id = ?",
		"scope-a", "content", "capture-1",
	).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	want := `{"name":"capture-1","materialization":"captured","description":null,"content":"hello <world>","metadata":{"a":1,"nested":{"x":"y"}}}`
	if string(payload) != want {
		t.Fatalf("payload mismatch\n got: %s\nwant: %s", payload, want)
	}

	var second sqlstore.StoredSource
	if err := database.Transaction(ctx, func(tx sqlstore.DBTX) error {
		var addErr error
		second, addErr = repository.Add(ctx, tx, "scope-a", value)
		return addErr
	}); err != nil {
		t.Fatal(err)
	}
	if second.JournalPosition != 1 {
		t.Fatalf("idempotent position = %d", second.JournalPosition)
	}

	conflicting := contentSource(t, "capture-1", "different", nil)
	err = database.Transaction(ctx, func(tx sqlstore.DBTX) error {
		_, addErr := repository.Add(ctx, tx, "scope-a", conflicting)
		return addErr
	})
	var conflict *sqlstore.StoredPayloadConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func TestSourceRepositorySerializesConcurrentJournalPositions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database := openTestDatabase(t)
	repository, err := sqlstore.NewSourceRepository(sqlstore.SQLiteDialect, sqlstore.ContentSourceCodec())
	if err != nil {
		t.Fatal(err)
	}

	const count = 16
	positions := make([]int, 0, count)
	var positionsMu sync.Mutex
	var group sync.WaitGroup
	errorsFound := make(chan error, count)
	for index := range count {
		group.Go(func() {
			value := contentSource(t, fmt.Sprintf("capture-%02d", index), "content", nil)
			var stored sqlstore.StoredSource
			err := database.Transaction(ctx, func(tx sqlstore.DBTX) error {
				var addErr error
				stored, addErr = repository.Add(ctx, tx, "scope-concurrent", value)
				return addErr
			})
			if err != nil {
				errorsFound <- err
				return
			}
			positionsMu.Lock()
			positions = append(positions, int(stored.JournalPosition))
			positionsMu.Unlock()
		})
	}
	group.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Error(err)
	}
	if t.Failed() {
		return
	}
	sort.Ints(positions)
	for index, position := range positions {
		if position != index+1 {
			t.Fatalf("positions = %v", positions)
		}
	}
	var highWatermark int64
	if err := database.Transaction(ctx, func(tx sqlstore.DBTX) error {
		var positionErr error
		highWatermark, positionErr = repository.JournalPosition(ctx, tx, "scope-concurrent")
		return positionErr
	}); err != nil {
		t.Fatal(err)
	}
	if highWatermark != count {
		t.Fatalf("high watermark = %d", highWatermark)
	}
}

func TestSourceRepositoryRejectsIndexedIdentityMismatch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database := openTestDatabase(t)
	repository, err := sqlstore.NewSourceRepository(sqlstore.SQLiteDialect, sqlstore.ContentSourceCodec())
	if err != nil {
		t.Fatal(err)
	}
	value := contentSource(t, "indexed", "content", nil)
	if err := database.Transaction(ctx, func(tx sqlstore.DBTX) error {
		_, addErr := repository.Add(ctx, tx, "scope-a", value)
		return addErr
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQLDB().ExecContext(ctx,
		`UPDATE pc_sources SET payload = ? WHERE scope_id = ? AND source_type = ? AND source_id = ?`,
		[]byte(`{"name":"decoded","materialization":"captured","description":null,"content":"content","metadata":{}}`),
		"scope-a", "content", "indexed",
	); err != nil {
		t.Fatal(err)
	}
	ref, err := source.NewRef("content", "indexed")
	if err != nil {
		t.Fatal(err)
	}
	err = database.Transaction(ctx, func(tx sqlstore.DBTX) error {
		_, getErr := repository.Get(ctx, tx, "scope-a", ref)
		return getErr
	})
	var mismatch *sqlstore.IdentityMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("expected identity mismatch, got %v", err)
	}
}

func openTestDatabase(t *testing.T) *sqlstore.Database {
	t.Helper()
	config := sqlstore.DefaultSQLiteConfig(filepath.Join(t.TempDir(), "powercontext.db"))
	database, err := sqlstore.OpenSQLite(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return database
}

func contentSource(t *testing.T, id, content string, metadata map[string]any) source.ContentSource {
	t.Helper()
	capture, err := source.NewContentCapture(id, content, metadata)
	if err != nil {
		t.Fatal(err)
	}
	value, err := (source.ContentAdapter{}).Resolve(context.Background(), capture)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
