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

package server

import (
	"context"
	"errors"

	"github.com/ob-labs/powercontext-go/internal/sqlstore"
	embeddedseekdb "github.com/ob-labs/powercontext-go/internal/sqlstore/seekdb"
)

// seekDBInstance preserves the embedded-runtime shutdown boundary: the SQL
// pool must reject and drain work before the local server is stopped.
type seekDBInstance struct {
	database closeResource
	value    closeResource
}

type closeResource interface{ Close(context.Context) error }

var (
	_ closeResource = (*sqlstore.Database)(nil)
	_ closeResource = (*embeddedseekdb.Instance)(nil)
)

func (r *seekDBInstance) Close(ctx context.Context) error {
	if r == nil {
		return nil
	}
	// The Python profile shields both cleanup steps from cancellation and only
	// propagates cancellation after the engine and native instance are closed.
	// Preserve that boundary so the local server can never outlive its pool.
	cleanupContext := context.WithoutCancel(ctx)
	var cleanupErrors []error
	if r.database != nil {
		if err := r.database.Close(cleanupContext); err != nil {
			cleanupErrors = append(cleanupErrors, err)
		}
	}
	if r.value != nil {
		if err := r.value.Close(cleanupContext); err != nil {
			cleanupErrors = append(cleanupErrors, err)
		}
	}
	if cancellation := context.Cause(ctx); cancellation != nil {
		cleanupErrors = append(cleanupErrors, cancellation)
	}
	return errors.Join(cleanupErrors...)
}
