package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestSetupAndDoctorExposeCurrentHostMatrix(t *testing.T) {
	command := newCommandWithAllDependencies(
		VersionInfo{Version: "test"}, &strings.Builder{}, &strings.Builder{}, nil, nil, &scriptedSystemCommands{t: t},
	)
	want := []string{"claude-code", "codex", "dsh", "hermes", "openclaw", "opencode", "pi"}
	for _, parentName := range []string{"setup", "doctor"} {
		parent, _, err := command.Find([]string{parentName})
		if err != nil {
			t.Fatal(err)
		}
		got := make([]string, 0, len(parent.Commands()))
		for _, child := range parent.Commands() {
			if !child.IsAdditionalHelpTopicCommand() {
				got = append(got, child.Name())
			}
		}
		slices.Sort(got)
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("%s integrations = %v, want %v", parentName, got, want)
		}
	}
}

func TestSetupClaudeCodeIsTransactionalAndMergesSettings(t *testing.T) {
	config := filepath.Join(t.TempDir(), "claude")
	t.Setenv("CLAUDE_CONFIG_DIR", config)
	if err := os.MkdirAll(config, 0o700); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(config, "settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{"unrelated":{"preserved":true}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	settingsPath, err := resolvePath(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	commands := &scriptedSystemCommands{
		t: t, paths: map[string]string{"claude": "/usr/bin/claude"},
		results: []systemCommandResult{
			{output: `[]`},
			{output: `[]`},
			{},
			{},
			{output: `[{"id":"powercontext@powercontext","version":"0.1.0","enabled":true}]`},
		},
	}
	stdout, stderr, err := executeSystemCLI(t, nil, commands,
		"setup", "claude-code", "--source", "ob-labs/powercontext-go", "--ref", "tested-ref",
		"--server-url", "http://127.0.0.1:9000/mcp/", "--no-capture-prompts", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr, "no changes made yet") || !strings.Contains(stderr, settingsPath) {
		t.Fatalf("setup plan = %q", stderr)
	}
	payload := decodeSystemOutput(t, stdout)
	if payload["plugin_version"] != "0.1.0" || payload["settings_file"] != settingsPath {
		t.Fatalf("setup output = %#v", payload)
	}
	var settings map[string]any
	content, readErr := os.ReadFile(settingsPath)
	if readErr != nil || json.Unmarshal(content, &settings) != nil {
		t.Fatalf("settings = %q, error = %v", content, readErr)
	}
	if settings["unrelated"].(map[string]any)["preserved"] != true {
		t.Fatalf("unrelated settings were lost: %#v", settings)
	}
	options := settings["pluginConfigs"].(map[string]any)[claudePluginID].(map[string]any)["options"].(map[string]any)
	if options["server_url"] != "http://127.0.0.1:9000" || options["capture_prompts"] != false {
		t.Fatalf("plugin options = %#v", options)
	}
	wantCalls := []string{
		"/usr/bin/claude plugin marketplace list --json",
		"/usr/bin/claude plugin list --json",
		"/usr/bin/claude plugin marketplace add ob-labs/powercontext-go@tested-ref --scope user",
		"/usr/bin/claude plugin install powercontext@powercontext --scope user",
		"/usr/bin/claude plugin list --json",
	}
	if got := commandCallStrings(commands.calls); fmt.Sprint(got) != fmt.Sprint(wantCalls) {
		t.Fatalf("commands = %v, want %v", got, wantCalls)
	}
}

func TestSetupClaudeCodeRollsBackNewObjectsAfterVerificationFailure(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(t.TempDir(), "claude"))
	commands := &scriptedSystemCommands{
		t: t, paths: map[string]string{"claude": "/usr/bin/claude"},
		results: []systemCommandResult{{output: `[]`}, {output: `[]`}, {}, {}, {output: `[]`}, {}, {}},
	}
	_, _, err := executeSystemCLI(t, nil, commands, "setup", "claude-code")
	if err == nil || !strings.Contains(err.Error(), "enabled PowerContext plugin") {
		t.Fatalf("setup error = %v", err)
	}
	got := commandCallStrings(commands.calls)
	if !slices.Contains(got, "/usr/bin/claude plugin uninstall powercontext@powercontext --scope user") ||
		got[len(got)-1] != "/usr/bin/claude plugin marketplace remove powercontext" {
		t.Fatalf("rollback commands = %v", got)
	}
}

func TestSetupClaudeCodeRejectsUnsafeURLBeforeHostInspection(t *testing.T) {
	commands := &scriptedSystemCommands{t: t, paths: map[string]string{"claude": "/usr/bin/claude"}}
	_, _, err := executeSystemCLI(t, nil, commands,
		"setup", "claude-code", "--server-url", "http://memory.example.com")
	if err == nil || len(commands.calls) != 0 {
		t.Fatalf("error = %v, calls = %v", err, commands.calls)
	}
}

