package scheduler

import (
	"encoding/base64"
	"errors"
	"testing"
)

func FuzzRestrictedPickleJobDecoder(f *testing.F) {
	valid, err := base64.StdEncoding.DecodeString(pythonSourceWindowPickle)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid)
	f.Add([]byte{0x80, 5, 'N', '.'})
	f.Add([]byte{0x80, 5, 0x8c, 5, 'p', 'o', 's', 'i', 'x', 0x8c, 6, 's', 'y', 's', 't', 'e', 'm', 0x93, ')', 'R', '.'})
	f.Fuzz(func(t *testing.T, blob []byte) {
		_, err := decodeJobState(
			blob,
			SourceWindowJobID,
			"/private/tmp/powercontext-scheduler-oracle.db",
			1786907189.018739,
		)
		if err == nil {
			return
		}
		var invalid *InvalidJobStateError
		if !errors.As(err, &invalid) {
			t.Fatalf("decoder returned unclassified error %T: %v", err, err)
		}
	})
}
