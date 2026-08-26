package skill

import (
	"testing"
	"unicode/utf8"
)

func FuzzAgentSkillFrontmatterParser(f *testing.F) {
	f.Add([]byte("---\nname: example\ndescription: Use for examples.\n---\n\nBody.\n"), "example", string(CodexAgent))
	f.Add([]byte("---\ndescription: 'Claude example'\n---\n"), "package-name", string(ClaudeCodeAgent))
	f.Add([]byte("---\nname: [not, a, scalar]\n---\n"), "example", string(CodexAgent))
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
		if firstErr == nil && (!utf8.ValidString(firstName) || !utf8.ValidString(firstDescription) ||
			firstName == "" || firstDescription == "") {
			t.Fatalf("parser accepted invalid metadata name=%q description=%q", firstName, firstDescription)
		}
	})
}
