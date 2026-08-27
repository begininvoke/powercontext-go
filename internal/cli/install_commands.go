package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	v1 "github.com/ob-labs/powercontext-go/api/v1"
	"github.com/ob-labs/powercontext-go/server"
	"github.com/spf13/cobra"
)

const (
	defaultMarketplaceSource = "ob-labs/powercontext-go"
	defaultMarketplaceRef    = "main"
	powerContextPlugin       = "powercontext"
	dshPluginName            = "powercontext-dsh"
	maximumCommandOutput     = 1 << 20
)

// systemCommandExecutor is the narrow process boundary used by setup and
// doctor. Keeping lookup and execution together lets tests prove exact argv
// contracts without teaching the rest of the CLI about subprocesses.
type systemCommandExecutor interface {
	LookPath(string) (string, error)
	Run(context.Context, string, ...string) ([]byte, error)
	RunEnv(context.Context, map[string]string, string, ...string) ([]byte, error)
	RunTimeout(context.Context, time.Duration, string, ...string) ([]byte, error)
}

type processCommandExecutor struct{}

func (processCommandExecutor) LookPath(name string) (string, error) { return exec.LookPath(name) }

func (processCommandExecutor) Run(ctx context.Context, executable string, arguments ...string) ([]byte, error) {
	return runProcessCommandWithEnvironment(ctx, nil, executable, arguments...)
}

func (processCommandExecutor) RunEnv(
	ctx context.Context,
	environment map[string]string,
	executable string,
	arguments ...string,
) ([]byte, error) {
	return runProcessCommandWithEnvironment(ctx, environment, executable, arguments...)
}

func (processCommandExecutor) RunTimeout(
	ctx context.Context,
	timeout time.Duration,
	executable string,
	arguments ...string,
) ([]byte, error) {
	return runProcessCommandWithOptions(ctx, timeout, nil, executable, arguments...)
}

type diagnostic struct {
	OK     bool              `json:"ok"`
	Status string            `json:"status"`
	Detail string            `json:"detail"`
	Checks map[string]string `json:"checks,omitempty"`
}

func newSetupCommand(state *commandState) *cobra.Command {
	command := &cobra.Command{Use: "setup", Short: "Install and configure PowerContext integrations."}
	command.AddCommand(
		newSetupCodexCommand(state),
		newSetupClaudeCodeCommand(state),
		newSetupDSHCommand(state),
		newSetupPiCommand(state),
		newSetupOpenCodeCommand(state),
		newSetupHermesCommand(state),
		newSetupOpenClawCommand(state),
	)
	return command
}

func newSetupCodexCommand(state *commandState) *cobra.Command {
	var source, ref string
	command := &cobra.Command{
		Use: "codex", Short: "Install the PowerContext Codex plugin and prepare local storage.", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if _, err := state.system.LookPath("codex"); err != nil {
				return errors.New("Codex CLI is not installed or is not on PATH")
			}
			dataDirectory, err := prepareDataDirectory()
			if err != nil {
				return err
			}
			marketplaceSource, local, err := normalizeMarketplaceSource(source)
			if err != nil {
				return err
			}
			arguments := []string{"plugin", "marketplace", "add", marketplaceSource}
			if !local {
				arguments = append(arguments, "--ref", ref)
			}
			marketplace, err := runJSONCommand(command.Context(), state.system, "codex", arguments...)
			if err != nil {
				return err
			}
			marketplaceName, err := requiredJSONText(marketplace, "marketplaceName")
			if err != nil {
				return err
			}
			plugin, err := runJSONCommand(
				command.Context(), state.system, "codex", "plugin", "add", powerContextPlugin+"@"+marketplaceName,
			)
			if err != nil {
				return err
			}
			name, err := requiredJSONText(plugin, "name")
			if err != nil {
				return err
			}
			version, err := requiredJSONText(plugin, "version")
			if err != nil {
				return err
			}
			checks := runCodexDiagnostics(command.Context(), state.system)
			if diagnosticsStatus(checks) != "ok" {
				if err := writeDiagnostics(state, checks); err != nil {
					return err
				}
				return alreadyReported(errors.New("Codex diagnostics did not pass"))
			}
			result := map[string]string{
				"marketplace": marketplaceName, "plugin": name, "plugin_version": version, "data_dir": dataDirectory,
			}
			if state.json {
				return writeJSON(state.stdout, result)
			}
			_, err = fmt.Fprintf(state.stdout,
				"PowerContext Codex setup complete.\nPlugin: %s@%s (%s)\nData directory: %s\nNext: run `powercontext server run`, start a new Codex session, then review `/hooks`.\n",
				name, marketplaceName, version, dataDirectory,
			)
			return err
		},
	}
	command.Flags().StringVar(&source, "source", defaultMarketplaceSource, "Codex marketplace Git source or local path.")
	command.Flags().StringVar(&ref, "ref", defaultMarketplaceRef, "Git ref used for a remote marketplace source.")
	return command
}

