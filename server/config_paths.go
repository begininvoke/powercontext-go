package server

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// PowerContextDataDir matches platformdirs.user_data_path("powercontext",
// appauthor=False) on the supported Linux and macOS release targets.
func PowerContextDataDir() (string, error) {
	if configured := strings.TrimSpace(os.Getenv(PowerContextHomeEnv)); configured != "" {
		return absoluteExpandedPath(configured)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("server: resolve user data directory: %w", err)
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "powercontext"), nil
	case "linux":
		if dataHome := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); dataHome != "" {
			return absoluteExpandedPath(filepath.Join(dataHome, "powercontext"))
		}
		return filepath.Join(home, ".local", "share", "powercontext"), nil
	default:
		base, err := os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("server: resolve user data directory: %w", err)
		}
		return filepath.Join(base, "powercontext"), nil
	}
}

func DefaultDatabasePath() (string, error) {
	directory, err := PowerContextDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "powercontext.db"), nil
}

func DefaultSeekDBPath() (string, error) {
	directory, err := PowerContextDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "seekdb"), nil
}

func DefaultSchedulerPath() (string, error) {
	directory, err := PowerContextDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "scheduler.db"), nil
}

func sqliteURL(path string) string {
	if path == ":memory:" {
		return "sqlite+aiosqlite:///:memory:"
	}
	return "sqlite+aiosqlite:///" + filepath.ToSlash(path)
}

// SQLiteDSN converts the frozen SQLAlchemy URL spelling without accepting a
// different database scheme by accident. Three-slash URLs intentionally keep
// a relative database path: the frozen .env.example relies on resolving that
// path from the Server working directory, just as SQLAlchemy does.
func SQLiteDSN(value string) (string, error) {
	const prefix = "sqlite+aiosqlite:///"
	if !strings.HasPrefix(value, prefix) {
		return "", errors.New("server: SQLite URL must use sqlite+aiosqlite")
	}
	database := strings.TrimPrefix(value, prefix)
	if database == "" {
		return "", errors.New("server: SQLite URL must identify a database")
	}
	if database == ":memory:" {
		return database, nil
	}
	return filepath.FromSlash(database), nil
}

func absoluteExpandedPath(value string) (string, error) {
	if value == "~" || strings.HasPrefix(value, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		value = filepath.Join(home, strings.TrimPrefix(value, "~/"))
	}
	absolute, err := filepath.Abs(value)
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
