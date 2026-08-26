package conformance_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ob-labs/powercontext-go/internal/sqlstore"
	"github.com/ob-labs/powercontext-go/source"
)

const (
	authoritySHA256 = "ac9b78b07b84097c51cbe2a08258a03e9e1c9ec57786fb2a50e2c2570ced715e"
	oracleScope     = "project:python-oracle"
)

func TestPythonSQLiteAuthorityCanBeReadAndExtendedByGo(t *testing.T) {
	fixture := filepath.Join("testdata", "python-v0.0.1", "authority.db")
	contents, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(contents)); got != authoritySHA256 {
		t.Fatalf("authority fixture SHA-256 = %s, want %s", got, authoritySHA256)
	}
	working := filepath.Join(t.TempDir(), "authority.db")
	if err := os.WriteFile(working, contents, 0o600); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	database, err := sqlstore.OpenSQLite(ctx, sqlstore.DefaultSQLiteConfig(working))
	if err != nil {
		t.Fatal(err)
	}
	sources, err := sqlstore.NewSourceRepository(sqlstore.SQLiteDialect, sqlstore.ContentSourceCodec())
	if err != nil {
		t.Fatal(err)
	}
	artifacts, err := sqlstore.NewArtifactRepository(
		sqlstore.SQLiteDialect,
		sqlstore.MemoryArtifactCodec(),
		sqlstore.ExperienceArtifactCodec(),
		sqlstore.SkillArtifactCodec(),
		sqlstore.HandoffArtifactCodec(),
	)
	if err != nil {
		t.Fatal(err)
	}
	pythonRef, _ := source.NewRef("content", "capture-python-1")
	var stored sqlstore.StoredSource
	if err := database.Transaction(ctx, func(tx sqlstore.DBTX) error {
		var getErr error
		stored, getErr = sources.Get(ctx, tx, oracleScope, pythonRef)
		return getErr
	}); err != nil {
		t.Fatal(err)
	}
	pythonSource, ok := stored.Value.(source.ContentSource)
	if !ok || pythonSource.Content() != "Use one atomic café composition boundary." || stored.JournalPosition != 1 {
		t.Fatalf("Python Source = %#v", stored)
	}

	memoryRepository, err := sqlstore.NewMemoryRepository(database, oracleScope, artifacts, nil)
	if err != nil {
		t.Fatal(err)
	}
	head, err := memoryRepository.Latest(ctx, "memory")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := memoryRepository.Entries(ctx, head.Ref())
	if err != nil {
		t.Fatal(err)
	}
	if head.Revision() != 1 || len(entries) != 1 || entries[0].Text != "Use one atomic café composition boundary." {
		t.Fatalf("Python Memory = %#v, entries = %#v", head, entries)
	}

	capture, err := source.NewContentCapture(
		"capture-go-1",
		"Go wrote this row into the Python authority database.",
		map[string]any{"origin": "go"},
	)
	if err != nil {
		t.Fatal(err)
	}
	goSource, err := (source.ContentAdapter{}).Resolve(ctx, capture)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Transaction(ctx, func(tx sqlstore.DBTX) error {
		value, addErr := sources.Add(ctx, tx, oracleScope, goSource)
		if addErr == nil && value.JournalPosition != 2 {
			return fmt.Errorf("Go Source journal position = %d, want 2", value.JournalPosition)
		}
		return addErr
	}); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(ctx); err != nil {
		t.Fatal(err)
	}

	python := os.Getenv("POWERCONTEXT_ORACLE_PYTHON")
	if python == "" {
		t.Log("POWERCONTEXT_ORACLE_PYTHON is unset; Python back-read is exercised by the Oracle CI job")
		return
	}
	command := exec.Command(python, "python_oracle_fixture.py", "verify", working)
	command.Dir = "."
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("Python could not read the Go-extended database: %v\n%s", err, output)
	}
}
