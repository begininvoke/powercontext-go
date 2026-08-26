package skill

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

const (
	MaxExternalFiles         = 256
	MaxExternalPackageBytes  = 4 * 1024 * 1024
	MaxExternalManifestBytes = 128 * 1024
	MaxExternalHostIDLength  = 128
	MaxExternalLocatorLength = 2_000
)

var (
	lowerHexFingerprint = regexp.MustCompile(`^[0-9a-f]{64}$`)
	rootIDPattern       = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
)

type InstallationScope string

const (
	UserScope    InstallationScope = "user"
	ProjectScope InstallationScope = "project"
	PluginScope  InstallationScope = "plugin"
)

type AgentKind string

const (
	CodexAgent      AgentKind = "codex"
	ClaudeCodeAgent AgentKind = "claude_code"
)

func validAgentKind(value string) bool {
	return value == string(CodexAgent) || value == string(ClaudeCodeAgent)
}

type ResolutionStatus string

const (
	Available   ResolutionStatus = "available"
	Unavailable ResolutionStatus = "unavailable"
)

type ExternalNotFoundError struct{ ExternalSkillID string }

func (e *ExternalNotFoundError) Error() string { return "external Skill registration was not found" }

type ExternalRegistryUnavailableError struct{}

func (*ExternalRegistryUnavailableError) Error() string {
	return "external Skill Registry is not configured"
}

type ExternalSnapshotUnavailableError struct{ ExternalSkillID string }

func (e *ExternalSnapshotUnavailableError) Error() string {
	return "external Skill snapshot is unavailable"
}

type Registration struct {
	externalSkillID   string
	provider          string
	agentKind         string
	hostID            string
	installationScope InstallationScope
	locator           string
	fingerprint       string
	name              string
	description       string
}

func NewRegistration(
	externalSkillID, provider, agentKind, hostID string,
	installationScope InstallationScope,
	locator, fingerprint, name, description string,
) (Registration, error) {
	for _, value := range []struct {
		label   string
		text    string
		maximum int
	}{
		{"external_skill_id", externalSkillID, artifactIDLimit},
		{"provider", provider, 128}, {"agent_kind", agentKind, 128},
		{"host_id", hostID, MaxExternalHostIDLength},
		{"locator", locator, MaxExternalLocatorLength},
		{"name", name, MaxNameLength}, {"description", description, MaxDescriptionLength},
	} {
		if err := externalText(value.label, value.text, value.maximum); err != nil {
			return Registration{}, err
		}
	}
	if installationScope != UserScope && installationScope != ProjectScope && installationScope != PluginScope {
		return Registration{}, fmt.Errorf("invalid external Skill installation scope %q", installationScope)
	}
	if !validAgentKind(provider) {
		return Registration{}, fmt.Errorf("invalid external Skill provider %q", provider)
	}
	if !validAgentKind(agentKind) {
		return Registration{}, fmt.Errorf("invalid external Skill agent kind %q", agentKind)
	}
	if !lowerHexFingerprint.MatchString(fingerprint) {
		return Registration{}, fmt.Errorf("external Skill fingerprint must be 64 lowercase hexadecimal characters")
	}
	return Registration{
		externalSkillID: externalSkillID, provider: provider, agentKind: agentKind, hostID: hostID,
		installationScope: installationScope, locator: locator, fingerprint: fingerprint,
		name: name, description: description,
	}, nil
}

const artifactIDLimit = 128

func (r Registration) ExternalSkillID() string              { return r.externalSkillID }
func (r Registration) Provider() string                     { return r.provider }
func (r Registration) AgentKind() string                    { return r.agentKind }
func (r Registration) HostID() string                       { return r.hostID }
func (r Registration) InstallationScope() InstallationScope { return r.installationScope }
func (r Registration) Locator() string                      { return r.locator }
func (r Registration) Fingerprint() string                  { return r.fingerprint }
func (r Registration) Name() string                         { return r.name }
func (r Registration) Description() string                  { return r.description }

type Resolution struct {
	Registration Registration
	Status       ResolutionStatus
	Entrypoint   string
}

type ProviderScan struct {
	registrations []Registration
	skipped       int
}

func NewProviderScan(registrations []Registration, skipped int) (ProviderScan, error) {
	if skipped < 0 {
		return ProviderScan{}, fmt.Errorf("external Skill skipped count must not be negative")
	}
	return ProviderScan{registrations: slices.Clone(registrations), skipped: skipped}, nil
}

