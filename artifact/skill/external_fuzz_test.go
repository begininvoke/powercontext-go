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
	"testing"
	"unicode/utf8"
)

func FuzzAgentSkillFrontmatterParser(f *testing.F) {
	f.Add([]byte("---\nname: example\ndescription: Use for examples.\n---\n\nBody.\n"), "example", string(CodexAgent))
	f.Add([]byte("---\ndescription: 'Claude example'\n---\n"), "package-name", string(ClaudeCodeAgent))
	f.Add([]byte("---\nname: [not, a, scalar]\n---\n"), "example", string(CodexAgent))
	f.Add([]byte("---\nname: \"\"\ndescription: Empty name.\n---\n"), "example", string(CodexAgent))
	f.Add([]byte("---\nname: 'unterminated\ndescription: Invalid name.\n---\n"), "example", string(CodexAgent))
	f.Add([]byte{0xff, 0xfe, 0xfd}, "example", string(CodexAgent))
	f.Fuzz(func(t *testing.T, contents []byte, packageName, rawAgentKind string) {
		if len(contents) > MaxExternalManifestBytes || len(packageName) > 512 || len(rawAgentKind) > 64 {
			t.Skip()
		}
		agentKind := AgentKind(rawAgentKind)
		if agentKind != CodexAgent && agentKind != ClaudeCodeAgent {
			agentKind = CodexAgent
		}
		firstName, firstDescription, firstErr := parseSkillMetadata(contents, packageName, agentKind)
		secondName, secondDescription, secondErr := parseSkillMetadata(contents, packageName, agentKind)
		if (firstErr == nil) != (secondErr == nil) || firstName != secondName || firstDescription != secondDescription {
			t.Fatal("frontmatter parsing is not deterministic")
		}
		if firstErr == nil && (!utf8.ValidString(firstName) || !utf8.ValidString(firstDescription)) {
			t.Fatalf("parser produced invalid UTF-8 metadata name=%q description=%q", firstName, firstDescription)
		}
		if firstErr == nil && (firstName == "" || firstDescription == "") {
			// A fuzzed Claude Code package name may be empty even though a live
			// filesystem package cannot have an empty base name. The public
			// registration boundary must still reject all empty metadata.
			_, err := NewRegistration(
				"codex:project:fuzz/package", string(agentKind), string(agentKind), "fuzz-host", UserScope,
				"/fuzz/package", "0000000000000000000000000000000000000000000000000000000000000000",
				firstName, firstDescription,
			)
			if err == nil {
				t.Fatalf("registration accepted empty metadata name=%q description=%q", firstName, firstDescription)
			}
		}
	})
}
