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

package sqlstore

import (
	"strings"
	"testing"
)

func TestHandoffCodecRejectsUnknownContentSchema(t *testing.T) {
	t.Parallel()
	_, err := decodeHandoff([]byte(`{
        "schema":"powercontext.handoff.v2",
        "objective":"objective",
        "state":[],
        "disposition":"complete",
        "next_action":null,
        "omissions":[]
    }`))
	if err == nil || !strings.Contains(err.Error(), `unsupported Handoff schema "powercontext.handoff.v2"`) {
		t.Fatalf("error = %v, want unsupported schema", err)
	}
}
