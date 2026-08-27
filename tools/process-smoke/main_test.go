package main

import (
	"slices"
	"strings"
	"testing"
)

func TestIsolatedEnvironmentRemovesPowerContextContamination(t *testing.T) {
	source := []string{
		"PATH=/usr/bin",
		"POWERCONTEXT_HOME=/private/user-data",
		"POWERCONTEXT_SERVER_AUTH_TOKEN=secret",
		"POWERCONTEXT_CLIENT_API_TOKEN=secret",
		"UNRELATED=value=with=equals",
		"MALFORMED",
	}
	result := isolatedEnvironment(source, "/isolated/home")
	for _, forbidden := range []string{"/private/user-data", "secret", "MALFORMED"} {
		if strings.Contains(strings.Join(result, "\n"), forbidden) {
			t.Fatalf("isolated environment contains %q: %v", forbidden, result)
		}
	}
	for _, expected := range []string{
		"PATH=/usr/bin",
		"UNRELATED=value=with=equals",
		"POWERCONTEXT_HOME=/isolated/home",
		"POWERCONTEXT_SERVER_LOGGING_FORMAT=json",
		"POWERCONTEXT_SERVER_LOGGING_ACCESS=true",
	} {
		if !slices.Contains(result, expected) {
			t.Fatalf("isolated environment is missing %q: %v", expected, result)
		}
	}
}

func TestFrozenBaseToolNamesAreUnique(t *testing.T) {
	seen := make(map[string]struct{}, len(baseToolNames))
	for _, name := range baseToolNames {
		if _, duplicate := seen[name]; duplicate {
			t.Fatalf("duplicate MCP tool %q", name)
		}
		seen[name] = struct{}{}
	}
	if len(seen) != 20 {
		t.Fatalf("base MCP tools = %d, want 20", len(seen))
	}
}