func newSetupDSHCommand(state *commandState) *cobra.Command {
	var source, ref string
	command := &cobra.Command{
		Use: "dsh", Short: "Install the PowerContext DeepSeek Harness plugin and prepare local storage.", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			executable, err := dshExecutable(state.system, runtime.GOOS)
			if err != nil {
				return errors.New("DeepSeek Harness CLI is not installed or is not on PATH")
			}
			dataDirectory, err := prepareDataDirectory()
			if err != nil {
				return err
			}
			pluginPath, err := resolveDSHPlugin(command.Context(), state.system, source, ref, dataDirectory)
			if err != nil {
				return err
			}
			bundle, err := os.Stat(filepath.Join(pluginPath, "lib", "index.js"))
			if err != nil || !bundle.Mode().IsRegular() {
				return errors.New("PowerContext DSH plugin is missing lib/index.js; build the plugin before setup")
			}
			if _, err := state.system.Run(command.Context(), executable, "plugin", "--profile", "web", "add", pluginPath); err != nil {
				return err
			}
			checks := runDSHDiagnostics(command.Context(), state.system)
			if diagnosticsStatus(checks) != "ok" {
				if err := writeDiagnostics(state, checks); err != nil {
					return err
				}
				return alreadyReported(errors.New("DeepSeek Harness diagnostics did not pass"))
			}
			result := map[string]string{"plugin": dshPluginName, "plugin_path": pluginPath, "data_dir": dataDirectory}
			if state.json {
				return writeJSON(state.stdout, result)
			}
			_, err = fmt.Fprintf(state.stdout,
				"PowerContext DeepSeek Harness setup complete.\nPlugin: %s (%s)\nData directory: %s\nNext: run `powercontext server run`, then start `dsh web`.\n",
				dshPluginName, pluginPath, dataDirectory,
			)
			return err
		},
	}
	command.Flags().StringVar(&source, "source", defaultMarketplaceSource, "PowerContext Git source or local checkout path.")
	command.Flags().StringVar(&ref, "ref", defaultMarketplaceRef, "Git ref used for a remote source.")
	return command
}

func newDoctorCommand(state *commandState) *cobra.Command {
	command := &cobra.Command{
		Use: "doctor", Short: "Check an installed PowerContext environment.", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			serverURL := state.serverURL
			if serverURL == "" && !command.Flags().Changed("server-url") {
				serverURL = defaultServerURL
			}
			checks := runServerDiagnostics(command.Context(), state.version.Version, serverURL, state.httpClient)
			if err := writeDiagnostics(state, checks); err != nil {
				return err
			}
			if diagnosticsStatus(checks) != "ok" {
				return alreadyReported(errors.New("one or more diagnostics did not pass"))
			}
			return nil
		},
	}
	command.AddCommand(
		&cobra.Command{
			Use: "codex", Short: "Check the optional Codex CLI and PowerContext plugin.", Args: cobra.NoArgs,
			RunE: func(command *cobra.Command, _ []string) error {
				checks := runCodexDiagnostics(command.Context(), state.system)
				if err := writeDiagnostics(state, checks); err != nil {
					return err
				}
				if diagnosticsStatus(checks) != "ok" {
					return alreadyReported(errors.New("Codex diagnostics did not pass"))
				}
				return nil
			},
		},
		newDoctorClaudeCodeCommand(state),
		&cobra.Command{
			Use: "dsh", Short: "Check the optional DeepSeek Harness CLI and PowerContext plugin.", Args: cobra.NoArgs,
			RunE: func(command *cobra.Command, _ []string) error {
				checks := runDSHDiagnostics(command.Context(), state.system)
				if err := writeDiagnostics(state, checks); err != nil {
					return err
				}
				if diagnosticsStatus(checks) != "ok" {
					return alreadyReported(errors.New("DeepSeek Harness diagnostics did not pass"))
				}
				return nil
			},
		},
		newDoctorPiCommand(state),
		newDoctorOpenCodeCommand(state),
		newDoctorHermesCommand(state),
		newDoctorOpenClawCommand(state),
	)
	return command
}

