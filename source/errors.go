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
	"fmt"
	"reflect"
)

// NotFoundError reports that an exact Source value is absent from a catalog.
type NotFoundError struct {
	Source Value
}

func (e *NotFoundError) Error() string { return "source was not found" }

// AdapterNotFoundError reports that no adapter owns an exact Go type.
type AdapterNotFoundError struct {
	Route string
	Type  reflect.Type
}

func (e *AdapterNotFoundError) Error() string {
	return fmt.Sprintf("no Source adapter is registered for %s type %s", e.Route, typeName(e.Type))
}

// InvalidAdapterError reports an invalid adapter declaration.
type InvalidAdapterError struct {
	Type   reflect.Type
	Field  string
	Detail string
}

func (e *InvalidAdapterError) Error() string {
	return fmt.Sprintf("invalid Source adapter %s %s: %s", typeName(e.Type), e.Field, e.Detail)
}

// ConflictError reports ambiguous immutable catalog routing.
type ConflictError struct {
	Field string
	Value any
}

func (e *ConflictError) Error() string {
	if valueType, ok := e.Value.(reflect.Type); ok {
		return fmt.Sprintf("duplicate Source %s: %s", e.Field, typeName(valueType))
	}
	return fmt.Sprintf("duplicate Source %s: %v", e.Field, e.Value)
}

// InvalidReferenceError reports an invalid Source identity.
type InvalidReferenceError struct {
	Field  string
	Detail string
}

func (e *InvalidReferenceError) Error() string {
	return fmt.Sprintf("invalid Source reference %s: %s", e.Field, e.Detail)
}

// InvalidEntryError reports a value that does not satisfy the Source contract.
type InvalidEntryError struct {
	Type reflect.Type
}

func (e *InvalidEntryError) Error() string {
	return fmt.Sprintf("catalog entries must be Source values, got %s", typeName(e.Type))
}

// InvalidResultError reports an adapter that returned a value outside its
// declared exact Source type.
type InvalidResultError struct {
	AdapterName string
	Operation   string
	Expected    reflect.Type
	Actual      reflect.Type
}

func (e *InvalidResultError) Error() string {
	return fmt.Sprintf(
		"Source adapter %q returned %s from %s, expected %s",
		e.AdapterName,
		typeName(e.Actual),
		e.Operation,
		typeName(e.Expected),
	)
}

func typeName(value reflect.Type) string {
	if value == nil {
		return "<nil>"
	}
	if value.PkgPath() == "" {
		return value.String()
	}
	return value.PkgPath() + "." + value.Name()
}
