// Command release builds auditable, deterministic PowerContext release bundles.
package main

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"debug/buildinfo"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"slices"
	"sort"
	"strings"
	"time"

	"golang.org/x/mod/module"
)

const (
	assetsPath       = "build/native-assets.json"
	oraclePath       = "test/conformance/testdata/python-v0.0.2/manifest.json"
	modulePath       = "github.com/ob-labs/powercontext-go"
	maxLicenseBytes  = 2 << 20
	maxMetadataBytes = 16 << 20
)

var (
	semanticVersion  = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?$`)
	commitHash       = regexp.MustCompile(`^[0-9a-f]{40}$`)
	darwinORTLibrary = regexp.MustCompile(`^libonnxruntime(?:_[A-Za-z0-9_]+)?(?:\.[0-9]+)*\.dylib$`)
	linuxORTLibrary  = regexp.MustCompile(`^libonnxruntime(?:_[A-Za-z0-9_]+)?\.so(?:\.[0-9]+)*$`)

	//go:embed licenses/*.txt
	nativeLicenses embed.FS
)

type nativeAssets struct {
	SchemaVersion int `json:"schema_version"`
	SQLiteVec     struct {
		Version   string `json:"version"`
		SourceURL string `json:"source_url"`
		SHA256    string `json:"sha256"`
	} `json:"sqlite_vec"`
	Tokenizers struct {
		Version string                 `json:"version"`
		Assets  map[string]nativeAsset `json:"assets"`
	} `json:"tokenizers"`
	ONNXRuntime struct {
		Version string                 `json:"version"`
		Commit  string                 `json:"commit"`
		Assets  map[string]nativeAsset `json:"assets"`
	} `json:"onnxruntime"`
	Syft struct {
		Version string `json:"version"`
	} `json:"syft"`
}

type nativeAsset struct {
	Name            string `json:"name,omitempty"`
	SHA256          string `json:"sha256,omitempty"`
	BuildFromSource bool   `json:"build_from_source,omitempty"`
}

type oracleManifest struct {
	OracleCommit  string `json:"oracle_commit"`
	OpenAPISHA256 string `json:"openapi_sha256"`
}

type packageOptions struct {
	Binary         string
	ONNXRuntimeDir string
	Edition        string
	Version        string
	Commit         string
	BuildDate      string
	Output         string
	Repository     string
	Syft           string
}

type binaryFacts struct {
	Info        *buildinfo.BuildInfo
	GOOS        string
	GOARCH      string
	CGOEnabled  string
	BuildTags   []string
	BinaryHash  string
	BinaryBytes int64
}

type buildManifest struct {
	SchemaVersion int                 `json:"schema_version"`
	Product       string              `json:"product"`
	Edition       string              `json:"edition"`
	Version       string              `json:"version"`
	Commit        string              `json:"commit"`
	BuildDate     string              `json:"build_date"`
	GoVersion     string              `json:"go_version"`
	Target        string              `json:"target"`
	CGOEnabled    bool                `json:"cgo_enabled"`
	BuildTags     []string            `json:"build_tags"`
	Oracle        oracleManifest      `json:"python_oracle"`
	Binary        fileRecord          `json:"binary"`
	NativeAssets  []nativeAssetRecord `json:"native_assets"`
}

type fileRecord struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type nativeAssetRecord struct {
	Name         string `json:"name"`
	Version      string `json:"version"`
	Source       string `json:"source"`
	SourceDigest string `json:"source_digest"`
	PayloadHash  string `json:"payload_sha256"`
}

type dependencyManifest struct {
	SchemaVersion int                `json:"schema_version"`
	Modules       []dependencyRecord `json:"go_modules"`
	Native        []dependencyRecord `json:"native_dependencies"`
}

type dependencyRecord struct {
	Path        string          `json:"path"`
	Version     string          `json:"version"`
	Replacement string          `json:"replacement,omitempty"`
	Licenses    []licenseRecord `json:"licenses"`
}

type licenseRecord struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

type packageResult struct {
	Archive     string `json:"archive"`
	ArchiveHash string `json:"archive_sha256"`
	SBOM        string `json:"sbom"`
	SBOMHash    string `json:"sbom_sha256"`
}

func main() {
	if len(os.Args) < 2 {
		fatal(errors.New("usage: release <asset|package|metadata|checksum|verify> [flags]"))
	}
	var err error
	switch os.Args[1] {
	case "asset":
		err = runAsset(os.Args[2:], os.Stdout)
	case "package":
		err = runPackage(os.Args[2:], os.Stdout)
	case "metadata":
		err = runMetadata(os.Args[2:])
	case "checksum":
		err = runChecksum(os.Args[2:])
	case "verify":
		err = runVerify(os.Args[2:])
	default:
		err = fmt.Errorf("unknown release command %q", os.Args[1])
	}
	if err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	_, _ = fmt.Fprintln(os.Stderr, "release:", err)
	os.Exit(1)
}

func runAsset(arguments []string, output io.Writer) error {
	flags := flag.NewFlagSet("asset", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var repository, component, target, field string
	flags.StringVar(&repository, "repository", ".", "repository root")
	flags.StringVar(&component, "component", "", "sqlite-vec, tokenizers, onnxruntime, or syft")
	flags.StringVar(&target, "target", "", "GOOS-GOARCH target")
	flags.StringVar(&field, "field", "", "version, url, sha256, commit, name, or build-from-source")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	assets, err := readAssets(repository)
	if err != nil {
		return err
	}
	value, err := assetValue(assets, component, target, field)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(output, value)
	return err
}

func assetValue(assets nativeAssets, component, target, field string) (string, error) {
	switch component {
	case "sqlite-vec":
		switch field {
		case "version":
			return assets.SQLiteVec.Version, nil
		case "url":
			return assets.SQLiteVec.SourceURL, nil
		case "sha256":
			return assets.SQLiteVec.SHA256, nil
		}
	case "syft":
		if field == "version" {
			return assets.Syft.Version, nil
		}
	case "tokenizers":
		if field == "version" {
			return assets.Tokenizers.Version, nil
		}
		asset, ok := assets.Tokenizers.Assets[target]
		if !ok {
			return "", fmt.Errorf("unsupported tokenizers target %q", target)
		}
		return releaseAssetValue(
			asset, field,
			fmt.Sprintf("https://github.com/daulet/tokenizers/releases/download/v%s/%s", assets.Tokenizers.Version, asset.Name),
		)
	case "onnxruntime":
		if field == "version" {
			return assets.ONNXRuntime.Version, nil
		}
		if field == "commit" {
			return assets.ONNXRuntime.Commit, nil
		}
		asset, ok := assets.ONNXRuntime.Assets[target]
		if !ok {
			return "", fmt.Errorf("unsupported ONNX Runtime target %q", target)
		}
		return releaseAssetValue(
			asset, field,
			fmt.Sprintf("https://github.com/microsoft/onnxruntime/releases/download/v%s/%s", assets.ONNXRuntime.Version, asset.Name),
		)
	}
	return "", fmt.Errorf("unsupported asset field %q for %q", field, component)
}

func releaseAssetValue(asset nativeAsset, field, url string) (string, error) {
	switch field {
	case "name":
		return asset.Name, nil
	case "sha256":
		return asset.SHA256, nil
	case "url":
		if asset.Name == "" {
			return "", errors.New("asset is built from source and has no binary URL")
		}
		return url, nil
	case "build-from-source":
		return fmt.Sprintf("%t", asset.BuildFromSource), nil
	default:
		return "", fmt.Errorf("unsupported asset field %q", field)
	}
}

func runPackage(arguments []string, output io.Writer) error {
	flags := flag.NewFlagSet("package", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	options := bindPackageFlags(flags)
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	result, err := packageRelease(*options)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

func bindPackageFlags(flags *flag.FlagSet) *packageOptions {
	options := new(packageOptions)
	flags.StringVar(&options.Binary, "binary", "", "built powercontext binary")
	flags.StringVar(&options.ONNXRuntimeDir, "onnxruntime-dir", "", "ONNX Runtime library directory")
	flags.StringVar(&options.Edition, "edition", "standard", "standard or full")
	flags.StringVar(&options.Version, "version", "", "release version")
	flags.StringVar(&options.Commit, "commit", "", "40-character source commit")
	flags.StringVar(&options.BuildDate, "build-date", "", "RFC3339 UTC build date")
	flags.StringVar(&options.Output, "output", "dist", "release output directory")
	flags.StringVar(&options.Repository, "repository", ".", "repository root")
	flags.StringVar(&options.Syft, "syft", "syft", "pinned Syft executable")
	return options
}

func packageRelease(options packageOptions) (packageResult, error) {
	buildTime, err := validatePackageOptions(options)
	if err != nil {
		return packageResult{}, err
	}
	repository, err := filepath.Abs(options.Repository)
	if err != nil {
		return packageResult{}, err
	}
	assets, err := readAssets(repository)
	if err != nil {
		return packageResult{}, err
	}
	oracle, err := readOracle(repository)
	if err != nil {
		return packageResult{}, err
	}
	facts, err := inspectBinary(options.Binary, options.Edition, options.Version)
	if err != nil {
		return packageResult{}, err
	}
	target := facts.GOOS + "-" + facts.GOARCH
	if _, ok := assets.Tokenizers.Assets[target]; !ok {
		return packageResult{}, fmt.Errorf("unsupported release target %q", target)
	}
	if _, ok := assets.ONNXRuntime.Assets[target]; !ok {
		return packageResult{}, fmt.Errorf("unsupported release target %q", target)
	}

	outputDirectory, err := filepath.Abs(options.Output)
	if err != nil {
		return packageResult{}, err
	}
	if err := os.MkdirAll(outputDirectory, 0o755); err != nil {
		return packageResult{}, err
	}
	temporary, err := os.MkdirTemp(outputDirectory, ".powercontext-release-")
	if err != nil {
		return packageResult{}, err
	}
	defer os.RemoveAll(temporary)

	version := strings.TrimPrefix(options.Version, "v")
	artifactName := "powercontext-" + version + "-" + target
	if options.Edition == "full" {
		artifactName = "powercontext-full-" + version + "-" + target
	}
	archivePath := filepath.Join(outputDirectory, artifactName+".tar.gz")
	sbomPath := filepath.Join(outputDirectory, artifactName+".spdx.json")
	for _, releasePath := range []string{archivePath, sbomPath} {
		if _, err := os.Stat(releasePath); err == nil {
			return packageResult{}, fmt.Errorf("refusing to replace existing release file %q", filepath.Base(releasePath))
		} else if !errors.Is(err, os.ErrNotExist) {
			return packageResult{}, err
		}
	}
	root := filepath.Join(temporary, artifactName)
	if err := stageRelease(repository, root, options, facts); err != nil {
		return packageResult{}, err
	}

	onnxPayload := ""
	if options.Edition == "full" {
		onnxPayload = filepath.Join(root, "lib", "onnxruntime")
	}
	nativeRecords, err := describeNativeAssets(onnxPayload, options.Edition, facts, assets)
	if err != nil {
		return packageResult{}, err
	}
	manifest := newBuildManifest(options, buildTime, facts, oracle, nativeRecords)
	if err := writeJSON(filepath.Join(root, "BUILD-INFO.json"), manifest); err != nil {
		return packageResult{}, err
	}
	dependencies, notices, err := collectLicenses(options.Binary, repository, options.Edition, assets)
	if err != nil {
		return packageResult{}, err
	}
	if err := writeJSON(filepath.Join(root, "DEPENDENCIES.json"), dependencies); err != nil {
		return packageResult{}, err
	}
	if err := os.WriteFile(filepath.Join(root, "THIRD-PARTY-LICENSES.txt"), notices, 0o644); err != nil {
		return packageResult{}, err
	}

	temporarySBOM := filepath.Join(temporary, artifactName+".spdx.json")
	if err := generateSBOM(options.Syft, root, temporarySBOM, assets.Syft.Version, buildTime); err != nil {
		return packageResult{}, err
	}
	if err := copyRegularFile(temporarySBOM, filepath.Join(root, "SBOM.spdx.json"), 0o644); err != nil {
		return packageResult{}, err
	}
	if err := writeTreeChecksums(root); err != nil {
		return packageResult{}, err
	}

	temporaryArchive := filepath.Join(temporary, artifactName+".tar.gz")
	if err := archiveTree(root, temporaryArchive, buildTime); err != nil {
		return packageResult{}, err
	}
	archiveHash, _, err := hashFile(temporaryArchive)
	if err != nil {
		return packageResult{}, err
	}
	sbomHash, _, err := hashFile(temporarySBOM)
	if err != nil {
		return packageResult{}, err
	}
	if err := os.Rename(temporaryArchive, archivePath); err != nil {
		return packageResult{}, err
	}
	if err := os.Rename(temporarySBOM, sbomPath); err != nil {
		_ = os.Remove(archivePath)
		return packageResult{}, err
	}
	return packageResult{
		Archive: filepath.Base(archivePath), ArchiveHash: archiveHash,
		SBOM: filepath.Base(sbomPath), SBOMHash: sbomHash,
	}, nil
}

func runMetadata(arguments []string) error {
	flags := flag.NewFlagSet("metadata", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	options := bindPackageFlags(flags)
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	return writeImageMetadata(*options)
}

func writeImageMetadata(options packageOptions) error {
	buildTime, err := validatePackageOptions(options)
	if err != nil {
		return err
	}
	repository, err := filepath.Abs(options.Repository)
	if err != nil {
		return err
	}
	assets, err := readAssets(repository)
	if err != nil {
		return err
	}
	oracle, err := readOracle(repository)
	if err != nil {
		return err
	}
	facts, err := inspectBinary(options.Binary, options.Edition, options.Version)
	if err != nil {
		return err
	}
	target := facts.GOOS + "-" + facts.GOARCH
	if _, ok := assets.Tokenizers.Assets[target]; !ok {
		return fmt.Errorf("unsupported release target %q", target)
	}
	if _, ok := assets.ONNXRuntime.Assets[target]; !ok {
		return fmt.Errorf("unsupported release target %q", target)
	}
	output, err := filepath.Abs(options.Output)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(output, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(output)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return errors.New("metadata output directory must be empty")
	}
	nativeRecords, err := describeNativeAssets(options.ONNXRuntimeDir, options.Edition, facts, assets)
	if err != nil {
		return err
	}
	manifest := newBuildManifest(options, buildTime, facts, oracle, nativeRecords)
	if err := writeJSON(filepath.Join(output, "BUILD-INFO.json"), manifest); err != nil {
		return err
	}
	dependencies, notices, err := collectLicenses(options.Binary, repository, options.Edition, assets)
	if err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(output, "DEPENDENCIES.json"), dependencies); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(output, "THIRD-PARTY-LICENSES.txt"), notices, 0o644); err != nil {
		return err
	}
	return writeTreeChecksums(output)
}

func newBuildManifest(
	options packageOptions,
	buildTime time.Time,
	facts binaryFacts,
	oracle oracleManifest,
	nativeRecords []nativeAssetRecord,
) buildManifest {
	return buildManifest{
		SchemaVersion: 1, Product: "PowerContext", Edition: options.Edition,
		Version: strings.TrimPrefix(options.Version, "v"), Commit: options.Commit,
		BuildDate: buildTime.Format(time.RFC3339), GoVersion: facts.Info.GoVersion,
		Target: facts.GOOS + "-" + facts.GOARCH, CGOEnabled: facts.CGOEnabled == "1",
		BuildTags: slices.Clone(facts.BuildTags), Oracle: oracle,
		Binary:       fileRecord{Path: "bin/powercontext", SHA256: facts.BinaryHash, Size: facts.BinaryBytes},
		NativeAssets: nativeRecords,
	}
}

func validatePackageOptions(options packageOptions) (time.Time, error) {
	if options.Binary == "" || options.Version == "" || options.Commit == "" || options.BuildDate == "" {
		return time.Time{}, errors.New("binary, version, commit, and build-date are required")
	}
	if options.Edition != "standard" && options.Edition != "full" {
		return time.Time{}, errors.New("edition must be standard or full")
	}
	if options.Edition == "full" && options.ONNXRuntimeDir == "" {
		return time.Time{}, errors.New("full edition requires onnxruntime-dir")
	}
	if !semanticVersion.MatchString(options.Version) {
		return time.Time{}, errors.New("version must use semantic versioning")
	}
	if !commitHash.MatchString(options.Commit) {
		return time.Time{}, errors.New("commit must be a lowercase 40-character SHA-1")
	}
	buildTime, err := time.Parse(time.RFC3339, options.BuildDate)
	if err != nil || options.BuildDate != buildTime.UTC().Format(time.RFC3339) {
		return time.Time{}, errors.New("build-date must be a canonical UTC RFC3339 timestamp")
	}
	return buildTime, nil
}

func inspectBinary(path, edition, version string) (binaryFacts, error) {
	info, err := buildinfo.ReadFile(path)
	if err != nil {
		return binaryFacts{}, fmt.Errorf("read binary build information: %w", err)
	}
	if info.Main.Path != modulePath {
		return binaryFacts{}, fmt.Errorf("binary module is %q, want %q", info.Main.Path, modulePath)
	}
	settings := make(map[string]string, len(info.Settings))
	for _, setting := range info.Settings {
		settings[setting.Key] = setting.Value
	}
	tags := splitBuildTags(settings["-tags"])
	required := []string{"sqlite_fts5"}
	if edition == "full" {
		required = append(required, "local_embeddings", "ORT")
	}
	for _, tag := range required {
		if !slices.Contains(tags, tag) {
			return binaryFacts{}, fmt.Errorf("binary is missing required build tag %q", tag)
		}
	}
	if settings["CGO_ENABLED"] != "1" {
		return binaryFacts{}, errors.New("release binary must be built with CGO_ENABLED=1")
	}
	if settings["GOOS"] != runtime.GOOS || settings["GOARCH"] != runtime.GOARCH {
		return binaryFacts{}, errors.New("release packaging must run on the binary's target platform")
	}
	versionOutput, err := exec.Command(path, "--version").CombinedOutput()
	if err != nil || strings.TrimSpace(string(versionOutput)) != strings.TrimPrefix(version, "v") {
		return binaryFacts{}, errors.New("binary version metadata does not match release version")
	}
	hash, size, err := hashFile(path)
	if err != nil {
		return binaryFacts{}, err
	}
	return binaryFacts{
		Info: info, GOOS: settings["GOOS"], GOARCH: settings["GOARCH"],
		CGOEnabled: settings["CGO_ENABLED"], BuildTags: tags,
		BinaryHash: hash, BinaryBytes: size,
	}, nil
}

func splitBuildTags(value string) []string {
	parts := strings.FieldsFunc(value, func(character rune) bool { return character == ',' || character == ' ' })
	sort.Strings(parts)
	return slices.Compact(parts)
}

func stageRelease(repository, root string, options packageOptions, facts binaryFacts) error {
	for _, directory := range []string{
		filepath.Join(root, "bin"), filepath.Join(root, "lib"),
		filepath.Join(root, "openapi"), filepath.Join(root, "docs"),
	} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return err
		}
	}
	if err := copyRegularFile(options.Binary, filepath.Join(root, "bin", "powercontext"), 0o755); err != nil {
		return err
	}
	for _, pair := range [][2]string{
		{filepath.Join(repository, "LICENSE"), filepath.Join(root, "LICENSE")},
		{filepath.Join(repository, "README.md"), filepath.Join(root, "README.md")},
		{filepath.Join(repository, ".env.example"), filepath.Join(root, ".env.example")},
		{filepath.Join(repository, "openapi", "powercontext.yaml"), filepath.Join(root, "openapi", "powercontext.yaml")},
		{filepath.Join(repository, "docs", "release", "INSTALL.md"), filepath.Join(root, "docs", "INSTALL.md")},
	} {
		if err := copyRegularFile(pair[0], pair[1], 0o644); err != nil {
			return err
		}
	}
	if err := stageIntegrations(repository, root); err != nil {
		return err
	}
	if options.Edition == "full" {
		if err := copyONNXRuntime(
			options.ONNXRuntimeDir,
			filepath.Join(root, "lib", "onnxruntime"),
			facts.GOOS,
		); err != nil {
			return err
		}
	}
	return nil
}

func stageIntegrations(repository, root string) error {
	// Claude Code discovers a local marketplace from this repository-level
	// manifest; copying only integrations/ leaves the plugin files present but
	// makes `setup claude-code --source <archive>` unusable.
	if err := copyTree(
		filepath.Join(repository, ".claude-plugin"),
		filepath.Join(root, ".claude-plugin"),
	); err != nil {
		return err
	}
	if err := copyTree(
		filepath.Join(repository, "integrations"),
		filepath.Join(root, "integrations"),
	); err != nil {
		return err
	}
	// dist is normally workspace output and is filtered by copyTree. OpenClaw's
	// tracked bundle is its executable adapter, so stage that one runtime tree
	// explicitly after copying the source tree.
	return copyTree(
		filepath.Join(repository, "integrations", "openclaw", "plugins", "memory-powercontext", "dist"),
		filepath.Join(root, "integrations", "openclaw", "plugins", "memory-powercontext", "dist"),
	)
}

func describeNativeAssets(
	onnxDirectory, edition string,
	facts binaryFacts,
	assets nativeAssets,
) ([]nativeAssetRecord, error) {
	records := []nativeAssetRecord{{
		Name: "sqlite-vec (statically embedded)", Version: assets.SQLiteVec.Version, Source: assets.SQLiteVec.SourceURL,
		SourceDigest: assets.SQLiteVec.SHA256, PayloadHash: facts.BinaryHash,
	}}
	if edition != "full" {
		return records, nil
	}
	target := facts.GOOS + "-" + facts.GOARCH
	tokenizers := assets.Tokenizers.Assets[target]
	records = append(records, nativeAssetRecord{
		Name: "Daulet Tokenizers static library", Version: assets.Tokenizers.Version,
		Source: tokenizers.Name, SourceDigest: tokenizers.SHA256, PayloadHash: facts.BinaryHash,
	})
	onnx := assets.ONNXRuntime.Assets[target]
	source, sourceDigest := onnx.Name, onnx.SHA256
	if onnx.BuildFromSource {
		source = "https://github.com/microsoft/onnxruntime.git"
		sourceDigest = assets.ONNXRuntime.Commit
	}
	onnxHash, err := hashTree(onnxDirectory)
	if err != nil {
		return nil, err
	}
	records = append(records, nativeAssetRecord{
		Name: "ONNX Runtime", Version: assets.ONNXRuntime.Version,
		Source: source, SourceDigest: sourceDigest, PayloadHash: onnxHash,
	})
	return records, nil
}

func collectLicenses(
	binaryPath, repository, edition string,
	assets nativeAssets,
) (dependencyManifest, []byte, error) {
	info, err := buildinfo.ReadFile(binaryPath)
	if err != nil {
		return dependencyManifest{}, nil, err
	}
	moduleCache, err := goModuleCache(repository)
	if err != nil {
		return dependencyManifest{}, nil, err
	}

	dependencies := dependencyManifest{SchemaVersion: 1}
	var notices strings.Builder
	for _, dependency := range info.Deps {
		directory, replacement, err := moduleDirectory(dependency, moduleCache, repository)
		if err != nil {
			return dependencyManifest{}, nil, err
		}
		licenseFiles, err := findLicenseFiles(directory)
		if err != nil {
			return dependencyManifest{}, nil, fmt.Errorf("%s %s: %w", dependency.Path, dependency.Version, err)
		}
		record := dependencyRecord{Path: dependency.Path, Version: dependency.Version, Replacement: replacement}
		writeNoticeHeader(&notices, dependency.Path, dependency.Version)
		for _, licensePath := range licenseFiles {
			contents, err := readBoundedFile(licensePath, maxLicenseBytes)
			if err != nil {
				return dependencyManifest{}, nil, err
			}
			hash := sha256.Sum256(contents)
			record.Licenses = append(record.Licenses, licenseRecord{
				Name: filepath.Base(licensePath), SHA256: hex.EncodeToString(hash[:]),
			})
			writeLicense(&notices, filepath.Base(licensePath), contents)
		}
		dependencies.Modules = append(dependencies.Modules, record)
	}
	sort.Slice(dependencies.Modules, func(left, right int) bool {
		return dependencies.Modules[left].Path < dependencies.Modules[right].Path
	})

	nativeNames := []struct {
		path    string
		version string
		file    string
	}{
		{path: "github.com/asg017/sqlite-vec", version: assets.SQLiteVec.Version, file: "licenses/sqlite-vec.txt"},
	}
	if edition == "full" {
		nativeNames = append(nativeNames,
			struct{ path, version, file string }{"github.com/daulet/tokenizers/native", assets.Tokenizers.Version, "licenses/tokenizers.txt"},
			struct{ path, version, file string }{"github.com/microsoft/onnxruntime/native", assets.ONNXRuntime.Version, "licenses/onnxruntime.txt"},
		)
	}
	for _, native := range nativeNames {
		contents, err := fs.ReadFile(nativeLicenses, native.file)
		if err != nil {
			return dependencyManifest{}, nil, err
		}
		hash := sha256.Sum256(contents)
		dependencies.Native = append(dependencies.Native, dependencyRecord{
			Path: native.path, Version: native.version,
			Licenses: []licenseRecord{{Name: filepath.Base(native.file), SHA256: hex.EncodeToString(hash[:])}},
		})
		writeNoticeHeader(&notices, native.path, native.version)
		writeLicense(&notices, filepath.Base(native.file), contents)
	}
	return dependencies, []byte(notices.String()), nil
}

func goModuleCache(repository string) (string, error) {
	command := exec.Command("go", "env", "GOMODCACHE")
	command.Dir = repository
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("locate Go module cache: %w", err)
	}
	directory := strings.TrimSpace(string(output))
	if directory == "" {
		return "", errors.New("Go module cache is empty")
	}
	return directory, nil
}

func moduleDirectory(dependency *debug.Module, moduleCache, repository string) (string, string, error) {
	effective := dependency
	replacement := ""
	if dependency.Replace != nil {
		effective = dependency.Replace
		replacement = effective.Path
		if effective.Version != "" {
			replacement += "@" + effective.Version
		}
	}
	var directory string
	if effective.Version == "" {
		directory = effective.Path
		if !filepath.IsAbs(directory) {
			directory = filepath.Join(repository, directory)
		}
	} else {
		escapedPath, err := module.EscapePath(effective.Path)
		if err != nil {
			return "", "", err
		}
		escapedVersion, err := module.EscapeVersion(effective.Version)
		if err != nil {
			return "", "", err
		}
		directory = filepath.Join(moduleCache, escapedPath+"@"+escapedVersion)
	}
	info, err := os.Stat(directory)
	if err != nil || !info.IsDir() {
		return "", "", fmt.Errorf("module source for %s %s is unavailable in the module cache", dependency.Path, dependency.Version)
	}
	return directory, replacement, nil
}

func findLicenseFiles(directory string) ([]string, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !licenseFileName(entry.Name()) {
			continue
		}
		paths = append(paths, filepath.Join(directory, entry.Name()))
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return nil, errors.New("no top-level license or notice file")
	}
	return paths, nil
}

func licenseFileName(name string) bool {
	lower := strings.ToLower(name)
	for _, prefix := range []string{"license", "copying", "notice", "patents"} {
		if lower == prefix || strings.HasPrefix(lower, prefix+".") || strings.HasPrefix(lower, prefix+"-") {
			return true
		}
	}
	return false
}

func writeNoticeHeader(writer *strings.Builder, name, version string) {
	writer.WriteString("================================================================================\n")
	writer.WriteString(name)
	writer.WriteByte(' ')
	writer.WriteString(version)
	writer.WriteByte('\n')
	writer.WriteString("================================================================================\n")
}

func writeLicense(writer *strings.Builder, name string, contents []byte) {
	writer.WriteString("-- ")
	writer.WriteString(name)
	writer.WriteString(" --\n\n")
	writer.Write(contents)
	if len(contents) == 0 || contents[len(contents)-1] != '\n' {
		writer.WriteByte('\n')
	}
	writer.WriteByte('\n')
}

func generateSBOM(syft, root, output, syftVersion string, created time.Time) error {
	sourceName := filepath.Base(root)
	command := exec.Command(
		syft,
		"scan", "dir:"+root,
		"--source-name", sourceName,
		"--output", "spdx-json="+output,
	)
	command.Env = append(os.Environ(), "SYFT_CHECK_FOR_APP_UPDATE=false")
	combined, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("generate SPDX SBOM: %w: %s", err, strings.TrimSpace(string(combined)))
	}
	contents, err := readBoundedFile(output, maxMetadataBytes)
	if err != nil {
		return err
	}
	var document map[string]any
	if err := json.Unmarshal(contents, &document); err != nil || document["spdxVersion"] == nil {
		return errors.New("Syft did not produce a valid SPDX JSON document")
	}
	creationInfo, ok := document["creationInfo"].(map[string]any)
	if !ok {
		return errors.New("Syft SPDX document has no creationInfo object")
	}
	document["name"] = sourceName
	document["documentNamespace"] = "https://github.com/ob-labs/powercontext-go/sbom/" + sourceName
	creationInfo["created"] = created.UTC().Format(time.RFC3339)
	creationInfo["creators"] = []string{
		"Organization: Anchore, Inc",
		"Tool: syft-" + syftVersion,
	}
	return writeJSON(output, document)
}

func runChecksum(arguments []string) error {
	flags := flag.NewFlagSet("checksum", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var output string
	flags.StringVar(&output, "output", "", "checksum manifest path")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if output == "" || flags.NArg() == 0 {
		return errors.New("checksum requires output and at least one file")
	}
	return writeFileChecksums(output, flags.Args())
}

func runVerify(arguments []string) error {
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var expected string
	flags.StringVar(&expected, "sha256", "", "expected lowercase SHA-256")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if !validSHA256(expected) || flags.NArg() != 1 {
		return errors.New("verify requires one file and a lowercase SHA-256")
	}
	actual, _, err := hashFile(flags.Arg(0))
	if err != nil {
		return err
	}
	if actual != expected {
		return fmt.Errorf("SHA-256 mismatch for %q", filepath.Base(flags.Arg(0)))
	}
	return nil
}

func writeFileChecksums(output string, paths []string) error {
	type checksum struct{ name, hash string }
	seen := make(map[string]struct{}, len(paths))
	values := make([]checksum, 0, len(paths))
	for _, path := range paths {
		name := filepath.Base(path)
		if _, exists := seen[name]; exists {
			return fmt.Errorf("duplicate checksum basename %q", name)
		}
		seen[name] = struct{}{}
		hash, _, err := hashFile(path)
		if err != nil {
			return err
		}
		values = append(values, checksum{name: name, hash: hash})
	}
	sort.Slice(values, func(left, right int) bool { return values[left].name < values[right].name })
	var contents strings.Builder
	for _, value := range values {
		fmt.Fprintf(&contents, "%s  %s\n", value.hash, value.name)
	}
	return writeNewFile(output, []byte(contents.String()), 0o644)
}

func writeTreeChecksums(root string) error {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root || entry.IsDir() || filepath.Base(path) == "SHA256SUMS" {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				return err
			}
			paths = append(paths, resolved+"\x00"+path)
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("unsupported release file %q", path)
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return err
	}
	sort.Slice(paths, func(left, right int) bool {
		return checksumDisplayPath(root, paths[left]) < checksumDisplayPath(root, paths[right])
	})
	var contents strings.Builder
	for _, value := range paths {
		hashPath, displayPath := value, value
		if resolved, original, ok := strings.Cut(value, "\x00"); ok {
			hashPath, displayPath = resolved, original
		}
		hash, _, err := hashFile(hashPath)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, displayPath)
		if err != nil {
			return err
		}
		fmt.Fprintf(&contents, "%s  %s\n", hash, filepath.ToSlash(relative))
	}
	return os.WriteFile(filepath.Join(root, "SHA256SUMS"), []byte(contents.String()), 0o644)
}

func checksumDisplayPath(root, value string) string {
	if _, original, ok := strings.Cut(value, "\x00"); ok {
		value = original
	}
	relative, _ := filepath.Rel(root, value)
	return filepath.ToSlash(relative)
}

func archiveTree(root, output string, timestamp time.Time) (returnErr error) {
	file, err := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := file.Close(); returnErr == nil {
			returnErr = closeErr
		}
		if returnErr != nil {
			_ = os.Remove(output)
		}
	}()
	gzipWriter, err := gzip.NewWriterLevel(file, gzip.BestCompression)
	if err != nil {
		return err
	}
	gzipWriter.Header.ModTime = timestamp
	gzipWriter.Header.OS = 255
	tarWriter := tar.NewWriter(gzipWriter)

	parent := filepath.Dir(root)
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		link := ""
		if info.Mode()&os.ModeSymlink != 0 {
			link, err = os.Readlink(path)
			if err != nil {
				return err
			}
		}
		header, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(parent, path)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(relative)
		if info.IsDir() {
			header.Name += "/"
			header.Mode = 0o755
		} else if info.Mode().IsRegular() {
			header.Mode = normalizedFileMode(info.Mode())
		}
		header.ModTime = timestamp
		header.AccessTime = time.Time{}
		header.ChangeTime = time.Time{}
		header.Uid, header.Gid = 0, 0
		header.Uname, header.Gname = "root", "root"
		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tarWriter, input)
		closeErr := input.Close()
		return errors.Join(copyErr, closeErr)
	})
	if err != nil {
		return err
	}
	if err := tarWriter.Close(); err != nil {
		return err
	}
	return gzipWriter.Close()
}

func normalizedFileMode(mode os.FileMode) int64 {
	if mode&0o111 != 0 {
		return 0o755
	}
	return 0o644
}

func copyTree(source, destination string) error {
	root, err := filepath.Abs(source)
	if err != nil {
		return err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("release tree %q is not a directory", filepath.Base(source))
	}
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return os.MkdirAll(destination, 0o755)
		}
		if skipReleaseEntry(entry) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				return err
			}
			inside, err := filepath.Rel(root, resolved)
			if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
				return fmt.Errorf("release symlink %q escapes its source tree", relative)
			}
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			if filepath.IsAbs(link) {
				return fmt.Errorf("release symlink %q is absolute", relative)
			}
			return os.Symlink(link, target)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported release entry %q", relative)
		}
		return copyRegularFile(path, target, os.FileMode(normalizedFileMode(info.Mode())))
	})
}

// copyONNXRuntime keeps release bundles relocatable and limited to the native
// libraries needed at runtime. Upstream archives also contain CMake metadata,
// pkg-config files, and (on macOS) large dSYM trees; those are build inputs, not
// runtime dependencies.
func copyONNXRuntime(source, destination, goos string) error {
	root, err := filepath.Abs(source)
	if err != nil {
		return err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("ONNX Runtime library source %q is not a directory", filepath.Base(source))
	}
	if goos != "darwin" && goos != "linux" {
		return fmt.Errorf("unsupported ONNX Runtime release target %q", goos)
	}
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return err
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	copiedRegular := false
	for _, entry := range entries {
		if !isONNXRuntimeLibrary(entry.Name(), goos) {
			continue
		}
		sourcePath := filepath.Join(root, entry.Name())
		targetPath := filepath.Join(destination, entry.Name())
		entryInfo, err := os.Lstat(sourcePath)
		if err != nil {
			return err
		}
		if entryInfo.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(sourcePath)
			if err != nil {
				return err
			}
			if filepath.IsAbs(link) {
				return fmt.Errorf("ONNX Runtime symlink %q is absolute", entry.Name())
			}
			resolved, err := filepath.EvalSymlinks(sourcePath)
			if err != nil {
				return err
			}
			inside, err := filepath.Rel(root, resolved)
			if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
				return fmt.Errorf("ONNX Runtime symlink %q escapes its source tree", entry.Name())
			}
			if !isONNXRuntimeLibrary(filepath.Base(resolved), goos) {
				return fmt.Errorf("ONNX Runtime symlink %q has an invalid target", entry.Name())
			}
			if err := os.Symlink(link, targetPath); err != nil {
				return err
			}
			continue
		}
		if !entryInfo.Mode().IsRegular() {
			return fmt.Errorf("unsupported ONNX Runtime entry %q", entry.Name())
		}
		if err := copyRegularFile(sourcePath, targetPath, os.FileMode(normalizedFileMode(entryInfo.Mode()))); err != nil {
			return err
		}
		copiedRegular = true
	}
	if !copiedRegular {
		return fmt.Errorf("ONNX Runtime library directory %q contains no runtime library for %s", filepath.Base(source), goos)
	}
	return nil
}

func isONNXRuntimeLibrary(name, goos string) bool {
	switch goos {
	case "darwin":
		return darwinORTLibrary.MatchString(name)
	case "linux":
		return linuxORTLibrary.MatchString(name)
	default:
		return false
	}
}

func skipReleaseEntry(entry fs.DirEntry) bool {
	name := entry.Name()
	if entry.IsDir() && slices.Contains([]string{
		".git", ".mypy_cache", ".pytest_cache", ".ruff_cache", ".venv",
		"__pycache__", "coverage", "dist", "node_modules",
	}, name) {
		return true
	}
	return name == ".DS_Store" || strings.HasSuffix(name, ".pyc") || strings.HasSuffix(name, ".pyo")
}

func copyRegularFile(source, destination string, mode os.FileMode) (returnErr error) {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("release input %q is not a regular file", filepath.Base(source))
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := output.Close(); returnErr == nil {
			returnErr = closeErr
		}
	}()
	_, err = io.Copy(output, input)
	return err
}

func writeJSON(path string, value any) error {
	var contents strings.Builder
	encoder := json.NewEncoder(&contents)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(contents.String()), 0o644)
}

func writeNewFile(path string, contents []byte, mode os.FileMode) (returnErr error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := file.Close(); returnErr == nil {
			returnErr = closeErr
		}
	}()
	_, err = file.Write(contents)
	return err
}

func hashFile(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hash.Sum(nil)), size, nil
}

func hashTree(root string) (string, error) {
	hash := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root || entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		_, _ = io.WriteString(hash, filepath.ToSlash(relative))
		_, _ = hash.Write([]byte{0})
		if entry.Type()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			_, _ = io.WriteString(hash, "link:"+link)
		} else if entry.Type().IsRegular() {
			fileHash, _, err := hashFile(path)
			if err != nil {
				return err
			}
			_, _ = io.WriteString(hash, fileHash)
		} else {
			return fmt.Errorf("unsupported native runtime entry %q", relative)
		}
		_, _ = hash.Write([]byte{0})
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func readAssets(repository string) (nativeAssets, error) {
	var assets nativeAssets
	contents, err := readBoundedFile(filepath.Join(repository, assetsPath), maxMetadataBytes)
	if err != nil {
		return assets, err
	}
	decoder := json.NewDecoder(strings.NewReader(string(contents)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&assets); err != nil {
		return assets, err
	}
	if err := validateAssets(assets); err != nil {
		return assets, err
	}
	return assets, nil
}

func validateAssets(assets nativeAssets) error {
	if assets.SchemaVersion != 2 || assets.SQLiteVec.Version == "" || assets.Tokenizers.Version == "" ||
		assets.ONNXRuntime.Version == "" || assets.Syft.Version == "" || !commitHash.MatchString(assets.ONNXRuntime.Commit) {
		return errors.New("native asset manifest is incomplete")
	}
	for _, target := range []string{"darwin-amd64", "darwin-arm64", "linux-amd64", "linux-arm64"} {
		tokenizers, tokenizersOK := assets.Tokenizers.Assets[target]
		onnx, onnxOK := assets.ONNXRuntime.Assets[target]
		if !tokenizersOK || tokenizers.Name == "" || !validSHA256(tokenizers.SHA256) || !onnxOK {
			return fmt.Errorf("native asset manifest is incomplete for %s", target)
		}
		if onnx.BuildFromSource {
			if onnx.Name != "" || onnx.SHA256 != "" {
				return fmt.Errorf("source-built ONNX Runtime target %s also declares a binary asset", target)
			}
		} else if onnx.Name == "" || !validSHA256(onnx.SHA256) {
			return fmt.Errorf("ONNX Runtime asset is incomplete for %s", target)
		}
	}
	if !validSHA256(assets.SQLiteVec.SHA256) {
		return errors.New("sqlite-vec source digest is invalid")
	}
	return nil
}

func validSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == strings.ToLower(value)
}

func readOracle(repository string) (oracleManifest, error) {
	var oracle oracleManifest
	contents, err := readBoundedFile(filepath.Join(repository, oraclePath), maxMetadataBytes)
	if err != nil {
		return oracle, err
	}
	if err := json.Unmarshal(contents, &oracle); err != nil {
		return oracle, err
	}
	if !commitHash.MatchString(oracle.OracleCommit) || !validSHA256(oracle.OpenAPISHA256) {
		return oracle, errors.New("Python Oracle manifest is incomplete")
	}
	return oracle, nil
}

func readBoundedFile(path string, maximum int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader := bufio.NewReader(io.LimitReader(file, maximum+1))
	contents, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if int64(len(contents)) > maximum {
		return nil, fmt.Errorf("metadata file %q exceeds %d bytes", filepath.Base(path), maximum)
	}
	return contents, nil
}