func runServerDiagnostics(ctx context.Context, version, serverURL string, baseClient *http.Client) map[string]diagnostic {
	if version == "" {
		version = "devel"
	}
	result := map[string]diagnostic{
		"package": {OK: true, Status: "ok", Detail: "powercontext " + version},
	}
	client := diagnosticHTTPClient(baseClient)
	live := requestDiagnostic(ctx, client, serverURL, "/health/live", false)
	result["server_liveness"] = live
	if !live.OK {
		result["server_readiness"] = diagnostic{Status: "skipped", Detail: "not checked because Server liveness failed"}
		return result
	}
	result["server_readiness"] = requestDiagnostic(ctx, client, serverURL, "/health/ready", true)
	return result
}

func diagnosticHTTPClient(base *http.Client) *http.Client {
	if base == nil {
		base = &http.Client{}
	}
	client := *base
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &client
}

func requestDiagnostic(ctx context.Context, client *http.Client, serverURL, path string, readiness bool) diagnostic {
	parsed, err := url.Parse(serverURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return diagnostic{Status: "failed", Detail: "Server URL must be an HTTP base URL without credentials or query data"}
	}
	requestCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, strings.TrimRight(parsed.String(), "/")+path, nil)
	if err != nil {
		return diagnostic{Status: "failed", Detail: "Server URL must be an HTTP base URL without credentials or query data"}
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "powercontext-doctor")
	response, err := client.Do(request)
	if err != nil {
		return diagnostic{Status: "failed", Detail: "cannot reach " + serverURL}
	}
	defer response.Body.Close()
	if !readiness && response.StatusCode != http.StatusOK {
		return diagnostic{Status: "failed", Detail: fmt.Sprintf("liveness returned HTTP %d", response.StatusCode)}
	}
	if readiness && response.StatusCode != http.StatusOK && response.StatusCode != http.StatusServiceUnavailable {
		return diagnostic{Status: "failed", Detail: fmt.Sprintf("readiness returned HTTP %d", response.StatusCode)}
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, maximumCommandOutput+1))
	if err != nil || len(payload) > maximumCommandOutput {
		if readiness {
			return diagnostic{Status: "failed", Detail: "readiness returned an invalid response"}
		}
		return diagnostic{Status: "failed", Detail: "liveness returned an invalid response"}
	}
	if !readiness {
		var value struct {
			Status *string `json:"status"`
		}
		if decodeDiagnosticJSON(payload, &value) != nil || value.Status == nil {
			return diagnostic{Status: "failed", Detail: "liveness returned an invalid response"}
		}
		status := "failed"
		ok := *value.Status == "ok"
		if ok {
			status = "ok"
		}
		return diagnostic{OK: ok, Status: status, Detail: serverURL + " status=" + *value.Status}
	}
	var value struct {
		Status *v1.ReadinessStatus `json:"status"`
		Checks *map[string]string  `json:"checks"`
	}
	if decodeDiagnosticJSON(payload, &value) != nil || value.Status == nil || value.Checks == nil || value.Status.Validate() != nil {
		return diagnostic{Status: "failed", Detail: "readiness returned an invalid response"}
	}
	status := "failed"
	ok := false
	if response.StatusCode == http.StatusOK && *value.Status == v1.ReadinessStatusReady {
		status, ok = "ok", true
	} else if response.StatusCode == http.StatusOK && *value.Status == v1.ReadinessStatusDegraded {
		status = "degraded"
	}
	checks := make(map[string]string, len(*value.Checks))
	for name, check := range *value.Checks {
		checks[name] = check
	}
	return diagnostic{OK: ok, Status: status, Detail: serverURL + " status=" + string(*value.Status), Checks: checks}
}

