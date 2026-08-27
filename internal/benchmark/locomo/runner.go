package locomo

import (
	"context"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ob-labs/powercontext-go/artifact/memory"
	"github.com/ob-labs/powercontext-go/inference"
	pcruntime "github.com/ob-labs/powercontext-go/internal/runtime"
)

const maxObservationBytes = 16 << 20

type Progress func(string)

// Operations is the narrow use-case boundary required by the benchmark. The
// production composition uses the same Runtime operations; tests can provide a
// deterministic implementation without a database or model provider.
type Operations interface {
	Capture(context.Context, string, string, string, map[string]any) (int64, error)
	Flush(context.Context, string) (pcruntime.MemoryFlushResult, error)
	List(context.Context, string) (pcruntime.MemoryEntriesPage, error)
	Search(context.Context, string, string, int, memory.SearchMode) (pcruntime.MemorySearchPage, error)
}

func benchmarkUsage(value inference.Usage) Usage {
	result := Usage{Requests: value.Requests}
	if value.InputTokens != nil {
		result.InputTokens = int(*value.InputTokens)
	}
	if value.OutputTokens != nil {
		result.OutputTokens = int(*value.OutputTokens)
	}
	return result
}

func sourceIDs(values []RetrievedMemory) [][]string {
	result := make([][]string, len(values))
	for index, value := range values {
		result[index] = slices.Clone(value.SourceIDs)
	}
	return result
}

func answerSourceIDs(values []AnswerSourceSession) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = value.SourceID
	}
	return result
}

func sourceSuffix(value string) string {
	if index := strings.LastIndexByte(value, ':'); index >= 0 {
		return value[index+1:]
	}
	return value
}

func errorType(err error) string {
	if err == nil {
		return ""
	}
	typeOf := reflect.TypeOf(err)
	for typeOf.Kind() == reflect.Pointer {
		typeOf = typeOf.Elem()
	}
	if typeOf.Name() != "" {
		return typeOf.Name()
	}
	return "error"
}

func equalInt64Pointer(left, right *int64) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func milliseconds(value time.Duration) float64 { return float64(value) / float64(time.Millisecond) }

func pythonUTC(value time.Time) string {
	value = value.UTC().Truncate(time.Microsecond)
	if value.Nanosecond() == 0 {
		return value.Format("2006-01-02T15:04:05+00:00")
	}
	return value.Format("2006-01-02T15:04:05.000000+00:00")
}

func sortedMetricGroups(values map[string]Summary) []string {
	result := make([]string, 0, len(values))
	for name := range values {
		if strings.HasPrefix(name, "category_") {
			result = append(result, name)
		}
	}
	sort.Strings(result)
	return result
}
