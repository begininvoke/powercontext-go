// Copyright (c) 2026 OceanBase.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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
