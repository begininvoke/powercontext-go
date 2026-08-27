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
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "github.com/ob-labs/powercontext-go"

var allowedGoRoots = map[string]struct{}{
	"api": {}, "artifact": {}, "client": {}, "cmd": {}, "inference": {},
	"internal": {}, "openapi": {}, "server": {}, "source": {}, "test": {},
	"tools": {}, "trigger": {},
}

type dependencyRule struct {
	owner     string
	forbidden []string
}

var dependencyRules = []dependencyRule{
	{
		owner: "source",
		forbidden: []string{
			"internal/sqlstore", "internal/runtime", "internal/endpoint",
			"internal/httpapi", "internal/mcpapi", "internal/webui",
			"internal/modelprovider", "server",
		},
	},
	{
		owner: "artifact",
		forbidden: []string{
			"internal/sqlstore", "internal/runtime", "internal/endpoint",
			"internal/httpapi", "internal/mcpapi", "internal/webui",
			"internal/modelprovider", "server",
		},
	},
	{
		owner: "trigger",
		forbidden: []string{
			"internal/sqlstore", "internal/runtime", "internal/endpoint",
			"internal/httpapi", "internal/mcpapi", "internal/webui",
			"internal/modelprovider", "server",
		},
	},
	{
		owner: "inference",
		forbidden: []string{
			"internal/sqlstore", "internal/runtime", "internal/endpoint",
			"internal/httpapi", "internal/mcpapi", "internal/webui",
			"internal/modelprovider", "server",
		},
	},
	{
		owner: "internal/sqlstore",
		forbidden: []string{
			"internal/runtime", "internal/endpoint", "internal/httpapi",
			"internal/mcpapi", "internal/webui", "server",
		},
	},
	{
		owner: "internal/runtime",
		forbidden: []string{
			"internal/sqlstore", "internal/endpoint", "internal/httpapi",
			"internal/mcpapi", "internal/webui", "internal/modelprovider", "server",
		},
	},
	{
		owner: "internal/endpoint",
		forbidden: []string{
			"internal/sqlstore", "internal/httpapi", "internal/mcpapi",
			"internal/webui", "internal/modelprovider", "internal/scheduler", "server",
		},
	},
	{
		owner: "internal/httpapi",
		forbidden: []string{
			"internal/sqlstore", "internal/runtime", "internal/modelprovider",
			"internal/scheduler", "server",
		},
	},
	{
		owner: "internal/mcpapi",
		forbidden: []string{
			"internal/sqlstore", "internal/runtime", "internal/modelprovider",
			"internal/scheduler", "server",
		},
	},
	{
		owner: "internal/webui",
		forbidden: []string{
			"internal/sqlstore", "internal/runtime", "internal/modelprovider",
			"internal/scheduler", "server",
		},
	},
}

func TestRepositoryDependencyDirection(t *testing.T) {
	t.Parallel()
	root := repositoryRoot(t)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && ignoredRepositoryDirectory(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		parts := strings.Split(relative, "/")
		if len(parts) == 1 {
			t.Errorf("repository root must not contain Go package file %s", relative)
			return nil
		}
		if _, ok := allowedGoRoots[parts[0]]; !ok {
			t.Errorf("Go package %s is outside the deliberate top-level package roots", relative)
		}
		return checkFileDependencies(t, root, relative)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func checkFileDependencies(t *testing.T, root, relative string) error {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, filepath.FromSlash(relative)), nil, parser.ImportsOnly)
	if err != nil {
		return err
	}
	owner := filepath.ToSlash(filepath.Dir(relative))
	for _, imported := range parsed.Imports {
		path, err := strconv.Unquote(imported.Path.Value)
		if err != nil {
			return err
		}
		for _, rule := range dependencyRules {
			if !packageOrChild(owner, rule.owner) {
				continue
			}
			for _, forbidden := range rule.forbidden {
				if packageOrChild(path, modulePath+"/"+forbidden) {
					t.Errorf("%s imports forbidden dependency %s", relative, path)
				}
			}
		}
	}
	return nil
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate repository conformance tests")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}

func ignoredRepositoryDirectory(name string) bool {
	switch name {
	case ".git", ".gitnexus", ".idea", ".pytest_cache", ".ruff_cache", ".venv",
		"bin", "coverage", "dist", "node_modules", "__pycache__":
		return true
	default:
		return false
	}
}

func packageOrChild(value, parent string) bool {
	return value == parent || strings.HasPrefix(value, parent+"/")
}
