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

package contextpack

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func FuzzTruncateUTF8PreservesRuneBoundariesAndBudget(f *testing.F) {
	f.Add("plain ASCII", 8)
	f.Add("迁移到 Go 并保持兼容", 12)
	f.Add("e\u0301 and é", 7)
	f.Fuzz(func(t *testing.T, text string, rawBudget int) {
		if !utf8.ValidString(text) || len(text) > 64*1024 {
			t.Skip()
		}
		budget := 3 + positiveModulo(rawBudget, 4094)
		got := truncateUTF8(text, budget)
		if !utf8.ValidString(got) {
			t.Fatalf("truncated value is not valid UTF-8: %q", got)
		}
		if len([]byte(got)) > budget {
			t.Fatalf("truncated value uses %d bytes, budget %d", len([]byte(got)), budget)
		}
		if !strings.HasSuffix(got, ellipsis) {
			t.Fatalf("truncated value does not end in ellipsis: %q", got)
		}
		if again := truncateUTF8(text, budget); again != got {
			t.Fatal("UTF-8 truncation is not deterministic")
		}
	})
}

func positiveModulo(value, modulus int) int {
	result := value % modulus
	if result < 0 {
		result += modulus
	}
	return result
}
