package main

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNativeAssetManifestSupportsReleaseMatrix(t *testing.T) {
	repository := filepath.Clean(filepath.Join("..", ".."))
	assets, err := readAssets(repository)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"darwin-amd64", "darwin-arm64", "linux-amd64", "linux-arm64"} {
		if _, err := assetValue(assets, "tokenizers", target, "url"); err != nil {
			t.Errorf("tokenizers %s: %v", target, err)
		}
		onnx := assets.ONNXRuntime.Assets[target]
		if onnx.BuildFromSource {
			if target != "darwin-amd64" {
				t.Errorf("unexpected source-built target %s", target)
			}
			continue
		}
		if _, err := assetValue(assets, "onnxruntime", target, "url"); err != nil {
			t.Errorf("ONNX Runtime %s: %v", target, err)
		}
	}
}

func TestDockerNativeAssetDefaultsMatchManifest(t *testing.T) {
	repository := filepath.Clean(filepath.Join("..", ".."))
	assets, err := readAssets(repository)
	if err != nil {
		t.Fatal(err)
	}
	dockerfile, err := os.ReadFile(filepath.Join(repository, "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]string{
		"VEC1_VERSION":             assets.Vec1.Version,
		"VEC1_SHA256":              assets.Vec1.SHA256,
		"TOKENIZERS_VERSION":       assets.Tokenizers.Version,
		"TOKENIZERS_AMD64_SHA256":  assets.Tokenizers.Assets["linux-amd64"].SHA256,
		"TOKENIZERS_ARM64_SHA256":  assets.Tokenizers.Assets["linux-arm64"].SHA256,
		"ONNXRUNTIME_VERSION":      assets.ONNXRuntime.Version,
		"ONNXRUNTIME_AMD64_SHA256": assets.ONNXRuntime.Assets["linux-amd64"].SHA256,
		"ONNXRUNTIME_ARM64_SHA256": assets.ONNXRuntime.Assets["linux-arm64"].SHA256,
	}
	for name, value := range expected {
		if !strings.Contains(string(dockerfile), "ARG "+name+"="+value) {
			t.Errorf("Dockerfile does not pin %s from native-assets.json", name)
		}
	}
}

func TestCopyTreeRejectsEscapingSymlink(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source")
	destination := filepath.Join(t.TempDir(), "destination")
	if err := os.Mkdir(source, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(source, "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	err := copyTree(source, destination)
	if err == nil || !strings.Contains(err.Error(), "escapes its source tree") {
		t.Fatalf("copyTree error = %v", err)
	}
}

func TestArchiveTreeIsDeterministic(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "powercontext-1.2.3-test")
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bin", "powercontext"), []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "LICENSE"), []byte("license\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	timestamp := time.Date(2026, time.August, 17, 0, 0, 0, 0, time.UTC)
	first := filepath.Join(parent, "first.tar.gz")
	second := filepath.Join(parent, "second.tar.gz")
	if err := archiveTree(root, first, timestamp); err != nil {
		t.Fatal(err)
	}
	if err := archiveTree(root, second, timestamp); err != nil {
		t.Fatal(err)
	}
	firstHash, _, err := hashFile(first)
	if err != nil {
		t.Fatal(err)
	}
	secondHash, _, err := hashFile(second)
	if err != nil {
		t.Fatal(err)
	}
	if firstHash != secondHash {
		t.Fatalf("archives differ: %s != %s", firstHash, secondHash)
	}

	file, err := os.Open(first)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if !header.ModTime.Equal(timestamp) || header.Uid != 0 || header.Gid != 0 {
			t.Fatalf("non-deterministic header %#v", header)
		}
	}
}

func TestTreeChecksumsUsePortableRelativePaths(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "value.txt"), []byte("value"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeTreeChecksums(root); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(filepath.Join(root, "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "cd42404d52ad55ccfa9aca4adc828aa5800ad9d385a0671fbcbf724118320619  nested/value.txt\n" {
		t.Fatalf("checksums = %q", contents)
	}
}

func TestLicenseDiscoveryIsStrictAndDeterministic(t *testing.T) {
	directory := t.TempDir()
	if _, err := findLicenseFiles(directory); err == nil {
		t.Fatal("missing license accepted")
	}
	for _, name := range []string{"NOTICE", "LICENSE.md", "source.go"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	paths, err := findLicenseFiles(directory)
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{filepath.Base(paths[0]), filepath.Base(paths[1])}; strings.Join(got, ",") != "LICENSE.md,NOTICE" {
		t.Fatalf("licenses = %v", got)
	}
}

func TestVerifyRejectsWrongDigestWithoutLeakingPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credential-name")
	if err := os.WriteFile(path, []byte("value"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := runVerify([]string{"-sha256", strings.Repeat("0", 64), path})
	if err == nil || strings.Contains(err.Error(), filepath.Dir(path)) || !strings.Contains(err.Error(), "credential-name") {
		t.Fatalf("verify error = %v", err)
	}
}