func decodeDiagnosticJSON(payload []byte, target any) error {
	if !utf8.Valid(payload) {
		return errors.New("response is not valid UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return errors.New("response contains a trailing JSON value")
		}
		return err
	}
	return nil
}

func runCodexDiagnostics(ctx context.Context, commands systemCommandExecutor) map[string]diagnostic {
	executable, err := commands.LookPath("codex")
	if err != nil {
		return map[string]diagnostic{
			"codex":  {Status: "failed", Detail: "Codex CLI is not installed or is not on PATH"},
			"plugin": {Status: "skipped", Detail: "not checked because Codex CLI is unavailable"},
		}
	}
	result, err := runJSONCommand(ctx, commands, "codex", "plugin", "list")
	if err != nil {
		return map[string]diagnostic{
			"codex":  {Status: "failed", Detail: err.Error()},
			"plugin": {Status: "skipped", Detail: "plugin list is unavailable"},
		}
	}
	var installed map[string]any
	if values, ok := result["installed"].([]any); ok {
		for _, item := range values {
			entry, ok := item.(map[string]any)
			if ok && entry["name"] == powerContextPlugin && entry["installed"] == true && entry["enabled"] == true {
				installed = entry
				break
			}
		}
	}
	plugin := diagnostic{Status: "failed", Detail: "PowerContext plugin is not installed"}
	if installed != nil {
		pluginID := "None"
		if value, ok := installed["pluginId"].(string); ok {
			pluginID = value
		}
		plugin = diagnostic{OK: true, Status: "ok", Detail: pluginID + " enabled=True"}
	}
	return map[string]diagnostic{"codex": {OK: true, Status: "ok", Detail: executable}, "plugin": plugin}
}

func runDSHDiagnostics(ctx context.Context, commands systemCommandExecutor) map[string]diagnostic {
	executable, err := dshExecutable(commands, runtime.GOOS)
	if err != nil {
		return map[string]diagnostic{
			"dsh":    {Status: "failed", Detail: "DeepSeek Harness CLI is not installed or is not on PATH"},
			"plugin": {Status: "skipped", Detail: "not checked because DeepSeek Harness CLI is unavailable"},
		}
	}
	output, err := commands.Run(ctx, executable, "--profile", "web", "--dump-config")
	if err != nil {
		return map[string]diagnostic{
			"dsh":    {Status: "failed", Detail: err.Error()},
			"plugin": {Status: "skipped", Detail: "plugin list is unavailable"},
		}
	}
	installed := false
	for _, line := range strings.Split(string(output), "\n") {
		value := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "-"))
		if value == "id: "+dshPluginName || value == `id: "`+dshPluginName+`"` || value == "id: '"+dshPluginName+"'" {
			installed = true
		}
	}
	plugin := diagnostic{Status: "failed", Detail: "PowerContext DSH plugin is not installed"}
	if installed {
		plugin = diagnostic{OK: true, Status: "ok", Detail: dshPluginName + " is installed"}
	}
	return map[string]diagnostic{"dsh": {OK: true, Status: "ok", Detail: executable}, "plugin": plugin}
}

func writeDiagnostics(state *commandState, values map[string]diagnostic) error {
	if state.json {
		return writeJSON(state.stdout, map[string]any{
			"ok": diagnosticsStatus(values) == "ok", "status": diagnosticsStatus(values), "checks": values,
		})
	}
	order := []string{
		"package", "server_liveness", "server_readiness", "codex", "claude_code", "dsh", "pi", "opencode",
		"hermes", "openclaw", "plugin", "skill", "activation", "version",
	}
	for _, name := range order {
		value, ok := values[name]
		if !ok {
			continue
		}
		if _, err := fmt.Fprintf(state.stdout, "%s: %s - %s\n", strings.ReplaceAll(name, "_", " "), value.Status, value.Detail); err != nil {
			return err
		}
		checks := make([]string, 0, len(value.Checks))
		for check := range value.Checks {
			checks = append(checks, check)
		}
		slices.Sort(checks)
		for _, check := range checks {
			status := value.Checks[check]
			if _, err := fmt.Fprintf(state.stdout, "  %s: %s\n", check, status); err != nil {
				return err
			}
		}
	}
	return nil
}