func (s ProviderScan) Registrations() []Registration { return slices.Clone(s.registrations) }
func (s ProviderScan) Skipped() int                  { return s.skipped }

type Snapshot struct {
	registration Registration
	manifest     string
}

func NewSnapshot(registration Registration, manifest string) (Snapshot, error) {
	if manifest == "" || len([]byte(manifest)) > MaxExternalManifestBytes {
		return Snapshot{}, fmt.Errorf("external Skill manifest must contain 1..%d UTF-8 bytes", MaxExternalManifestBytes)
	}
	return Snapshot{registration: registration, manifest: manifest}, nil
}

func (s Snapshot) Registration() Registration { return s.registration }
func (s Snapshot) Manifest() string           { return s.manifest }

type ExternalProvider interface {
	Name() string
	AgentKind() string
	HostID() string
	ProviderNames() []string
	Scan(context.Context) (ProviderScan, error)
	Resolve(context.Context, Registration) (Resolution, error)
}

type AgentSkillTarget struct {
	targetID            string
	agentKind           AgentKind
	installationScope   InstallationScope
	path                string
	allowManagedPublish bool
}

func NewAgentSkillTarget(
	targetID string,
	agentKind AgentKind,
	scope InstallationScope,
	path string,
	allowManagedPublish bool,
) (AgentSkillTarget, error) {
	if len(targetID) < 1 || len(targetID) > 64 || !rootIDPattern.MatchString(targetID) {
		return AgentSkillTarget{}, fmt.Errorf("Agent Skill target ID is invalid")
	}
	if !validAgentKind(string(agentKind)) {
		return AgentSkillTarget{}, fmt.Errorf("invalid Agent Skill kind %q", agentKind)
	}
	if scope != UserScope && scope != ProjectScope && scope != PluginScope {
		return AgentSkillTarget{}, fmt.Errorf("invalid external Skill installation scope %q", scope)
	}
	resolved, err := resolveLoose(path)
	if err != nil {
		return AgentSkillTarget{}, err
	}
	return AgentSkillTarget{
		targetID: targetID, agentKind: agentKind, installationScope: scope,
		path: resolved, allowManagedPublish: allowManagedPublish,
	}, nil
}

func (t AgentSkillTarget) ID() string                           { return t.targetID }
func (t AgentSkillTarget) AgentKind() AgentKind                 { return t.agentKind }
func (t AgentSkillTarget) InstallationScope() InstallationScope { return t.installationScope }
func (t AgentSkillTarget) Path() string                         { return t.path }
func (t AgentSkillTarget) AllowManagedPublish() bool            { return t.allowManagedPublish }

type CodexRoot struct{ target AgentSkillTarget }

func NewCodexRoot(rootID string, scope InstallationScope, path string) (CodexRoot, error) {
	return NewCodexRootWithPublish(rootID, scope, path, false)
}

func NewCodexRootWithPublish(
	rootID string,
	scope InstallationScope,
	path string,
	allowManagedPublish bool,
) (CodexRoot, error) {
	target, err := NewAgentSkillTarget(rootID, CodexAgent, scope, path, allowManagedPublish)
	if err != nil {
		return CodexRoot{}, err
	}
	return CodexRoot{target: target}, nil
}

func (r CodexRoot) ID() string                           { return r.target.ID() }
func (r CodexRoot) InstallationScope() InstallationScope { return r.target.InstallationScope() }
func (r CodexRoot) Path() string                         { return r.target.Path() }
func (r CodexRoot) AllowManagedPublish() bool            { return r.target.AllowManagedPublish() }
func (r CodexRoot) AgentTarget() AgentSkillTarget        { return r.target }

type AgentSkillProvider struct {
	hostID        string
	targets       []AgentSkillTarget
	providerNames []string
}

func NewAgentSkillProvider(hostID string, targets []AgentSkillTarget) (*AgentSkillProvider, error) {
	return newAgentSkillProvider(hostID, targets, []string{string(CodexAgent), string(ClaudeCodeAgent)})
}

