package conformance_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

func TestRepositoryLayoutKeepsProductImplementationInternal(t *testing.T) {
	t.Parallel()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate repository layout test")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))

	forbidden := []string{
		"common", "contextpack", "handoffreport", "helpers", "pkg",
		"review", "runtime", "src", "stats", "utils", "work",
	}
	for _, name := range forbidden {
		_, err := os.Stat(filepath.Join(root, name))
		if err == nil {
			t.Errorf("product implementation or catch-all directory returned to repository root: %s", name)
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Errorf("inspect forbidden root directory %s: %v", name, err)
		}
	}

	required := []string{
		"internal/contextpack",
		"internal/handoffreport",
		"internal/review",
		"internal/runtime",
		"internal/sqlstore/seekdb",
		"internal/sqlstore/sqlitevec",
		"internal/stats",
		"internal/work",
	}
	for _, name := range required {
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			t.Errorf("required ownership directory %s is missing: %v", name, err)
		} else if !info.IsDir() {
			t.Errorf("required ownership path %s is not a directory", name)
		}
	}

	rootGoFiles, err := filepath.Glob(filepath.Join(root, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(rootGoFiles)
	if len(rootGoFiles) != 0 {
		t.Fatalf("repository root must not recreate a facade package: %v", rootGoFiles)
	}
}
