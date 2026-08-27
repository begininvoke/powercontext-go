// Copyright (c) 2026 OceanBase.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package source

import (
	"context"
	"fmt"
	"reflect"
)

// Adapter resolves one exact input type and reads one exact Source type.
type Adapter[I any, S Value, V any] interface {
	Name() string
	Resolve(context.Context, I) (S, error)
	Read(context.Context, S) (V, error)
}

type erasedAdapter struct {
	name       string
	inputType  reflect.Type
	sourceType reflect.Type
	resolve    func(context.Context, any) (Value, error)
	read       func(context.Context, Value) (any, error)
}

// Registry is a mutable builder for immutable Catalog routing.
type Registry struct {
	byInput  map[reflect.Type]erasedAdapter
	bySource map[reflect.Type]erasedAdapter
}

// Register adds an exact typed adapter to a Registry.
func Register[I any, S Value, V any](registry *Registry, adapter Adapter[I, S, V]) error {
	if registry == nil {
		return &InvalidAdapterError{Field: "registry", Detail: "must not be nil"}
	}
	adapterType := reflect.TypeOf(adapter)
	if isNil(adapter) {
		return &InvalidAdapterError{Type: adapterType, Field: "adapter", Detail: "must not be nil"}
	}
	name := adapter.Name()
	if err := validateReferencePart("source_type", name, MaxTypeLength); err != nil {
		return &InvalidAdapterError{Type: adapterType, Field: "name", Detail: err.Error()}
	}
	inputType := reflect.TypeFor[I]()
	sourceType := reflect.TypeFor[S]()
	if inputType.Kind() == reflect.Interface {
		return &InvalidAdapterError{Type: adapterType, Field: "input_type", Detail: "must be a concrete type"}
	}
	if sourceType.Kind() == reflect.Interface {
		return &InvalidAdapterError{Type: adapterType, Field: "source_type", Detail: "must be a concrete type"}
	}
	if registry.byInput == nil {
		registry.byInput = make(map[reflect.Type]erasedAdapter)
		registry.bySource = make(map[reflect.Type]erasedAdapter)
	}
	if _, exists := registry.byInput[inputType]; exists {
		return &ConflictError{Field: "input_type", Value: inputType}
	}
	if _, exists := registry.bySource[sourceType]; exists {
		return &ConflictError{Field: "source_type", Value: sourceType}
	}
	erased := erasedAdapter{
		name:       name,
		inputType:  inputType,
		sourceType: sourceType,
		resolve: func(ctx context.Context, value any) (Value, error) {
			resolved, err := adapter.Resolve(ctx, value.(I))
			if err != nil {
				return nil, err
			}
			return resolved, nil
		},
		read: func(ctx context.Context, value Value) (any, error) {
			return adapter.Read(ctx, value.(S))
		},
	}
	registry.byInput[inputType] = erased
	registry.bySource[sourceType] = erased
	return nil
}

// CatalogBackend provides persisted Source reads.
type CatalogBackend interface {
	Get(context.Context, Value) (Value, error)
	List(context.Context) ([]Value, error)
}

// Catalog is an immutable adapter registry over persisted Source reads.
type Catalog struct {
	backend  CatalogBackend
	byInput  map[reflect.Type]erasedAdapter
	bySource map[reflect.Type]erasedAdapter
}

// NewCatalog freezes a Registry into a Catalog.
func NewCatalog(backend CatalogBackend, registry Registry) (*Catalog, error) {
	if backend == nil || isNil(backend) {
		return nil, fmt.Errorf("source catalog backend must not be nil")
	}
	return &Catalog{
		backend:  backend,
		byInput:  cloneAdapters(registry.byInput),
		bySource: cloneAdapters(registry.bySource),
	}, nil
}

func (c *Catalog) Resolve(ctx context.Context, input any) (Value, error) {
	inputType := reflect.TypeOf(input)
	adapter, ok := c.byInput[inputType]
	if !ok {
		return nil, &AdapterNotFoundError{Route: "input", Type: inputType}
	}
	resolved, err := adapter.resolve(ctx, input)
	if err != nil {
		return nil, err
	}
	actual := reflect.TypeOf(resolved)
	if actual != adapter.sourceType || isNil(resolved) {
		return nil, &InvalidResultError{
			AdapterName: adapter.name,
			Operation:   "resolve",
			Expected:    adapter.sourceType,
			Actual:      actual,
		}
	}
	if _, err := c.Ref(resolved); err != nil {
		return nil, err
	}
	return resolved, nil
}

func (c *Catalog) Read(ctx context.Context, value Value) (any, error) {
	adapter, err := c.adapterFor(value)
	if err != nil {
		return nil, err
	}
	return adapter.read(ctx, value)
}

func (c *Catalog) Ref(value Value) (Ref, error) {
	adapter, err := c.adapterFor(value)
	if err != nil {
		return Ref{}, err
	}
	return NewRef(adapter.name, value.SourceName())
}

func (c *Catalog) Get(ctx context.Context, value Value) (Value, error) {
	if _, err := c.Ref(value); err != nil {
		return nil, err
	}
	stored, err := c.backend.Get(ctx, value)
	if err != nil {
		return nil, err
	}
	if _, err := c.Ref(stored); err != nil {
		return nil, err
	}
	if reflect.TypeOf(stored) != reflect.TypeOf(value) || !reflect.DeepEqual(stored, value) {
		return nil, &NotFoundError{Source: value}
	}
	return stored, nil
}

func (c *Catalog) List(ctx context.Context) ([]Value, error) {
	values, err := c.backend.List(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]Value, len(values))
	for index, value := range values {
		if _, err := c.Ref(value); err != nil {
			return nil, err
		}
		result[index] = value
	}
	return result, nil
}

func (c *Catalog) adapterFor(value Value) (erasedAdapter, error) {
	if value == nil || isNil(value) {
		return erasedAdapter{}, &InvalidEntryError{Type: reflect.TypeOf(value)}
	}
	valueType := reflect.TypeOf(value)
	adapter, ok := c.bySource[valueType]
	if !ok {
		return erasedAdapter{}, &AdapterNotFoundError{Route: "source", Type: valueType}
	}
	if err := validateValue(value); err != nil {
		return erasedAdapter{}, err
	}
	return adapter, nil
}

func cloneAdapters(values map[reflect.Type]erasedAdapter) map[reflect.Type]erasedAdapter {
	cloned := make(map[reflect.Type]erasedAdapter, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func isNil(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