func newAgentSkillProvider(
	hostID string,
	targets []AgentSkillTarget,
	providerNames []string,
) (*AgentSkillProvider, error) {
	if err := externalText("host_id", hostID, MaxExternalHostIDLength); err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		if _, exists := seen[target.targetID]; exists {
			return nil, fmt.Errorf("Agent Skill target IDs must be unique")
		}
		seen[target.targetID] = struct{}{}
	}
	return &AgentSkillProvider{
		hostID:        hostID,
		targets:       slices.Clone(targets),
		providerNames: slices.Clone(providerNames),
	}, nil
}

func (*AgentSkillProvider) Name() string              { return "agent-targets" }
func (*AgentSkillProvider) AgentKind() string         { return "multi" }
func (p *AgentSkillProvider) HostID() string          { return p.hostID }
func (p *AgentSkillProvider) ProviderNames() []string { return slices.Clone(p.providerNames) }

func (p *AgentSkillProvider) Scan(ctx context.Context) (ProviderScan, error) {
	var registrations []Registration
	skipped := 0
	for _, target := range p.targets {
		if err := ctx.Err(); err != nil {
			return ProviderScan{}, err
		}
		entries, err := os.ReadDir(target.path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return ProviderScan{}, err
		}
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return ProviderScan{}, err
			}
			info, err := entry.Info()
			if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				continue
			}
			registration, err := p.registration(target, filepath.Join(target.path, entry.Name()))
			if err != nil {
				skipped++
				continue
			}
			registrations = append(registrations, registration)
		}
	}
	return NewProviderScan(registrations, skipped)
}

func (p *AgentSkillProvider) Resolve(ctx context.Context, registration Registration) (Resolution, error) {
	if err := ctx.Err(); err != nil {
		return Resolution{}, err
	}
	if !slices.Contains(p.providerNames, registration.provider) ||
		registration.agentKind != registration.provider || registration.hostID != p.hostID {
		return unavailable(registration), nil
	}
	target, ok := p.targetFor(registration)
	if !ok {
		return unavailable(registration), nil
	}
	resolved, err := filepath.EvalSymlinks(registration.locator)
	if err != nil {
		return unavailable(registration), nil
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil || filepath.Dir(resolved) != target.path {
		return unavailable(registration), nil
	}
	info, err := os.Lstat(resolved)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return unavailable(registration), nil
	}
	current, err := p.registration(target, resolved)
	if err != nil || current.externalSkillID != registration.externalSkillID || current.fingerprint != registration.fingerprint {
		return unavailable(registration), nil
	}
	return Resolution{Registration: registration, Status: Available, Entrypoint: filepath.Join(resolved, "SKILL.md")}, nil
}

func (p *AgentSkillProvider) targetFor(registration Registration) (AgentSkillTarget, bool) {
	prefix := registration.agentKind + ":" + string(registration.installationScope) + ":"
	if !strings.HasPrefix(registration.externalSkillID, prefix) {
		return AgentSkillTarget{}, false
	}
	remainder := strings.TrimPrefix(registration.externalSkillID, prefix)
	targetID := strings.SplitN(remainder, "/", 2)[0]
	for _, target := range p.targets {
		if target.targetID == targetID && string(target.agentKind) == registration.agentKind &&
			target.installationScope == registration.installationScope {
			return target, true
		}
	}
	return AgentSkillTarget{}, false
}

func (p *AgentSkillProvider) registration(target AgentSkillTarget, packagePath string) (Registration, error) {
	if filepath.Dir(packagePath) != target.path {
		return Registration{}, fmt.Errorf("Agent Skill package must be an immediate child of its configured target")
	}
	name, description, err := skillMetadata(
		filepath.Join(packagePath, "SKILL.md"), filepath.Base(packagePath), target.agentKind,
	)
	if err != nil {
		return Registration{}, err
	}
	fingerprint, err := packageFingerprint(packagePath)
	if err != nil {
		return Registration{}, err
	}
	externalID := fmt.Sprintf(
		"%s:%s:%s/%s", target.agentKind, target.installationScope, target.targetID, filepath.Base(packagePath),
	)
	return NewRegistration(
		externalID, string(target.agentKind), string(target.agentKind), p.hostID, target.installationScope,
		packagePath, fingerprint, name, description,
	)
}

type CodexProvider struct{ provider *AgentSkillProvider }

func NewCodexProvider(hostID string, roots []CodexRoot) (*CodexProvider, error) {
	targets := make([]AgentSkillTarget, len(roots))
	for index, root := range roots {
		targets[index] = root.AgentTarget()
	}
	provider, err := newAgentSkillProvider(hostID, targets, []string{string(CodexAgent)})
	if err != nil {
		return nil, err
	}
	return &CodexProvider{provider: provider}, nil
}

