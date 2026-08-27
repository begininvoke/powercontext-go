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
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContinuousIntegrationMirrorsPythonWorkflowTopology(t *testing.T) {
	repository := filepath.Clean(filepath.Join("..", ".."))
	workflows := filepath.Join(repository, ".github", "workflows")
	allowed := map[string]bool{
		"build-artifacts.yml": true,
		"build-docker.yml":    true,
		"deploy-docs.yml":     true,
		"e2e-harness.yml":     true,
		"license-check.yml":   true,
		"master.yml":          true,
		"release-verify.yml":  true,
		"release.yml":         true,
	}
	paths, err := filepath.Glob(filepath.Join(workflows, "*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != len(allowed) {
		t.Errorf("workflow count = %d, want Python-aligned %d", len(paths), len(allowed))
	}
	for _, path := range paths {
		if !allowed[filepath.Base(path)] {
			t.Errorf("workflow %s has no Python counterpart", filepath.Base(path))
		}
	}
	required := map[string][]string{
		"master.yml": {
			"name: Main", "quality:", "run: make check", "run: make contract-test",
			"tests:", "run: make unit-test", "run: make e2e-test", "pi-package:", "check-docs:",
		},
		"e2e-harness.yml": {
			"name: E2E harness", "validate:", "acceptance:", "database: [sqlite, oceanbase]",
			"make harness-compose-acceptance", "Scan acceptance evidence", "retention-days: 14",
		},
		"deploy-docs.yml": {
			"name: Deploy documentation", "workflow_call:", "workflow_dispatch:",
			"run: make docs-build", "actions/deploy-pages@v5",
		},
		"release.yml": {
			"name: Release", "types: [published]", "release-verify:", "deploy-docs:",
			"uses: ./.github/workflows/release-verify.yml", "uses: ./.github/workflows/deploy-docs.yml",
		},
	}
	for name, values := range required {
		payload, err := os.ReadFile(filepath.Join(workflows, name))
		if err != nil {
			t.Fatal(err)
		}
		contents := string(payload)
		for _, value := range values {
			if !strings.Contains(contents, value) {
				t.Errorf("%s is missing %q", name, value)
			}
		}
	}
	for _, obsolete := range []string{"ci.yml", "provider-smoke.yml"} {
		if _, err := os.Stat(filepath.Join(workflows, obsolete)); !os.IsNotExist(err) {
			t.Errorf("obsolete workflow %s still exists", obsolete)
		}
	}
	master, err := os.ReadFile(filepath.Join(workflows, "master.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"_oracle", "frozen-oracle", "provider-smoke", "test-full"} {
		if strings.Contains(string(master), forbidden) {
			t.Errorf("master.yml contains migration or release-only concern %q", forbidden)
		}
	}
}

func TestWorkflowsReuseTheGoSetup(t *testing.T) {
	repository := filepath.Clean(filepath.Join("..", ".."))
	workflows, err := filepath.Glob(filepath.Join(repository, ".github", "workflows", "*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range workflows {
		payload, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(payload), "uses: actions/setup-go@") {
			t.Errorf("%s bypasses .github/actions/setup-go-env", filepath.Base(path))
		}
	}
}

func TestCandidateDeliveryWorkflowsExerciseTheirArtifacts(t *testing.T) {
	repository := filepath.Clean(filepath.Join("..", ".."))
	tests := map[string][]string{
		"build-artifacts.yml": {
			"workflow_dispatch:",
			"make package-standard",
			"make package-full",
			"go run ./tools/process-smoke",
			"dist/*.spdx.json",
			"retention-days: 30",
		},
		"build-docker.yml": {
			"workflow_dispatch:",
			"target: powercontext",
			"target: powercontext-full",
			"platforms: linux/amd64,linux/arm64",
			"outputs: type=oci",
			`"$image" server run`,
			"retention-days: 30",
		},
	}
	for name, requiredValues := range tests {
		payload, err := os.ReadFile(filepath.Join(repository, ".github", "workflows", name))
		if err != nil {
			t.Fatal(err)
		}
		workflow := string(payload)
		for _, required := range requiredValues {
			if !strings.Contains(workflow, required) {
				t.Errorf("%s is missing %q", name, required)
			}
		}
	}
}

func TestReleaseVerificationRechecksPublishedSurfaces(t *testing.T) {
	repository := filepath.Clean(filepath.Join("..", ".."))
	payload, err := os.ReadFile(filepath.Join(repository, ".github", "workflows", "release-verify.yml"))
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(payload)
	for _, required := range []string{
		"workflow_call:",
		"workflow_dispatch:",
		"gh release download",
		"sha256sum --check --strict SHA256SUMS",
		"go run ./tools/process-smoke",
		"docker buildx imagetools inspect",
		`"$IMAGE" server run`,
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("release verification workflow is missing %q", required)
		}
	}
}

func TestLicenseHeadersHaveOneLocalRepairAndCIContract(t *testing.T) {
	repository := filepath.Clean(filepath.Join("..", ".."))
	tests := map[string][]string{
		"Makefile": {
			"github.com/apache/skywalking-eyes/cmd/license-eye@v0.8.0",
			"license-check:",
			"header check",
			"license-fix:",
			"header fix",
		},
		filepath.Join(".github", "workflows", "license-check.yml"): {
			"pull_request:",
			"uses: apache/skywalking-eyes/header@v0.8.0",
			"config: .licenserc.yaml",
			"mode: check",
		},
		".licenserc.yaml": {
			"copyright-owner: OceanBase",
			"- '**/*_gen.go'",
			"internal/sqlstore/sqlitevec/sqlite-vec.c",
			"comment: never",
		},
	}
	for relative, requiredValues := range tests {
		payload, err := os.ReadFile(filepath.Join(repository, relative))
		if err != nil {
			t.Fatal(err)
		}
		contents := string(payload)
		for _, required := range requiredValues {
			if !strings.Contains(contents, required) {
				t.Errorf("%s is missing %q", filepath.ToSlash(relative), required)
			}
		}
	}
}
