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

package endpoint

import (
	"context"

	v1 "github.com/ob-labs/powercontext-go/api/v1"
	"github.com/ob-labs/powercontext-go/internal/contextpack"
)

type ContextOperations interface {
	Prepare(context.Context, string, contextpack.Request) (contextpack.Prepared, error)
}

func (h *Handler) PrepareContext(
	ctx context.Context,
	req *v1.PrepareContextRequest,
) (v1.PrepareContextRes, error) {
	if h.context == nil {
		return nil, &RuntimeNotReadyError{}
	}
	request, err := contextpack.NewRequest(req.Query, req.MaxBytes.Or(contextpack.DefaultMaxBytes))
	if err != nil {
		return nil, &InvalidRequestError{Field: "request"}
	}
	prepared, err := h.context.Prepare(ctx, req.ScopeID, request)
	if err != nil {
		return nil, err
	}
	content := v1.NilString{}
	if value := prepared.Content(); value == nil {
		content.SetToNull()
	} else {
		content.SetTo(*value)
	}
	return &v1.PreparedContextHeaders{
		XPowerContextRequestID: requestID(ctx),
		Response: v1.PreparedContext{
			Schema:       v1.PreparedContextSchema(prepared.Schema()),
			Status:       v1.PreparedContextStatus(prepared.Status()),
			Content:      content,
			ContentBytes: prepared.ContentBytes(),
		},
	}, nil
}
