package sqlstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

func marshalJSON(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'}), nil
}

func unmarshalJSON(payload []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func storedBytes(value any, column string) ([]byte, error) {
	switch typed := value.(type) {
	case []byte:
		return bytes.Clone(typed), nil
	default:
		return nil, &InvalidStoredColumnError{Column: column, Expected: "bytes"}
	}
}
