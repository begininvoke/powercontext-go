package jcs

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"
)

func FuzzMarshalCanonicalJSONIsIdempotent(f *testing.F) {
	f.Add([]byte(`{"z":"e\u0301","a":[2,true,null]}`))
	f.Add([]byte(`{"é":1,"e\u0301":2}`))
	f.Add([]byte(`[-0.0,1e30,"\u2028"]`))
	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) > 16*1024 {
			t.Skip()
		}
		decoder := json.NewDecoder(bytes.NewReader(input))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil {
			return
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			return
		}
		first, err := Marshal(value)
		if err != nil {
			return
		}
		if !json.Valid(first) {
			t.Fatalf("canonical output is not JSON: %q", first)
		}
		decoder = json.NewDecoder(bytes.NewReader(first))
		decoder.UseNumber()
		var canonicalValue any
		if err := decoder.Decode(&canonicalValue); err != nil {
			t.Fatal(err)
		}
		second, err := Marshal(canonicalValue)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first, second) {
			t.Fatalf("canonicalization is not idempotent:\nfirst:  %s\nsecond: %s", first, second)
		}
	})
}
