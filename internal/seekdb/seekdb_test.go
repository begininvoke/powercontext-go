//go:build cgo && (darwin || linux)

package seekdb

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCanonicalPathResolvesExistingSymlinkAncestors(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	realRoot := filepath.Join(root, "real")
	if err := os.Mkdir(realRoot, 0o750); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(realRoot, link); err != nil {
		t.Fatal(err)
	}
	got, err := canonicalPath(filepath.Join(link, "missing", "seekdb"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := canonicalPath(filepath.Join(realRoot, "missing", "seekdb"))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("canonical path = %q, want %q", got, want)
	}
}

func TestLibraryCandidatesPreserveExplicitChoice(t *testing.T) {
	t.Parallel()
	configured := filepath.Join(t.TempDir(), "libseekdb.test")
	candidates, err := libraryCandidates(configured)
	if err != nil {
		t.Fatal(err)
	}
	want, err := canonicalPath(configured)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0] != want {
		t.Fatalf("library candidates = %v", candidates)
	}
}

func TestOpenReportsUnavailableExplicitLibrary(t *testing.T) {
	t.Parallel()
	_, err := Open(t.Context(), Config{
		Path: filepath.Join(t.TempDir(), "seekdb"), LibraryPath: filepath.Join(t.TempDir(), "missing-library"),
	})
	var unavailable *UnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("open error = %T %v, want UnavailableError", err, err)
	}
}