func TestSetupPiInstallsBeforeRemovingSupersededPackages(t *testing.T) {
	checkout := filepath.Join(t.TempDir(), "checkout")
	packagePath := writePiPackage(t, checkout)
	t.Setenv("POWERCONTEXT_HOME", filepath.Join(t.TempDir(), "data"))
	listing := "User packages:\n  " + packagePath + "\n    " + packagePath + "\n"
	commands := &scriptedSystemCommands{
		t: t, paths: map[string]string{"pi": "/usr/bin/pi"},
		results: []systemCommandResult{{}, {output: listing}, {output: listing}},
	}
	stdout, _, err := executeSystemCLI(t, nil, commands, "setup", "pi", "--source", checkout, "--json")
	if err != nil {
		t.Fatal(err)
	}
	if decodeSystemOutput(t, stdout)["package_path"] != packagePath {
		t.Fatalf("setup output = %s", stdout)
	}
	want := []string{"/usr/bin/pi install " + packagePath, "/usr/bin/pi list", "/usr/bin/pi list"}
	if got := commandCallStrings(commands.calls); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("commands = %v, want %v", got, want)
	}
}

func TestSetupOpenCodeProtectsAndPublishesOwnedSkill(t *testing.T) {
	checkout := filepath.Join(t.TempDir(), "checkout")
	plugin := writeOpenCodePlugin(t, checkout)
	config := filepath.Join(t.TempDir(), "config")
	t.Setenv("POWERCONTEXT_HOME", filepath.Join(t.TempDir(), "data"))
	commands := &scriptedSystemCommands{
		t: t, paths: map[string]string{"opencode": "/usr/bin/opencode"},
		results: []systemCommandResult{{output: "1.18.21\n"}, {output: "config     " + config + "\n"}, {}},
	}
	if _, _, err := executeSystemCLI(t, nil, commands, "setup", "opencode", "--source", checkout); err != nil {
		t.Fatal(err)
	}
	skill := filepath.Join(config, "skills", "project-context")
	if !ownedOpenCodeSkill(skill) {
		t.Fatalf("skill %q is not marked as PowerContext-owned", skill)
	}
	content, err := os.ReadFile(filepath.Join(skill, "SKILL.md"))
	if err != nil || string(content) != "project context\n" {
		t.Fatalf("skill content = %q, error = %v", content, err)
	}
	if got := commands.calls[2].String(); got != "/usr/bin/opencode plugin "+plugin+" --global --force" {
		t.Fatalf("install command = %q", got)
	}
}

func TestSetupOpenCodeRefusesUnownedSkillBeforePluginMutation(t *testing.T) {
	checkout := filepath.Join(t.TempDir(), "checkout")
	writeOpenCodePlugin(t, checkout)
	config := filepath.Join(t.TempDir(), "config")
	target := filepath.Join(config, "skills", "project-context")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "SKILL.md"), []byte("user owned\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("POWERCONTEXT_HOME", filepath.Join(t.TempDir(), "data"))
	commands := &scriptedSystemCommands{
		t: t, paths: map[string]string{"opencode": "/usr/bin/opencode"},
		results: []systemCommandResult{{output: "1.18.21\n"}, {output: "config " + config + "\n"}},
	}
	_, _, err := executeSystemCLI(t, nil, commands, "setup", "opencode", "--source", checkout)
	if err == nil || !strings.Contains(err.Error(), "not owned by PowerContext") || len(commands.calls) != 2 {
		t.Fatalf("error = %v, commands = %v", err, commands.calls)
	}
}

func TestSetupHermesStagesDoctorAndAtomicallyReplacesPlugin(t *testing.T) {
	checkout := filepath.Join(t.TempDir(), "checkout")
	writeHermesPlugin(t, checkout)
	home := filepath.Join(t.TempDir(), "hermes")
	target := filepath.Join(home, "plugins", hermesPluginName)
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "stale.py"), []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	home, err := resolvePath(home)
	if err != nil {
		t.Fatal(err)
	}
	target = filepath.Join(home, "plugins", hermesPluginName)
	t.Setenv("HERMES_HOME", home)
	t.Setenv("POWERCONTEXT_HOME", filepath.Join(t.TempDir(), "data"))
	commands := &scriptedSystemCommands{
		t: t, paths: map[string]string{"hermes": "/usr/bin/hermes"},
		results: []systemCommandResult{
			{output: "Hermes Agent v0.20.4\n"}, {}, {output: "Hermes Agent v0.20.4\n"}, {},
		},
	}
	stdout, _, err := executeSystemCLI(t, nil, commands, "setup", "hermes", "--source", checkout, "--json")
	if err != nil {
		t.Fatal(err)
	}
	if decodeSystemOutput(t, stdout)["plugin_path"] != target {
		t.Fatalf("setup output = %s", stdout)
	}
	if _, err := os.Stat(filepath.Join(target, "stale.py")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale plugin content survived replacement: %v", err)
	}
	if _, ok := findHermesPlugin(target); !ok {
		t.Fatal("installed Hermes plugin is incomplete")
	}
}

