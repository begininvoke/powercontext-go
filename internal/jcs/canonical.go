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

package jcs

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strconv"

	webpkijcs "github.com/gowebpki/jcs"
	"golang.org/x/text/unicode/norm"
)

// KeyCollisionError reports two object keys that become equal after NFC
// normalization.
type KeyCollisionError struct{ Key string }

func (e *KeyCollisionError) Error() string {
	return fmt.Sprintf("canonical JSON object keys collide after NFC normalization: %q", e.Key)
}

// Marshal applies recursive Unicode NFC normalization, validates the JSON
// value domain, and emits RFC 8785 JSON Canonicalization Scheme bytes.
func Marshal(value any) ([]byte, error) {
	normalized, err := normalize(value)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("encode canonical JSON input: %w", err)
	}
	canonical, err := webpkijcs.Transform(encoded)
	if err != nil {
		return nil, fmt.Errorf("canonicalize JSON: %w", err)
	}
	return canonical, nil
}

func normalize(value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	if number, ok := value.(json.Number); ok {
		if _, err := strconv.ParseFloat(number.String(), 64); err != nil {
			return nil, fmt.Errorf("invalid JSON number %q", number)
		}
		return number, nil
	}
	reflected := reflect.ValueOf(value)
	for reflected.Kind() == reflect.Interface || reflected.Kind() == reflect.Pointer {
		if reflected.IsNil() {
			return nil, nil
		}
		reflected = reflected.Elem()
	}
	switch reflected.Kind() {
	case reflect.Bool:
		return reflected.Bool(), nil
	case reflect.String:
		return norm.NFC.String(reflected.String()), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return reflected.Int(), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return reflected.Uint(), nil
	case reflect.Float32, reflect.Float64:
		value := reflected.Float()
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, fmt.Errorf("non-finite number is not JSON-compatible")
		}
		return value, nil
	case reflect.Map:
		if reflected.Type().Key().Kind() != reflect.String {
			return nil, fmt.Errorf("canonical JSON object keys must be strings")
		}
		result := make(map[string]any, reflected.Len())
		iterator := reflected.MapRange()
		for iterator.Next() {
			key := norm.NFC.String(iterator.Key().String())
			if _, exists := result[key]; exists {
				return nil, &KeyCollisionError{Key: key}
			}
			normalized, err := normalize(iterator.Value().Interface())
			if err != nil {
				return nil, err
			}
			result[key] = normalized
		}
		return result, nil
	case reflect.Array, reflect.Slice:
		if reflected.Type().Elem().Kind() == reflect.Uint8 {
			return nil, fmt.Errorf("value of type %T is not JSON-compatible", value)
		}
		result := make([]any, reflected.Len())
		for index := range reflected.Len() {
			normalized, err := normalize(reflected.Index(index).Interface())
			if err != nil {
				return nil, err
			}
			result[index] = normalized
		}
		return result, nil
	default:
		return nil, fmt.Errorf("value of type %T is not JSON-compatible", value)
	}
}
