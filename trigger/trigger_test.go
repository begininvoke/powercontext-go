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

package trigger_test

import (
	"testing"

	"github.com/ob-labs/powercontext-go/trigger"
)

func TestTransitionCopiesActions(t *testing.T) {
	actions := []string{"one", "two"}
	transition := trigger.NewTransition(3, actions...)
	actions[0] = "changed"
	got := transition.Actions()
	if got[0] != "one" || transition.State() != 3 {
		t.Fatalf("unexpected transition: %v %v", transition.State(), got)
	}
	got[0] = "changed again"
	if transition.Actions()[0] != "one" {
		t.Fatal("actions accessor leaked mutable storage")
	}
}
