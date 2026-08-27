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

package runtime

import (
	"context"
	"errors"

	"github.com/ob-labs/powercontext-go/source"
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
