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
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var actionCommit = regexp.MustCompile(`^[0-9a-f]{40}$`)

func TestGitHubActionsArePinnedToImmutableCommits(t *testing.T) {
	repository := filepath.Clean(filepath.Join("..", ".."))
	github := filepath.Join(repository, ".github")
	err := filepath.WalkDir(github, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || (filepath.Ext(path) != ".yml" && filepath.Ext(path) != ".yaml") {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		scanner := bufio.NewScanner(file)
		line := 0
		for scanner.Scan() {
			line++
			value := strings.TrimSpace(scanner.Text())
			if !strings.HasPrefix(value, "uses:") && !strings.HasPrefix(value, "- uses:") {
				continue
			}
			value = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(value, "- "), "uses:"))
			value = strings.Trim(value, `"'`)
			if strings.HasPrefix(value, "./") {
				continue
			}
			at := strings.LastIndexByte(value, '@')
			if at < 1 {
				t.Errorf("%s:%d action has no revision: %s", filepath.ToSlash(path), line, value)
				continue
			}
			fields := strings.Fields(value[at+1:])
			if len(fields) == 0 || !actionCommit.MatchString(fields[0]) {
				t.Errorf("%s:%d action is not pinned to a 40-character commit: %s", filepath.ToSlash(path), line, value)
			}
		}
		return scanner.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestWorkflowsReuseTheLockedGoSetup(t *testing.T) {
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
			"uses: ./.github/actions/setup-go-env",
			"run: make license-check",
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
