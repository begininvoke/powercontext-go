package endpoint

import (
	"context"

	v1 "github.com/ob-labs/powercontext-go/api/v1"
	"github.com/ob-labs/powercontext-go/contextpack"
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
