package v1

import (
	"time"

	"github.com/go-faster/jx"
	ogenjson "github.com/ogen-go/ogen/json"
)

const utcMicrosecondLayout = "2006-01-02T15:04:05.000000Z07:00"

// encodeDateTime matches Python's externally observable datetime encoding:
// values are UTC, precision is truncated to microseconds, a non-zero fraction
// contains exactly six digits, and an all-zero fraction is omitted.
func encodeDateTime(encoder *jx.Encoder, value time.Time) {
	value = value.UTC().Truncate(time.Microsecond)
	layout := time.RFC3339
	if value.Nanosecond() != 0 {
		layout = utcMicrosecondLayout
	}
	ogenjson.EncodeTimeFormat(encoder, value, layout)
}
