package conformance_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type oracleManifest struct {
	SchemaVersion      int               `json:"schema_version"`
	OracleCommit       string            `json:"oracle_commit"`
	OpenAPISHA256      string            `json:"openapi_sha256"`
	SQLiteSchemaSHA256 string            `json:"sqlite_schema_sha256"`
	FixtureSHA256      map[string]string `json:"fixture_sha256"`
	TestFileCount      int               `json:"test_file_count"`
	TestCaseCount      int               `json:"test_case_count"`
}

func TestFrozenOracleManifestAndFixtureHashes(t *testing.T) {
	root := filepath.Join("testdata", "python-v0.0.2")
	contents, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest oracleManifest
	if err := json.Unmarshal(contents, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.SchemaVersion != 3 || manifest.OracleCommit != oracleCommit {
		t.Fatalf("unexpected Oracle manifest identity: %#v", manifest)
	}
	if manifest.TestFileCount != 109 || manifest.TestCaseCount != 622 {
		t.Fatalf("frozen Python test inventory = %d files/%d cases", manifest.TestFileCount, manifest.TestCaseCount)
	}
	if len(manifest.FixtureSHA256) != 6 {
		t.Fatalf("fixture hash inventory = %d, want 6", len(manifest.FixtureSHA256))
	}
	for name, expected := range manifest.FixtureSHA256 {
		if got := fileSHA256(t, filepath.Join(root, name)); got != expected {
			t.Errorf("%s SHA-256 = %s, want %s", name, got, expected)
		}
	}
	// This directory is the active immutable v0.0.2 interoperability fixture.
	// The active OpenAPI contract is pinned independently by openapi tests and
	// must not be forced back to the historical fixture hash.
	var authority struct {
		SchemaSHA256 string `json:"schema_sha256"`
	}
	authorityBytes, err := os.ReadFile(filepath.Join(root, "authority-rows.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(authorityBytes, &authority); err != nil {
		t.Fatal(err)
	}
	if authority.SchemaSHA256 != manifest.SQLiteSchemaSHA256 {
		t.Errorf("SQLite schema SHA-256 = %s, want %s", authority.SchemaSHA256, manifest.SQLiteSchemaSHA256)
	}
}

func fileSHA256(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(contents)
	return hex.EncodeToString(digest[:])
}
