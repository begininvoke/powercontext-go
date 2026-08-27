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
	"testing"
)

func TestProbeTextModelUsesMinimalUnstructuredRequest(t *testing.T) {
	var captured TextRequest
	model := textModelFunc(func(_ context.Context, request TextRequest) (TextResponse, error) {
		captured = request
		return NewTextResponse("ok", Usage{Requests: 1})
	})
	if err := ProbeTextModel(t.Context(), model); err != nil {
		t.Fatal(err)
	}
	if len(captured.Instructions()) != 0 || len(captured.Messages()) != 1 ||
		captured.Messages()[0].Role() != RoleUser || captured.Messages()[0].Content() != readinessPrompt {
		t.Fatalf("probe request = %#v", captured)
	}
	if captured.StructuredOutput() {
		t.Fatal("probe requested structured output")
	}
	if maxTokens := captured.Settings().MaxTokens(); maxTokens == nil || *maxTokens != 1 {
		t.Fatalf("max tokens = %v, want 1", maxTokens)
	}
}

type textModelFunc func(context.Context, TextRequest) (TextResponse, error)

func (f textModelFunc) Complete(ctx context.Context, request TextRequest) (TextResponse, error) {
	return f(ctx, request)
}