func diagnosticsStatus(values map[string]diagnostic) string {
	statuses := make(map[string]bool)
	for _, value := range values {
		statuses[value.Status] = true
	}
	for _, status := range []string{"failed", "degraded", "skipped"} {
		if statuses[status] {
			return status
		}
	}
	return "ok"
}

func prepareDataDirectory() (string, error) {
	directory, err := server.PowerContextDataDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", errors.New("cannot create PowerContext data directory")
	}
	return directory, nil
}

func normalizeMarketplaceSource(source string) (string, bool, error) {
	if strings.HasPrefix(source, ".") || strings.HasPrefix(source, "/") || strings.HasPrefix(source, "~") ||
		(len(source) >= 2 && source[1] == ':') {
		path := source
		if path == "~" || strings.HasPrefix(path, "~/") {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", false, err
			}
			if path == "~" {
				path = home
			} else {
				path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
			}
		}
		absolute, err := resolvePath(path)
		return absolute, true, err
	}
	if _, err := os.Stat(source); err == nil {
		absolute, err := resolvePath(source)
		return absolute, true, err
	}
	return source, false, nil
}

func dshExecutable(commands systemCommandExecutor, goos string) (string, error) {
	if goos == "windows" {
		if executable, err := commands.LookPath("dsh.cmd"); err == nil {
			return executable, nil
		}
	}
	return commands.LookPath("dsh")
}

func resolveDSHPlugin(
	ctx context.Context,
	commands systemCommandExecutor,
	source, ref, dataDirectory string,
) (string, error) {
	_, local, err := normalizeMarketplaceSource(source)
	if err != nil {
		return "", err
	}
	root := source
	if local {
		root, _, err = normalizeMarketplaceSource(source)
		if err != nil {
			return "", err
		}
	} else {
		if ref == "" || ref == "." || ref == ".." || strings.ContainsRune(ref, '\x00') {
			return "", errors.New("invalid DeepSeek Harness ref")
		}
		checkoutRoot := filepath.Join(dataDirectory, "checkouts", "dsh")
		target, err := checkoutTarget(checkoutRoot, ref)
		if err != nil {
			return "", errors.New("invalid DeepSeek Harness ref")
		}
		root = target
		if plugin, ok := findDSHPlugin(root); ok {
			return plugin, nil
		}
		if _, err := os.Lstat(root); err == nil {
			if err := os.RemoveAll(root); err != nil {
				return "", errors.New("cannot replace incomplete DSH checkout")
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", errors.New("cannot inspect DSH checkout")
		}
		if err := os.MkdirAll(filepath.Dir(root), 0o755); err != nil {
			return "", errors.New("cannot create DSH checkout directory")
		}
		cloneURL, err := githubCloneURL(source)
		if err != nil {
			return "", err
		}
		if _, err := commands.Run(ctx, "git", "clone", "--depth", "1", "--branch", ref, cloneURL, root); err != nil {
			return "", err
		}
	}
	if plugin, ok := findDSHPlugin(root); ok {
		return plugin, nil
	}
	return "", errors.New("PowerContext DSH plugin was not found under the selected source")
}

func findDSHPlugin(root string) (string, bool) {
	for _, candidate := range []string{root, filepath.Join(root, "integrations", "dsh", "plugins", "powercontext")} {
		payload, err := os.ReadFile(filepath.Join(candidate, "package.json"))
		if err != nil || len(payload) > maximumCommandOutput {
			continue
		}
		var manifest struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(payload, &manifest) == nil && manifest.Name == dshPluginName {
			return candidate, true
		}
	}
	return "", false
}

func checkoutTarget(root, ref string) (string, error) {
	if ref == "" || ref == "." || ref == ".." || strings.ContainsRune(ref, '\x00') || filepath.IsAbs(ref) {
		return "", errors.New("invalid ref")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	target, err := filepath.Abs(filepath.Join(root, ref))
	if err != nil {
		return "", err
	}
	rootResolved, err := resolvePath(root)
	if err != nil {
		return "", err
	}
	targetResolved, err := resolvePath(target)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(rootResolved, targetResolved)
	if err != nil || relative == "." || relative == ".." || filepath.IsAbs(relative) ||
		strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", errors.New("ref escapes checkout root")
	}
	return targetResolved, nil
}

// resolvePath mirrors Path.resolve(strict=False): symlinks in the existing
// prefix are resolved while a not-yet-created suffix remains lexical.
func resolvePath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	current := absolute
	var suffix []string
	for {
		resolved, resolveErr := filepath.EvalSymlinks(current)
		if resolveErr == nil {
			for index := len(suffix) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, suffix[index])
			}
			return filepath.Clean(resolved), nil
		}
		if !errors.Is(resolveErr, os.ErrNotExist) {
			return "", resolveErr
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", resolveErr
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
}

