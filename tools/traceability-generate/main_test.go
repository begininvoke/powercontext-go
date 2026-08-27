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

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunDistinguishesCaseSpecificFromFileSupportingEvidence(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "manifest.json")
	rulesPath := filepath.Join(root, "rules.json")
	outputPath := filepath.Join(root, "traceability.json")

	manifest := oracleManifest{
		OracleCommit: oracleCommit, TestFileCount: 2, TestCaseCount: 3,
		Tests: []pythonTest{
			{File: "oracle/test_group.py", Name: "test_first", Line: 1},
			{File: "oracle/test_group.py", Name: "test_second", Line: 2},
			{File: "oracle/test_host.py", Name: "test_retained", Line: 3},
		},
	}
	rules := ruleSet{
		SchemaVersion: 2, CaseSpecificEvidenceMinimum: 2,
		Files: map[string]rule{
			"oracle/test_group.py": {
				Mode:               "go-port",
				SupportingEvidence: []string{"go:group_test.go#TestGroupContract"},
				Cases: map[string][]string{
					"test_second": {"go:group_test.go#TestSecondContract"},
				},
			},
			"oracle/test_host.py": {
				Mode:         "retained-host",
				CaseEvidence: []string{"py:host_test.py#{python_test}"},
			},
		},
	}
	writeTestJSON(t, manifestPath, manifest)
	writeTestJSON(t, rulesPath, rules)
	writeTestFile(t, filepath.Join(root, "group_test.go"), "package fixture\nfunc TestGroupContract() {}\nfunc TestSecondContract() {}\n")
	writeTestFile(t, filepath.Join(root, "host_test.py"), "def test_retained():\n    pass\n")

	if err := run(root, manifestPath, rulesPath, outputPath, false); err != nil {
		t.Fatal(err)
	}
	var got table
	contents, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(contents, &got); err != nil {
		t.Fatal(err)
	}
	if got.CaseSpecificEvidenceCount != 2 || got.FileSupportingEvidenceCount != 1 {
		t.Fatalf("evidence counts = case:%d supporting:%d", got.CaseSpecificEvidenceCount, got.FileSupportingEvidenceCount)
	}
	wantLevels := []string{fileSupportingEvidence, caseSpecificEvidence, caseSpecificEvidence}
	for index, want := range wantLevels {
		if got.Entries[index].EvidenceLevel != want {
			t.Fatalf("entry %d level = %q, want %q", index, got.Entries[index].EvidenceLevel, want)
		}
	}
}

func TestRunRejectsCaseSpecificEvidenceRegression(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "manifest.json")
	rulesPath := filepath.Join(root, "rules.json")
	outputPath := filepath.Join(root, "traceability.json")
	writeTestJSON(t, manifestPath, oracleManifest{
		OracleCommit: oracleCommit, TestFileCount: 1, TestCaseCount: 1,
		Tests: []pythonTest{{File: "oracle/test_group.py", Name: "test_first", Line: 1}},
	})
	writeTestJSON(t, rulesPath, ruleSet{
		SchemaVersion: 2, CaseSpecificEvidenceMinimum: 1,
		Files: map[string]rule{"oracle/test_group.py": {
			Mode: "go-port", SupportingEvidence: []string{"go:group_test.go#TestGroupContract"},
		}},
	})
	writeTestFile(t, filepath.Join(root, "group_test.go"), "package fixture\nfunc TestGroupContract() {}\n")

	err := run(root, manifestPath, rulesPath, outputPath, false)
	if err == nil || !strings.Contains(err.Error(), "case-specific evidence count = 0, declared checkpoint = 1") {
		t.Fatalf("run error = %v", err)
	}
}

func TestRunRejectsCaseTemplateHiddenAsSupportingEvidence(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "manifest.json")
	rulesPath := filepath.Join(root, "rules.json")
	outputPath := filepath.Join(root, "traceability.json")
	writeTestJSON(t, manifestPath, oracleManifest{
		OracleCommit: oracleCommit, TestFileCount: 1, TestCaseCount: 1,
		Tests: []pythonTest{{File: "oracle/test_group.py", Name: "test_first", Line: 1}},
	})
	writeTestJSON(t, rulesPath, ruleSet{
		SchemaVersion: 2,
		Files: map[string]rule{"oracle/test_group.py": {
			Mode: "retained-host", SupportingEvidence: []string{"py:host_test.py#{python_test}"},
		}},
	})
	writeTestFile(t, filepath.Join(root, "host_test.py"), "def test_first():\n    pass\n")

	err := run(root, manifestPath, rulesPath, outputPath, false)
	if err == nil || !strings.Contains(err.Error(), "declare case_evidence instead") {
		t.Fatalf("run error = %v", err)
	}
}

func writeTestJSON(t *testing.T, path string, value any) {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, path, string(payload))
}

func writeTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
