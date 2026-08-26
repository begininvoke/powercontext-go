package server

import (
	"context"
	"errors"

	embeddedseekdb "github.com/ob-labs/powercontext-go/internal/seekdb"
	"github.com/ob-labs/powercontext-go/internal/sqlstore"
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
