package inference

import (
	"context"
)

const readinessPrompt = "Reply with one token."

// ProbeTextModel sends the smallest provider-neutral generation request. It
// intentionally bypasses structured generation, tracing, usage reporting, and
// prompt assets; the caller owns the wall-clock timeout and result cache.
func ProbeTextModel(ctx context.Context, model TextModel) error {
	if model == nil {
		return NewConfigurationError("model", "")
	}
	message, err := NewMessage(RoleUser, readinessPrompt)
	if err != nil {
		return err
	}
	maxTokens := int64(1)
	settings, err := NewGenerationSettings(nil, &maxTokens)
	if err != nil {
		return err
	}
	_, err = model.Complete(ctx, newProbeTextRequest([]Message{message}, settings))
	return err
}
