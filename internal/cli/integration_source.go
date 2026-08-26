package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func validateRemoteRef(ref string) error {
	if strings.TrimSpace(ref) == "" || ref == "." || ref == ".." || strings.ContainsRune(ref, '\x00') {
		return errors.New("invalid Git ref")
	}
	return nil
}

func githubRepositoryCloneURL(source string) (string, error) {
	value := strings.TrimSpace(source)
	if strings.HasPrefix(value, "https://github.com/") || strings.HasPrefix(value, "git@github.com:") || strings.HasPrefix(value, "ssh://git@github.com/") {
		if strings.Contains(value, "@github.com") && !strings.HasPrefix(value, "git@github.com:") && !strings.HasPrefix(value, "ssh://git@github.com/") {
			return "", errors.New("GitHub source must not contain credentials")
		}
		if !strings.HasSuffix(value, ".git") {
			value += ".git"
		}
		return value, nil
	}
	parts := strings.Split(value, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.ContainsAny(value, " \\?#@") {
		return "", errors.New("invalid GitHub source")
	}
	return "https://github.com/" + value + ".git", nil
}

func refreshIntegrationCheckout(
	ctx context.Context,
	commands systemCommandExecutor,
	cloneURL, ref, target string,
	validate func(string) error,
) (string, error) {
	parent, err := resolvePath(filepath.Dir(target))
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", errors.New("cannot create integration checkout directory")
	}
	target, err = resolvePath(target)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(parent, target)
	if err != nil || relative == "." || relative == ".." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", errors.New("integration checkout escapes its data directory")
	}
	staging, err := os.MkdirTemp(parent, "."+filepath.Base(target)+"-")
	if err != nil {
		return "", errors.New("cannot create integration staging directory")
	}
	removeStaging := true
	defer func() {
		if removeStaging {
			_ = os.RemoveAll(staging)
		}
	}()
	if _, err := commands.Run(ctx, "git", "clone", "--depth", "1", "--branch", ref, cloneURL, staging); err != nil {
		return "", errors.New("failed to clone the GitHub source")
	}
	if err := validate(staging); err != nil {
		return "", err
	}
	backup := ""
	if _, statErr := os.Lstat(target); statErr == nil {
		backup, err = os.MkdirTemp(parent, "."+filepath.Base(target)+"-previous-")
		if err != nil {
			return "", errors.New("cannot create integration backup path")
		}
		if err := os.Remove(backup); err != nil {
			return "", err
		}
		if err := os.Rename(target, backup); err != nil {
			return "", errors.New("cannot preserve the previous integration checkout")
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return "", errors.New("cannot inspect integration checkout")
	}
	if err := os.Rename(staging, target); err != nil {
		if backup != "" {
			_ = os.Rename(backup, target)
		}
		return "", errors.New("cannot activate the refreshed integration checkout")
	}
	removeStaging = false
	if backup != "" {
		_ = os.RemoveAll(backup)
	}
	return target, nil
}

func pathsEqual(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
	}
	return filepath.Clean(left) == filepath.Clean(right)
}

func displayPath(path string) string {
	return fmt.Sprintf("%q", filepath.Base(filepath.Clean(path)))
}