func TestSetupOpenClawBuildsAndPreservesToolAllowlist(t *testing.T) {
	checkout := filepath.Join(t.TempDir(), "checkout")
	plugin := writeOpenClawPlugin(t, checkout)
	t.Setenv("POWERCONTEXT_HOME", filepath.Join(t.TempDir(), "data"))
	commands := &scriptedSystemCommands{
		t: t, paths: map[string]string{"openclaw": "/usr/bin/openclaw", "pnpm": "/usr/bin/pnpm"},
		results: []systemCommandResult{
			{output: "OpenClaw 2026.8.1-beta.2\n"},
			{},
			{after: func(systemCommandCall) {
				bundle := filepath.Join(plugin, "dist", "index.js")
				if err := os.MkdirAll(filepath.Dir(bundle), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(bundle, []byte("export default {}\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}},
			{},
			{err: errors.New("missing gateway mode")},
			{},
			{output: `["custom_tool","powercontext_memory_get"]`},
			{},
			{},
		},
	}
	stdout, _, err := executeSystemCLI(t, nil, commands,
		"setup", "openclaw", "--source", checkout, "--server-url", "http://127.0.0.1:8765/", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if decodeSystemOutput(t, stdout)["server_url"] != "http://127.0.0.1:8765" {
		t.Fatalf("setup output = %s", stdout)
	}
	var allowlist []string
	call := commands.calls[7]
	if call.arguments[0] != "config" || call.arguments[1] != "set" || json.Unmarshal([]byte(call.arguments[3]), &allowlist) != nil {
		t.Fatalf("allowlist command = %v", call)
	}
	wantTools := []string{"custom_tool", "powercontext_memory_get"}
	for _, tool := range openClawTools {
		if !slices.Contains(wantTools, tool) {
			wantTools = append(wantTools, tool)
		}
	}
	if fmt.Sprint(allowlist) != fmt.Sprint(wantTools) {
		t.Fatalf("allowlist = %v, want %v", allowlist, wantTools)
	}
	if commands.calls[len(commands.calls)-1].String() != "/usr/bin/openclaw gateway restart" {
		t.Fatalf("last command = %v", commands.calls[len(commands.calls)-1])
	}
}

func TestDoctorOpenClawRequiresActiveSelectedMemoryPlugin(t *testing.T) {
	commands := &scriptedSystemCommands{
		t: t, paths: map[string]string{"openclaw": "/usr/bin/openclaw"},
		results: []systemCommandResult{{output: `{"plugins":[{"id":"memory-powercontext","enabled":true,"status":"loaded","memorySlotSelected":false}]}`}},
	}
	stdout, _, err := executeSystemCLI(t, nil, commands, "doctor", "openclaw", "--json")
	if err == nil {
		t.Fatal("doctor unexpectedly accepted an unselected memory plugin")
	}
	plugin := decodeSystemOutput(t, stdout)["checks"].(map[string]any)["plugin"].(map[string]any)
	if plugin["status"] != "failed" {
		t.Fatalf("plugin diagnostic = %#v", plugin)
	}
}

func writePiPackage(t *testing.T, root string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(piRelative))
	writeTestFile(t, filepath.Join(path, "package.json"), `{"name":"powercontext-pi"}`)
	writeTestFile(t, filepath.Join(path, "extensions", "powercontext.ts"), "export default () => {}\n")
	writeTestFile(t, filepath.Join(path, "skills", "project-context", "SKILL.md"), "project context\n")
	resolved, err := resolvePath(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func writeOpenCodePlugin(t *testing.T, root string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(openCodeRelative))
	writeTestFile(t, filepath.Join(path, "package.json"), `{"name":"powercontext-opencode"}`)
	writeTestFile(t, filepath.Join(path, "lib", "index.js"), "export default {}\n")
	writeTestFile(t, filepath.Join(path, "skills", "project-context", "SKILL.md"), "project context\n")
	resolved, err := resolvePath(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func writeHermesPlugin(t *testing.T, root string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(hermesRelative))
	writeTestFile(t, filepath.Join(path, "__init__.py"), "def register(): pass\n")
	writeTestFile(t, filepath.Join(path, "plugin.yaml"), "name: powercontext\n")
	resolved, err := resolvePath(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func writeOpenClawPlugin(t *testing.T, root string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(openClawRelative))
	writeTestFile(t, filepath.Join(path, "package.json"), `{"name":"@oceanbase/openclaw-memory-powercontext"}`)
	resolved, err := resolvePath(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

type environmentAwareCommands struct {
	*scriptedSystemCommands
	runEnv func(context.Context, map[string]string, string, ...string) ([]byte, error)
}

func (e *environmentAwareCommands) RunEnv(
	ctx context.Context,
	environment map[string]string,
	executable string,
	arguments ...string,
) ([]byte, error) {
	return e.runEnv(ctx, environment, executable, arguments...)
}
