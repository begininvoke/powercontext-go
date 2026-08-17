package v1

import (
	"testing"
	"time"

	"github.com/go-faster/jx"
)

func TestEncodeDateTimeMatchesPythonUTCMicroseconds(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		value time.Time
		want  string
	}{
		{
			name:  "zero fraction",
			value: time.Date(2026, time.August, 17, 15, 59, 58, 0, time.UTC),
			want:  `"2026-08-17T15:59:58Z"`,
		},
		{
			name:  "fixed six digit fraction",
			value: time.Date(2026, time.August, 17, 15, 59, 58, 120000000, time.UTC),
			want:  `"2026-08-17T15:59:58.120000Z"`,
		},
		{
			name:  "sub-microsecond precision is truncated",
			value: time.Date(2026, time.August, 17, 15, 59, 58, 123456789, time.FixedZone("offset", 8*60*60)),
			want:  `"2026-08-17T07:59:58.123456Z"`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			encoder := &jx.Encoder{}
			encodeDateTime(encoder, test.value)
			if got := string(encoder.Bytes()); got != test.want {
				t.Fatalf("encoded datetime = %s, want %s", got, test.want)
			}
		})
	}
}
