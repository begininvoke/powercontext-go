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
	"errors"
	"strings"
	"testing"
)

func TestWrappedInferenceErrorsPreserveCauseWithoutLeakingItsText(t *testing.T) {
	provider := errors.New("secret provider response")
	for _, err := range []error{
		WrapConfigurationError("provider-rejected", "", provider),
		WrapUnavailableError("generate", provider),
	} {
		if !errors.Is(err, provider) {
			t.Fatalf("error %T did not retain its cause", err)
		}
		if strings.Contains(err.Error(), provider.Error()) || strings.Contains(err.Error(), "secret") {
			t.Fatalf("error %T leaked its cause: %q", err, err)
		}
	}
}
