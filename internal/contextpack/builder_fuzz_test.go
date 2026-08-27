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
