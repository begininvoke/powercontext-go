package sqlstore

import (
	"encoding/hex"
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/ob-labs/powercontext-go/artifact/memory"
)

func TestSQLiteVec1ProfileContract(t *testing.T) {
	t.Parallel()
	valid, err := memory.NewEmbeddingProfile("profile", "model", 3, "unit")
	if err != nil {
		t.Fatal(err)
	}
	index, err := NewSQLiteMemoryVec1Index(valid)
	if err != nil {
		t.Fatal(err)
	}
	capabilities := index.Capabilities()
	if !capabilities.Vector || capabilities.FTS || capabilities.Hybrid ||
		capabilities.EmbeddingProfile == nil || *capabilities.EmbeddingProfile != valid {
		t.Fatalf("capabilities = %#v", capabilities)
	}

	notUnit, err := memory.NewEmbeddingProfile("profile", "model", 3, "none")
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewSQLiteMemoryVec1Index(notUnit)
	var unsupported *memory.CapabilityNotSupportedError
	if !errors.As(err, &unsupported) || unsupported.Capability != "vector" {
		t.Fatalf("profile error = %v", err)
	}
}

func TestSQLiteVec1VersionCompatibility(t *testing.T) {
	t.Parallel()
	for _, value := range []any{
		"sqlite-vec version 0.7.0", []byte("vec1 version 0.8 (build 1)"), "version 1.0",
	} {
		if err := validateVec1Info(value); err != nil {
			t.Errorf("validateVec1Info(%q): %v", value, err)
		}
	}
	for _, value := range []any{nil, "0.7.0", "version 0.6.99", "version x.7"} {
		err := validateVec1Info(value)
		var unsupported *memory.CapabilityNotSupportedError
		if !errors.As(err, &unsupported) || unsupported.Detail != "SQLite Vec1 0.7 or newer is required" {
			t.Errorf("validateVec1Info(%q) error = %v", value, err)
		}
	}
}

func TestSQLiteVec1NativeFloat32Codec(t *testing.T) {
	t.Parallel()
	packed, err := packSQLiteVector([]float64{1, -2.5, 0.125})
	if err != nil {
		t.Fatal(err)
	}
	// Linux/macOS amd64 and arm64 use the same little-endian bytes as Python's
	// struct.pack("=3f", ...), which is the frozen on-disk Vec1 contract.
	if got := hex.EncodeToString(packed); got != "0000803f000020c00000003e" {
		t.Fatalf("packed vector = %s", got)
	}
	decoded, err := unpackSQLiteVector(packed, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, []float64{1, -2.5, 0.125}) {
		t.Fatalf("decoded vector = %#v", decoded)
	}
	if _, err := unpackSQLiteVector(packed, 2); err == nil {
		t.Fatal("wrong vector dimension was accepted")
	}
	if _, err := unpackSQLiteVector("not-a-blob", 3); err == nil {
		t.Fatal("non-blob vector was accepted")
	}
	if _, err := packSQLiteVector([]float64{math.MaxFloat64}); err == nil {
		t.Fatal("float32 overflow was accepted")
	}
}

func TestSQLiteVec1DistanceValidation(t *testing.T) {
	t.Parallel()
	for _, value := range []any{float64(1.25), float32(1.25), int64(1), []byte("1.25"), "1.25"} {
		got, err := sqliteVectorDistance(value)
		if err != nil || got != 1.25 && got != 1 {
			t.Errorf("sqliteVectorDistance(%T(%v)) = %v, %v", value, value, got, err)
		}
	}
	for _, value := range []any{-1.0, math.NaN(), math.Inf(1), "NaN", struct{}{}} {
		if _, err := sqliteVectorDistance(value); err == nil {
			t.Errorf("sqliteVectorDistance(%T(%v)) accepted", value, value)
		}
	}
}
