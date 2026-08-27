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
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestRepositoryLayoutKeepsProductImplementationInternal(t *testing.T) {
	t.Parallel()
	root := repositoryRoot(t)

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
