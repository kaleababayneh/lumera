// Package types holds shared types and helpers for the registry module.
//
//revive:disable:var-naming // Cosmos module conventions use the `types` package name.
package types

import (
	"fmt"
	"reflect"
	"sync"

	"github.com/cosmos/gogoproto/proto"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// InMemoryParamSubspace provides an in-memory implementation of ParamSubspace for tests.
type InMemoryParamSubspace struct {
	mu       sync.RWMutex
	keyTable KeyTable
	values   map[string]interface{}
}

// NewInMemoryParamSubspace constructs an empty in-memory subspace.
func NewInMemoryParamSubspace() *InMemoryParamSubspace {
	return &InMemoryParamSubspace{values: make(map[string]interface{})}
}

// WithKeyTable sets the key table and returns the updated subspace.
func (s *InMemoryParamSubspace) WithKeyTable(table KeyTable) ParamSubspace {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keyTable = table
	if s.values == nil {
		s.values = make(map[string]interface{})
	}
	return s
}

// HasKeyTable reports whether a key table has been configured.
func (s *InMemoryParamSubspace) HasKeyTable() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.keyTable.pairs) > 0
}

// Get loads a parameter value into the provided pointer if present.
func (s *InMemoryParamSubspace) Get(_ sdk.Context, key []byte, ptr interface{}) {
	s.mu.RLock()
	value, ok := s.values[string(key)]
	s.mu.RUnlock()
	if !ok {
		return
	}
	s.assign(ptr, value)
}

// Has reports whether a parameter key is present in the subspace.
func (s *InMemoryParamSubspace) Has(_ sdk.Context, key []byte) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.values[string(key)]
	return ok
}

// Set stores the provided parameter value under the given key.
func (s *InMemoryParamSubspace) Set(_ sdk.Context, key []byte, param interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[string(key)] = s.clone(param)
}

// GetParamSet populates the provided ParamSet from stored values if present.
func (s *InMemoryParamSubspace) GetParamSet(_ sdk.Context, ps ParamSet) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, pair := range ps.ParamSetPairs() {
		value, ok := s.values[string(pair.Key)]
		if !ok {
			continue
		}
		s.assign(pair.Value, value)
	}
}

// SetParamSet writes each parameter from the provided ParamSet into the subspace.
func (s *InMemoryParamSubspace) SetParamSet(_ sdk.Context, ps ParamSet) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, pair := range ps.ParamSetPairs() {
		val := reflect.ValueOf(pair.Value)
		if val.Kind() != reflect.Pointer {
			panic("param set values must be pointers")
		}
		actual := val.Elem().Interface()
		if pair.ValidatorFn != nil {
			if err := pair.ValidatorFn(actual); err != nil {
				panic(fmt.Sprintf("invalid param %s: %v", string(pair.Key), err))
			}
		}
		s.values[string(pair.Key)] = s.clone(actual)
	}
}

func (s *InMemoryParamSubspace) assign(target interface{}, value interface{}) {
	dst := reflect.ValueOf(target)
	if dst.Kind() != reflect.Pointer {
		panic("param destination must be pointer")
	}
	cloned := reflect.ValueOf(s.clone(value))
	if !cloned.IsValid() {
		return
	}
	if !cloned.Type().AssignableTo(dst.Elem().Type()) {
		if cloned.Type().ConvertibleTo(dst.Elem().Type()) {
			dst.Elem().Set(cloned.Convert(dst.Elem().Type()))
			return
		}
		panic(fmt.Sprintf("cannot assign %T to %s", value, dst.Elem().Type()))
	}
	dst.Elem().Set(cloned)
}

func (s *InMemoryParamSubspace) clone(value interface{}) interface{} {
	if value == nil {
		return nil
	}
	if msg, ok := value.(proto.Message); ok {
		return proto.Clone(msg)
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Pointer:
		if rv.IsNil() {
			return reflect.Zero(rv.Type()).Interface()
		}
		cloned := reflect.New(rv.Elem().Type())
		cloned.Elem().Set(reflect.ValueOf(s.clone(rv.Elem().Interface())))
		return cloned.Interface()
	case reflect.Slice:
		if rv.IsNil() {
			return reflect.Zero(rv.Type()).Interface()
		}
		cloned := reflect.MakeSlice(rv.Type(), rv.Len(), rv.Len())
		for i := 0; i < rv.Len(); i++ {
			cloned.Index(i).Set(reflect.ValueOf(s.clone(rv.Index(i).Interface())))
		}
		return cloned.Interface()
	case reflect.Map:
		if rv.IsNil() {
			return reflect.Zero(rv.Type()).Interface()
		}
		cloned := reflect.MakeMapWithSize(rv.Type(), rv.Len())
		for _, key := range rv.MapKeys() {
			cloned.SetMapIndex(key, reflect.ValueOf(s.clone(rv.MapIndex(key).Interface())))
		}
		return cloned.Interface()
	default:
		return value
	}
}
