package keeper

import (
	"encoding/json"
)

// jsonValueCodec implements collections.ValueCodec for JSON-serializable types.
type jsonValueCodec[T any] struct{}

func (j jsonValueCodec[T]) Encode(value *T) ([]byte, error) {
	if value == nil {
		return nil, nil
	}
	return json.Marshal(value)
}

func (j jsonValueCodec[T]) Decode(b []byte) (*T, error) {
	if len(b) == 0 {
		return nil, nil
	}
	var value T
	if err := json.Unmarshal(b, &value); err != nil {
		return nil, err
	}
	return &value, nil
}

func (j jsonValueCodec[T]) EncodeJSON(value *T) ([]byte, error) { return j.Encode(value) }

func (j jsonValueCodec[T]) DecodeJSON(b []byte) (*T, error) { return j.Decode(b) }

func (j jsonValueCodec[T]) Stringify(value *T) string {
	if value == nil {
		return "<nil>"
	}
	bytes, err := j.Encode(value)
	if err != nil {
		return "<error>"
	}
	return string(bytes)
}

func (j jsonValueCodec[T]) ValueType() string { return "json" }