func (*CodexProvider) Name() string              { return "codex" }
func (*CodexProvider) AgentKind() string         { return "codex" }
func (p *CodexProvider) HostID() string          { return p.provider.HostID() }
func (p *CodexProvider) ProviderNames() []string { return []string{string(CodexAgent)} }
func (p *CodexProvider) Scan(ctx context.Context) (ProviderScan, error) {
	return p.provider.Scan(ctx)
}
func (p *CodexProvider) Resolve(ctx context.Context, registration Registration) (Resolution, error) {
	return p.provider.Resolve(ctx, registration)
}

func CaptureSnapshot(ctx context.Context, provider ExternalProvider, registration Registration) (Snapshot, error) {
	resolution, err := provider.Resolve(ctx, registration)
	if err != nil {
		return Snapshot{}, err
	}
	if resolution.Status != Available || resolution.Entrypoint == "" {
		return Snapshot{}, &ExternalSnapshotUnavailableError{ExternalSkillID: registration.externalSkillID}
	}
	manifest, err := readManifest(resolution.Entrypoint)
	if err != nil {
		return Snapshot{}, &ExternalSnapshotUnavailableError{ExternalSkillID: registration.externalSkillID}
	}
	confirmed, err := provider.Resolve(ctx, registration)
	if err != nil {
		return Snapshot{}, err
	}
	if confirmed.Status != Available || confirmed.Entrypoint != resolution.Entrypoint {
		return Snapshot{}, &ExternalSnapshotUnavailableError{ExternalSkillID: registration.externalSkillID}
	}
	return NewSnapshot(registration, manifest)
}

func unavailable(registration Registration) Resolution {
	return Resolution{Registration: registration, Status: Unavailable}
}

func skillMetadata(manifest, packageName string, agentKind AgentKind) (string, string, error) {
	info, err := os.Lstat(manifest)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", "", fmt.Errorf("Agent Skill manifest must be a regular non-symlink file")
	}
	contents, err := readBounded(manifest, MaxExternalManifestBytes)
	if err != nil {
		return "", "", err
	}
	return parseSkillMetadata(contents, packageName, agentKind)
}

func parseSkillMetadata(contents []byte, packageName string, agentKind AgentKind) (string, string, error) {
	if !utf8.Valid(contents) {
		return "", "", fmt.Errorf("Agent Skill manifest must be UTF-8")
	}
	if !utf8.ValidString(packageName) {
		return "", "", fmt.Errorf("Agent Skill package name must be UTF-8")
	}
	scanner := bufio.NewScanner(strings.NewReader(string(contents)))
	scanner.Buffer(make([]byte, 0, 4*1024), MaxExternalManifestBytes+1)
	if !scanner.Scan() || trimPythonWhitespace(scanner.Text()) != "---" {
		return "", "", fmt.Errorf("Agent Skill manifest is missing frontmatter")
	}
	metadata := make(map[string]string)
	terminated := false
	for scanner.Scan() {
		line := scanner.Text()
		if trimPythonWhitespace(line) == "---" {
			terminated = true
			break
		}
		field, raw, found := strings.Cut(line, ":")
		if found && (field == "name" || field == "description") {
			value, err := frontmatterScalar(trimPythonWhitespace(raw))
			if err != nil {
				return "", "", err
			}
			metadata[field] = value
		}
	}
	if err := scanner.Err(); err != nil {
		return "", "", err
	}
	if !terminated {
		return "", "", fmt.Errorf("Agent Skill frontmatter is not terminated")
	}
	name, hasName := metadata["name"]
	description, hasDescription := metadata["description"]
	if agentKind == ClaudeCodeAgent && !hasName {
		name, hasName = packageName, true
	}
	if !hasName || !hasDescription {
		required := "name and description"
		if agentKind == ClaudeCodeAgent {
			required = "description"
		}
		return "", "", fmt.Errorf("%s Skill frontmatter requires %s", agentKind, required)
	}
	return name, description, nil
}