func githubCloneURL(source string) (string, error) {
	value, err := githubRepositoryCloneURL(source)
	if err != nil {
		return "", errors.New("invalid DeepSeek Harness source")
	}
	return value, nil
}

func runJSONCommand(
	ctx context.Context,
	commands systemCommandExecutor,
	executable string,
	arguments ...string,
) (map[string]any, error) {
	output, err := commands.Run(ctx, executable, append(arguments, "--json")...)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if json.Unmarshal(output, &result) != nil || result == nil {
		return nil, errors.New("external command returned invalid JSON")
	}
	return result, nil
}

func runProcessCommand(parent context.Context, executable string, arguments ...string) ([]byte, error) {
	return runProcessCommandWithEnvironment(parent, nil, executable, arguments...)
}

func runProcessCommandWithEnvironment(
	parent context.Context,
	environment map[string]string,
	executable string,
	arguments ...string,
) ([]byte, error) {
	return runProcessCommandWithOptions(parent, 120*time.Second, environment, executable, arguments...)
}

func runProcessCommandWithOptions(
	parent context.Context,
	timeout time.Duration,
	environment map[string]string,
	executable string,
	arguments ...string,
) ([]byte, error) {
	if timeout <= 0 {
		return nil, errors.New("external command timeout must be positive")
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	command := exec.CommandContext(ctx, executable, arguments...)
	if len(environment) > 0 {
		keys := make([]string, 0, len(environment))
		for key := range environment {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		command.Env = os.Environ()
		for _, key := range keys {
			if key == "" || strings.ContainsAny(key, "=\x00") || strings.ContainsRune(environment[key], '\x00') {
				return nil, errors.New("invalid external command environment")
			}
			command.Env = append(command.Env, key+"="+environment[key])
		}
	}
	var stdout, stderr boundedBuffer
	command.Stdout, command.Stderr = &stdout, &stderr
	runErr := command.Run()
	if stdout.exceeded || stderr.exceeded {
		return nil, errors.New("external command output exceeded 1 MiB")
	}
	if runErr != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		if detail == "" {
			if ctx.Err() != nil {
				detail = ctx.Err().Error()
			} else {
				detail = runErr.Error()
			}
		}
		return nil, fmt.Errorf("`%s` failed: %s", strings.Join(append([]string{executable}, arguments...), " "), detail)
	}
	return stdout.Bytes(), nil
}

type boundedBuffer struct {
	buffer   bytes.Buffer
	exceeded bool
}

func (b *boundedBuffer) Write(value []byte) (int, error) {
	original := len(value)
	remaining := maximumCommandOutput - b.buffer.Len()
	if remaining <= 0 {
		b.exceeded = true
		return original, nil
	}
	if len(value) > remaining {
		value = value[:remaining]
		b.exceeded = true
	}
	_, _ = b.buffer.Write(value)
	return original, nil
}

func (b *boundedBuffer) Bytes() []byte  { return b.buffer.Bytes() }
func (b *boundedBuffer) String() string { return b.buffer.String() }

func requiredJSONText(value map[string]any, name string) (string, error) {
	result, ok := value[name].(string)
	if !ok || result == "" {
		return "", fmt.Errorf("external command did not return %s", name)
	}
	return result, nil
}
