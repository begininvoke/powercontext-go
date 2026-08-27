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
