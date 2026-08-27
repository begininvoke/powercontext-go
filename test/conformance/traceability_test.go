// Copyright (c) 2026 OceanBase.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package conformance_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

type tracedPythonTest struct {
	File string `json:"file"`
	Name string `json:"name"`
	Line int    `json:"line"`
}

type traceEvidence struct {
	Kind string `json:"kind"`
	File string `json:"file"`
	Test string `json:"test"`
}

type traceEntry struct {
	Python        tracedPythonTest `json:"python"`
	Mode          string           `json:"mode"`
	EvidenceLevel string           `json:"evidence_level"`
	Evidence      []traceEvidence  `json:"evidence"`
}

type traceTable struct {
	SchemaVersion               int          `json:"schema_version"`
	OracleCommit                string       `json:"oracle_commit"`
	PythonTestFileCount         int          `json:"python_test_file_count"`
	PythonTestCaseCount         int          `json:"python_test_case_count"`
	CaseSpecificEvidenceCount   int          `json:"case_specific_evidence_count"`
	FileSupportingEvidenceCount int          `json:"file_supporting_evidence_count"`
	CaseSpecificEvidenceMinimum int          `json:"case_specific_evidence_minimum"`
	Entries                     []traceEntry `json:"entries"`
}

func TestEveryFrozenPythonTestHasAuditableEvidenceLevel(t *testing.T) {
	var manifest struct {
		OracleCommit  string             `json:"oracle_commit"`
		TestFileCount int                `json:"test_file_count"`
		TestCaseCount int                `json:"test_case_count"`
		Tests         []tracedPythonTest `json:"tests"`
	}
	decodeJSONFile(t, filepath.Join("testdata", "python-v0.0.2", "manifest.json"), &manifest)
	var trace traceTable
	decodeJSONFile(t, "traceability.json", &trace)
	var rules struct {
		SchemaVersion               int `json:"schema_version"`
		CaseSpecificEvidenceMinimum int `json:"case_specific_evidence_minimum"`
	}
	decodeJSONFile(t, "traceability-rules.json", &rules)
	if trace.SchemaVersion != 2 || rules.SchemaVersion != trace.SchemaVersion ||
		trace.OracleCommit != manifest.OracleCommit || trace.OracleCommit != oracleCommit {
		t.Fatalf("unexpected traceability identity: schema=%d commit=%s", trace.SchemaVersion, trace.OracleCommit)
	}
	if trace.PythonTestFileCount != manifest.TestFileCount || trace.PythonTestCaseCount != manifest.TestCaseCount || len(trace.Entries) != len(manifest.Tests) {
		t.Fatalf("traceability inventory = %d files/%d cases/%d entries, want %d/%d/%d", trace.PythonTestFileCount, trace.PythonTestCaseCount, len(trace.Entries), manifest.TestFileCount, manifest.TestCaseCount, len(manifest.Tests))
	}
	if trace.CaseSpecificEvidenceMinimum != rules.CaseSpecificEvidenceMinimum ||
		trace.CaseSpecificEvidenceCount != trace.CaseSpecificEvidenceMinimum ||
		trace.CaseSpecificEvidenceCount+trace.FileSupportingEvidenceCount != trace.PythonTestCaseCount {
		t.Fatalf(
			"evidence summary = case:%d supporting:%d minimum:%d (rules:%d)",
			trace.CaseSpecificEvidenceCount, trace.FileSupportingEvidenceCount,
			trace.CaseSpecificEvidenceMinimum, rules.CaseSpecificEvidenceMinimum,
		)
	}
	seen := make(map[string]struct{}, len(trace.Entries))
	root := filepath.Join("..", "..")
	caseSpecificCount := 0
	for index, entry := range trace.Entries {
		if entry.Python != manifest.Tests[index] {
			t.Fatalf("traceability entry %d = %#v, want %#v", index, entry.Python, manifest.Tests[index])
		}
		key := entry.Python.File + "#" + entry.Python.Name
		if _, exists := seen[key]; exists {
			t.Fatalf("duplicate traceability entry %s", key)
		}
		seen[key] = struct{}{}
		if entry.Mode != "go-port" && entry.Mode != "retained-host" && entry.Mode != "cross-layer" {
			t.Fatalf("%s has invalid mode %q", key, entry.Mode)
		}
		if entry.EvidenceLevel != "case-specific" && entry.EvidenceLevel != "file-supporting" {
			t.Fatalf("%s has invalid evidence level %q", key, entry.EvidenceLevel)
		}
		if entry.EvidenceLevel == "case-specific" {
			caseSpecificCount++
		}
		if len(entry.Evidence) == 0 {
			t.Fatalf("%s has no evidence", key)
		}
		for _, evidence := range entry.Evidence {
			assertEvidenceResolves(t, root, key, evidence)
		}
	}
	if caseSpecificCount != trace.CaseSpecificEvidenceCount {
		t.Fatalf("counted %d case-specific entries, summary says %d", caseSpecificCount, trace.CaseSpecificEvidenceCount)
	}
}

var (
	goTraceTest = regexp.MustCompile(`^func\s+(Test[A-Za-z0-9_]+)\s*\(`)
	pyTraceTest = regexp.MustCompile(`^\s*(?:async\s+)?def\s+(test_[A-Za-z0-9_]+)\s*\(`)
)

func assertEvidenceResolves(t *testing.T, root, source string, evidence traceEvidence) {
	t.Helper()
	if evidence.Kind != "go" && evidence.Kind != "py" && evidence.Kind != "ts" {
		t.Fatalf("%s has invalid evidence kind %q", source, evidence.Kind)
	}
	clean := filepath.Clean(filepath.FromSlash(evidence.File))
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		t.Fatalf("%s evidence escapes repository: %s", source, evidence.File)
	}
	contents, err := os.ReadFile(filepath.Join(root, clean))
	if err != nil {
		t.Fatalf("%s evidence cannot be read: %v", source, err)
	}
	if evidence.Kind == "ts" {
		text := string(contents)
		if !strings.Contains(text, "it('"+evidence.Test+"'") && !strings.Contains(text, `it("`+evidence.Test+`"`) {
			t.Fatalf("%s evidence %s does not declare TypeScript test %q", source, evidence.File, evidence.Test)
		}
		return
	}
	pattern := goTraceTest
	if evidence.Kind == "py" {
		pattern = pyTraceTest
	}
	scanner := bufio.NewScanner(bytes.NewReader(contents))
	for scanner.Scan() {
		match := pattern.FindStringSubmatch(scanner.Text())
		if len(match) == 2 && match[1] == evidence.Test {
			return
		}
	}
	t.Fatalf("%s evidence %s does not declare %s test %q", source, evidence.File, evidence.Kind, evidence.Test)
}

func decodeJSONFile(t *testing.T, path string, target any) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(contents, target); err != nil {
		t.Fatal(err)
	}
}
