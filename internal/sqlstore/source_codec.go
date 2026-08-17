package sqlstore

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/thunguo/powercontext-go/source"
)

// SourceCodec is an exact concrete Source type route for the Python storage
// payload. It is immutable after construction.
type SourceCodec struct {
	name      string
	valueType reflect.Type
	encode    func(source.Value) ([]byte, error)
	decode    func([]byte) (source.Value, error)
}

// NewSourceCodec constructs a schema-specific exact-type Source codec.
func NewSourceCodec[S source.Value](
	name string,
	encode func(S) ([]byte, error),
	decode func([]byte) (S, error),
) (SourceCodec, error) {
	if _, err := source.NewRef(name, "codec-validation"); err != nil {
		return SourceCodec{}, err
	}
	valueType := reflect.TypeFor[S]()
	if valueType.Kind() == reflect.Interface {
		return SourceCodec{}, fmt.Errorf("sqlstore: Source codec type must be concrete")
	}
	if encode == nil || decode == nil {
		return SourceCodec{}, fmt.Errorf("sqlstore: Source codec functions must not be nil")
	}
	return SourceCodec{
		name:      name,
		valueType: valueType,
		encode: func(value source.Value) ([]byte, error) {
			if reflect.TypeOf(value) != valueType {
				return nil, fmt.Errorf("sqlstore: Source codec %q expected %s, got %T", name, valueType, value)
			}
			return encode(value.(S))
		},
		decode: func(payload []byte) (source.Value, error) {
			value, err := decode(payload)
			if err != nil {
				return nil, err
			}
			return value, nil
		},
	}, nil
}

// ContentSourceCodec returns the built-in content payload route.
func ContentSourceCodec() SourceCodec {
	codec, err := NewSourceCodec(source.ContentType, encodeContentSource, decodeContentSource)
	if err != nil {
		panic(err)
	}
	return codec
}

type contentSourceJSON struct {
	Name            string                 `json:"name"`
	Materialization source.Materialization `json:"materialization"`
	Description     *string                `json:"description"`
	Content         string                 `json:"content"`
	Metadata        map[string]any         `json:"metadata"`
}

func encodeContentSource(value source.ContentSource) ([]byte, error) {
	description, present := value.SourceDescription()
	var optional *string
	if present {
		optional = &description
	}
	return marshalJSON(contentSourceJSON{
		Name:            value.SourceName(),
		Materialization: value.SourceMaterialization(),
		Description:     optional,
		Content:         value.Content(),
		Metadata:        value.Metadata(),
	})
}

func decodeContentSource(payload []byte) (source.ContentSource, error) {
	var fields map[string]json.RawMessage
	if err := unmarshalJSON(payload, &fields); err != nil {
		return source.ContentSource{}, err
	}
	required := func(name string, destination any) error {
		raw, ok := fields[name]
		if !ok {
			return fmt.Errorf("required field %q is missing", name)
		}
		return unmarshalJSON(raw, destination)
	}
	var name string
	var materialization source.Materialization
	var content string
	if err := required("name", &name); err != nil {
		return source.ContentSource{}, err
	}
	if err := required("materialization", &materialization); err != nil {
		return source.ContentSource{}, err
	}
	if err := required("content", &content); err != nil {
		return source.ContentSource{}, err
	}
	var description *string
	if raw, ok := fields["description"]; ok && string(raw) != "null" {
		var decoded string
		if err := unmarshalJSON(raw, &decoded); err != nil {
			return source.ContentSource{}, err
		}
		description = &decoded
	}
	metadata := map[string]any{}
	if raw, ok := fields["metadata"]; ok {
		if string(raw) == "null" {
			return source.ContentSource{}, fmt.Errorf("metadata must be an object")
		}
		if err := unmarshalJSON(raw, &metadata); err != nil {
			return source.ContentSource{}, err
		}
	}
	return source.RestoreContentSource(name, materialization, description, content, metadata)
}
