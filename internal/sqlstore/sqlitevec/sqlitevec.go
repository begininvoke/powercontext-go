// Package sqlitevec statically embeds the sqlite-vec v0.1.9 amalgamation used
// by the Python v0.0.2 runtime. Keeping the exact C source in-tree makes every
// supported release artifact self-contained and avoids depending on a host
// loadable-extension path.
package sqlitevec

/*
#cgo CFLAGS: -DSQLITE_CORE
#cgo darwin CFLAGS: -Wno-deprecated-declarations
#cgo linux LDFLAGS: -lm
#include "sqlite-vec.h"
*/
import "C"

import (
	"fmt"
	"sync"
)

const Version = "v0.1.9"

var (
	registerOnce sync.Once
	registerErr  error
)

// RegisterAuto arranges for sqlite-vec to initialize on every SQLite
// connection opened later in this process. sqlite3_auto_extension maintains
// process-global state, so registration is intentionally idempotent and is not
// cancelled while independent databases may still be alive.
func RegisterAuto() error {
	registerOnce.Do(func() {
		if result := int(C.sqlite3_auto_extension((*[0]byte)(C.sqlite3_vec_init))); result != 0 {
			registerErr = fmt.Errorf("sqlite-vec: register auto extension: SQLite result %d", result)
		}
	})
	return registerErr
}
