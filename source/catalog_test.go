package source_test

import (
	"context"
	"errors"
	"testing"

	"github.com/thunguo/powercontext-go/source"
)

type input struct{ name string }

type capturedSource struct{ name string }

func (s capturedSource) SourceName() string { return s.name }
func (capturedSource) SourceMaterialization() source.Materialization {
	return source.Captured
}
func (capturedSource) SourceDescription() (string, bool) { return "", false }

type referencedSource struct{ name string }

func (s referencedSource) SourceName() string { return s.name }
func (referencedSource) SourceMaterialization() source.Materialization {
	return source.Referenced
}
func (referencedSource) SourceDescription() (string, bool) { return "", false }

type adapter struct{ resolveReferenced bool }

func (adapter) Name() string { return "capture" }
func (a adapter) Resolve(_ context.Context, value input) (capturedSource, error) {
	return capturedSource{name: value.name}, nil
}
func (adapter) Read(_ context.Context, value capturedSource) (string, error) {
	return "read:" + value.name, nil
}

type backend struct{ values []source.Value }

func (b *backend) Get(_ context.Context, value source.Value) (source.Value, error) {
	for _, candidate := range b.values {
		if candidate.SourceName() == value.SourceName() {
			return candidate, nil
		}
	}
	return nil, &source.NotFoundError{Source: value}
}
func (b *backend) List(context.Context) ([]source.Value, error) {
	return append([]source.Value(nil), b.values...), nil
}

func TestCatalogRoutesByExactConcreteType(t *testing.T) {
	var registry source.Registry
	if err := source.Register(&registry, adapter{}); err != nil {
		t.Fatal(err)
	}
	catalog, err := source.NewCatalog(&backend{}, registry)
	if err != nil {
		t.Fatal(err)
	}

	resolved, err := catalog.Resolve(context.Background(), input{name: "one"})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := catalog.Ref(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if ref.Type() != "capture" || ref.ID() != "one" {
		t.Fatalf("unexpected ref: %v", ref)
	}
	read, err := catalog.Read(context.Background(), resolved)
	if err != nil {
		t.Fatal(err)
	}
	if read != "read:one" {
		t.Fatalf("unexpected read value: %v", read)
	}

	_, err = catalog.Resolve(context.Background(), &input{name: "one"})
	var notFound *source.AdapterNotFoundError
	if !errors.As(err, &notFound) || notFound.Route != "input" {
		t.Fatalf("expected exact input adapter error, got %v", err)
	}
	_, err = catalog.Ref(referencedSource{name: "one"})
	if !errors.As(err, &notFound) || notFound.Route != "source" {
		t.Fatalf("expected exact source adapter error, got %v", err)
	}
}

func TestCatalogRejectsDuplicateRoutes(t *testing.T) {
	var registry source.Registry
	if err := source.Register(&registry, adapter{}); err != nil {
		t.Fatal(err)
	}
	err := source.Register(&registry, adapter{})
	var conflict *source.ConflictError
	if !errors.As(err, &conflict) || conflict.Field != "input_type" {
		t.Fatalf("expected input conflict, got %v", err)
	}
}

func TestCatalogGetRequiresExactPersistedValue(t *testing.T) {
	var registry source.Registry
	if err := source.Register(&registry, adapter{}); err != nil {
		t.Fatal(err)
	}
	catalog, err := source.NewCatalog(&backend{values: []source.Value{capturedSource{name: "stored"}}}, registry)
	if err != nil {
		t.Fatal(err)
	}
	_, err = catalog.Get(context.Background(), capturedSource{name: "requested"})
	var notFound *source.NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestNewRefMatchesUnicodeCharacterLimits(t *testing.T) {
	identifier := "界"
	for range source.MaxIDLength - 1 {
		identifier += "界"
	}
	if _, err := source.NewRef("content", identifier); err != nil {
		t.Fatalf("valid rune length rejected: %v", err)
	}
	if _, err := source.NewRef("content", identifier+"界"); err == nil {
		t.Fatal("expected overlong identifier to be rejected")
	}
}

func TestNewRefRejectsFrozenPythonInformationSeparatorWhitespace(t *testing.T) {
	for _, value := range []string{"\u001c", "\u001ccontent", "content\u001f"} {
		if _, err := source.NewRef(value, "source-id"); err == nil {
			t.Fatalf("source type %q was accepted", value)
		}
		if _, err := source.NewRef("content", value); err == nil {
			t.Fatalf("source ID %q was accepted", value)
		}
	}
}
