//go:build !cgo || (!darwin && !linux)

package seekdb

import (
	"context"
	"errors"
)

type ConnectionOptions struct {
	Transport string
	Port      uint
	Endpoint  string
	User      string
}

type Config struct {
	Path        string
	LibraryPath string
}

type UnavailableError struct{ Cause error }

func (e *UnavailableError) Error() string {
	return "embedded seekDB requires libseekdb and its sibling seekdb executable on Linux or macOS"
}

func (e *UnavailableError) Unwrap() error { return e.Cause }

type Instance struct{}

func Open(context.Context, Config) (*Instance, error) {
	return nil, &UnavailableError{Cause: errors.New("seekDB is unavailable in this build")}
}

func (*Instance) ConnectionOptions() ConnectionOptions { return ConnectionOptions{} }
func (*Instance) Close(context.Context) error          { return nil }
