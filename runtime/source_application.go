package runtime

import (
	"context"
	"errors"

	"github.com/thunguo/powercontext-go/source"
)

type SourceReceipt struct {
	Ref      source.Ref
	Sequence int64
}

type SourceCaptureBackend interface {
	Capture(context.Context, string, source.ContentCapture) (source.Ref, int64, error)
}

type SourceApplication struct {
	runtime *Runtime
	backend SourceCaptureBackend
}

func NewSourceApplication(runtime *Runtime, backend SourceCaptureBackend) (*SourceApplication, error) {
	if runtime == nil || backend == nil {
		return nil, errors.New("runtime: Source application dependencies must not be nil")
	}
	return &SourceApplication{runtime: runtime, backend: backend}, nil
}

func (a *SourceApplication) CaptureContent(
	ctx context.Context,
	scopeID, sourceID, content string,
	metadata map[string]any,
) (SourceReceipt, error) {
	capture, err := source.NewContentCapture(sourceID, content, metadata)
	if err != nil {
		return SourceReceipt{}, err
	}
	var result SourceReceipt
	err = a.runtime.ScopedWrite(ctx, scopeID, func(ctx context.Context, scope string) error {
		ref, sequence, operationErr := a.backend.Capture(ctx, scope, capture)
		result = SourceReceipt{Ref: ref, Sequence: sequence}
		return operationErr
	})
	return result, err
}