func frontmatterScalar(value string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("Agent Skill frontmatter values must not be empty")
	}
	if value[0] == '"' {
		var parsed string
		if err := json.Unmarshal([]byte(value), &parsed); err != nil {
			return "", fmt.Errorf("Agent Skill frontmatter contains an invalid scalar")
		}
		return parsed, nil
	}
	if value[0] == '\'' {
		runes := []rune(value)
		if len(runes) == 1 {
			return "", nil
		}
		return string(runes[1 : len(runes)-1]), nil
	}
	return value, nil
}

type packageFile struct {
	path     string
	relative string
}

func packageFingerprint(packagePath string) (string, error) {
	var files []packageFile
	err := filepath.WalkDir(packagePath, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == packagePath {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("Codex Skill packages containing symlinks are not supported")
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		relative, err := filepath.Rel(packagePath, path)
		if err != nil {
			return err
		}
		files = append(files, packageFile{path: path, relative: filepath.ToSlash(relative)})
		return nil
	})
	if err != nil {
		return "", err
	}
	slices.SortFunc(files, func(left, right packageFile) int { return strings.Compare(left.relative, right.relative) })
	if len(files) < 1 || len(files) > MaxExternalFiles {
		return "", fmt.Errorf("Codex Skill package has an unsupported file count")
	}
	digest := sha256.New()
	total := 0
	var size [8]byte
	for _, file := range files {
		content, err := readBounded(file.path, MaxExternalPackageBytes-total)
		if err != nil {
			return "", err
		}
		total += len(content)
		if total > MaxExternalPackageBytes {
			return "", fmt.Errorf("Codex Skill package exceeds the supported size")
		}
		relative := []byte(file.relative)
		binary.BigEndian.PutUint32(size[:4], uint32(len(relative)))
		_, _ = digest.Write(size[:4])
		_, _ = digest.Write(relative)
		binary.BigEndian.PutUint64(size[:], uint64(len(content)))
		_, _ = digest.Write(size[:])
		_, _ = digest.Write(content)
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func readManifest(path string) (string, error) {
	infoBefore, err := os.Lstat(path)
	if err != nil || infoBefore.Mode()&os.ModeSymlink != 0 || !infoBefore.Mode().IsRegular() {
		return "", fmt.Errorf("external Skill manifest is not a regular file")
	}
	content, err := readBounded(path, MaxExternalManifestBytes)
	if err != nil {
		return "", err
	}
	infoAfter, err := os.Lstat(path)
	if err != nil || !os.SameFile(infoBefore, infoAfter) || infoAfter.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("external Skill manifest changed while reading")
	}
	if !utf8.Valid(content) {
		return "", fmt.Errorf("external Skill manifest must be UTF-8")
	}
	return string(content), nil
}

func readBounded(path string, maximum int) ([]byte, error) {
	if maximum < 0 {
		return nil, fmt.Errorf("file bound exhausted")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, int64(maximum)+1))
	if err != nil {
		return nil, err
	}
	if len(content) > maximum {
		return nil, fmt.Errorf("external Skill file exceeds the supported size")
	}
	return content, nil
}

func externalText(label, value string, maximum int) error {
	trimmed := trimPythonWhitespace(value)
	if !utf8.ValidString(value) || trimmed == "" || value != trimmed {
		return fmt.Errorf("external Skill %s must be non-empty and trimmed", label)
	}
	if utf8.RuneCountInString(value) > maximum {
		return fmt.Errorf("external Skill %s must not exceed %d characters", label, maximum)
	}
	return nil
}

func resolveLoose(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	absolute = filepath.Clean(absolute)
	resolved, err := filepath.EvalSymlinks(absolute)
	if err == nil {
		return filepath.Clean(resolved), nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	// filepath.EvalSymlinks requires the full path to exist, while Python's
	// Path.resolve(strict=False) resolves every existing ancestor. Preserve
	// that behavior so a future target below /var and the same live path below
	// /private/var cannot acquire two different identities on macOS.
	ancestor := absolute
	var suffix []string
	for {
		if _, statErr := os.Lstat(ancestor); statErr == nil {
			resolvedAncestor, resolveErr := filepath.EvalSymlinks(ancestor)
			if resolveErr != nil {
				return "", resolveErr
			}
			for index := len(suffix) - 1; index >= 0; index-- {
				resolvedAncestor = filepath.Join(resolvedAncestor, suffix[index])
			}
			return filepath.Clean(resolvedAncestor), nil
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return "", statErr
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return absolute, nil
		}
		suffix = append(suffix, filepath.Base(ancestor))
		ancestor = parent
	}
}
