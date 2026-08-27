package runtime

import (
	"context"
	"errors"

	"github.com/ob-labs/powercontext-go/artifact"
	"github.com/ob-labs/powercontext-go/artifact/memory"
	"github.com/ob-labs/powercontext-go/source"
)

const (
	DefaultMemoryArtifactID  = "memory"
	DefaultSourceWindowLimit = int64(100)
	memorySearchAttempts     = 3
)

type MemoryServiceFactory func(string) (*memory.Service, error)

// MemoryFlushBackend is the use-case-shaped persistence port for the two
// transaction boundaries surrounding extraction. ObserveWindow returns one
// relational snapshot; ApplyWindow must atomically apply the Memory plan and
// cursor CAS.
type MemoryFlushBackend interface {
	ObserveWindow(context.Context, string, int64) (
		previous source.Cursor,
		next source.Cursor,
		generation *int64,
		highWatermark int64,
		values []source.Value,
		err error,
	)
	ApplyWindow(context.Context, string, memory.WritePlan, source.Cursor, *int64) (*memory.Memory, error)
}

type MemoryFlushBackendFactory func(string) (MemoryFlushBackend, error)

type MemoryApplication struct {
	runtime           *Runtime
	services          MemoryServiceFactory
	flushes           MemoryFlushBackendFactory
	memoryArtifactID  string
	sourceWindowLimit int64
}

func NewMemoryApplication(runtime *Runtime, services MemoryServiceFactory, memoryArtifactID string) (*MemoryApplication, error) {
	if runtime == nil || services == nil {
		return nil, errors.New("runtime: Memory application dependencies must not be nil")
	}
	if memoryArtifactID == "" {
		memoryArtifactID = DefaultMemoryArtifactID
	}
	if _, err := artifact.NewRef(memory.Family, memoryArtifactID, 1); err != nil {
		return nil, err
	}
	return &MemoryApplication{
		runtime: runtime, services: services, memoryArtifactID: memoryArtifactID,
		sourceWindowLimit: DefaultSourceWindowLimit,
	}, nil
}

// NewMemoryApplicationWithFlush constructs the complete Memory application.
// The simpler constructor remains useful for deployments that intentionally
// expose only explicit Memory operations.
func NewMemoryApplicationWithFlush(
	runtime *Runtime,
	services MemoryServiceFactory,
	flushes MemoryFlushBackendFactory,
	memoryArtifactID string,
	sourceWindowLimit int64,
) (*MemoryApplication, error) {
	if flushes == nil {
		return nil, errors.New("runtime: Memory Flush backend factory must not be nil")
	}
	if sourceWindowLimit < 1 {
		return nil, errors.New("runtime: source_window_limit must be positive")
	}
	application, err := NewMemoryApplication(runtime, services, memoryArtifactID)
	if err != nil {
		return nil, err
	}
	application.flushes = flushes
	application.sourceWindowLimit = sourceWindowLimit
	return application, nil
}

func (a *MemoryApplication) service(scope string) (*memory.Service, error) {
	service, err := a.services(scope)
	if err != nil {
		return nil, err
	}
	if service == nil {
		return nil, &StateError{Code: "memory"}
	}
	return service, nil
}
