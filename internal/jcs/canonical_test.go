package jcs

import (
	"errors"
	"testing"
)

func TestMarshalNormalizesUnicodeAndSortsKeys(t *testing.T) {
	got, err := Marshal(map[string]any{"z": "e\u0301", "a": []any{2, true}})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"a":[2,true],"z":"é"}`
	if string(got) != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestMarshalRejectsNormalizedKeyCollision(t *testing.T) {
	_, err := Marshal(map[string]any{"é": 1, "e\u0301": 2})
	var collision *KeyCollisionError
	if !errors.As(err, &collision) {
		t.Fatalf("expected key collision, got %v", err)
	}
}
