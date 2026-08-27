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
	requesttrace "github.com/ob-labs/powercontext-go/internal/observability/tracing"
)

func requestID(ctx context.Context) v1.OptString {
	value, ok := requesttrace.RequestID(ctx)
	if !ok {
		return v1.OptString{}
	}
	return v1.NewOptString(value)
}
