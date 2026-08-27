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

package httpapi

import (
	"context"
	"errors"

	v1 "github.com/ob-labs/powercontext-go/api/v1"
)

// Security satisfies ogen's optional OpenAPI bearer scheme. Enforcement is
// performed by Wrap because authentication also covers metrics, MCP and the
// Dashboard; keeping this handler permissive when disabled preserves the
// contract's anonymous alternative.
type Security struct {
	token string
}

func NewSecurity(token string) (*Security, error) {
	if token == "" {
		return &Security{}, nil
	}
	if containsLineBreak(token) {
		return nil, errors.New("httpapi: bearer token must not contain line breaks")
	}
	return &Security{token: token}, nil
}

func (s *Security) HandleBearerAuth(ctx context.Context, _ v1.OperationName, auth v1.BearerAuth) (context.Context, error) {
	if s == nil || s.token == "" || validBearer("Bearer "+auth.Token, s.token) {
		return ctx, nil
	}
	return nil, errors.New("invalid bearer token")
}

func containsLineBreak(value string) bool {
	for _, r := range value {
		if r == '\r' || r == '\n' {
			return true
		}
	}
	return false
}

var _ v1.SecurityHandler = (*Security)(nil)
