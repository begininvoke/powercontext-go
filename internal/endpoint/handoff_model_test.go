package endpoint

import (
	"errors"
	"testing"

	v1 "github.com/ob-labs/powercontext-go/api/v1"
)

func TestHandoffWireRejectsUnknownSchemaVersions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		field string
		call  func() error
	}{
		{
			name:  "content",
			field: "content.schema",
			call: func() error {
				_, err := runtimeHandoffContent(v1.HandoffContent{Schema: v1.HandoffSchema("powercontext.handoff.v2")})
				return err
			},
		},
		{
			name:  "prepared envelope",
			field: "handoff.schema",
			call: func() error {
				_, err := runtimePreparedHandoff(v1.PreparedHandoff{
					Schema: v1.PreparedHandoffSchema("powercontext.prepared-handoff.v2"),
				})
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var invalid *InvalidRequestError
			if err := test.call(); !errors.As(err, &invalid) || invalid.Field != test.field {
				t.Fatalf("error = %#v, want InvalidRequestError for %q", err, test.field)
			}
		})
	}
}
