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

package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackageFingerprintErrorsUseAgentSkillWording(t *testing.T) {
	t.Run("unsupported file count", func(t *testing.T) {
		_, err := packageFingerprint(t.TempDir())
		assertAgentSkillPackageError(t, err, "Agent Skill package has an unsupported file count")
	})

	t.Run("symlink", func(t *testing.T) {
		packagePath := t.TempDir()
		target := filepath.Join(t.TempDir(), "outside.txt")
		if err := os.WriteFile(target, []byte("outside\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(packagePath, "linked.txt")); err != nil {
			t.Fatal(err)
		}
		_, err := packageFingerprint(packagePath)
		assertAgentSkillPackageError(t, err, "Agent Skill packages containing symlinks are not supported")
	})
}

func assertAgentSkillPackageError(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || err.Error() != want {
		t.Fatalf("package fingerprint error = %v, want %q", err, want)
	}
	if strings.Contains(err.Error(), "Codex") {
		t.Fatalf("package fingerprint error is provider-specific: %v", err)
	}
}
