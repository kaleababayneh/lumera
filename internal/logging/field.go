// Package logging provides colorized, structured logging utilities for Lumera services.
package logging

import (
	"fmt"
	"time"
)

// Field represents a structured log field with a key-value pair.
type Field struct {
	Key   string
	Value any
}

// String creates a string-typed log field.
func String(key, value string) Field {
	return Field{Key: key, Value: value}
}

// Int creates an integer-typed log field.
func Int(key string, value int) Field {
	return Field{Key: key, Value: value}
}

// Int64 creates an int64-typed log field.
func Int64(key string, value int64) Field {
	return Field{Key: key, Value: value}
}

// Float64 creates a float64-typed log field.
func Float64(key string, value float64) Field {
	return Field{Key: key, Value: value}
}

// Bool creates a boolean-typed log field.
func Bool(key string, value bool) Field {
	return Field{Key: key, Value: value}
}

// Duration creates a duration log field, stored as milliseconds.
// The key is automatically suffixed with "_ms" for clarity.
func Duration(key string, value time.Duration) Field {
	return Field{Key: key + "_ms", Value: value.Milliseconds()}
}

// Error creates an error log field with key "error".
func Error(err error) Field {
	if err == nil {
		return Field{Key: "error", Value: nil}
	}
	return Field{Key: "error", Value: err.Error()}
}

// ErrorKey creates an error log field with a custom key.
func ErrorKey(key string, err error) Field {
	if err == nil {
		return Field{Key: key, Value: nil}
	}
	return Field{Key: key, Value: err.Error()}
}

// Any creates a log field with any value type.
func Any(key string, value any) Field {
	return Field{Key: key, Value: value}
}

// Stringer creates a string field from a fmt.Stringer interface.
func Stringer(key string, value fmt.Stringer) Field {
	if value == nil {
		return Field{Key: key, Value: nil}
	}
	return Field{Key: key, Value: value.String()}
}

// Time creates a time field formatted as RFC3339Nano.
func Time(key string, value time.Time) Field {
	return Field{Key: key, Value: value.Format(time.RFC3339Nano)}
}

// Strings creates a string slice field.
func Strings(key string, values []string) Field {
	return Field{Key: key, Value: values}
}

// fieldsToMap converts a slice of fields to a map for JSON serialization.
func fieldsToMap(fields []Field) map[string]any {
	m := make(map[string]any, len(fields))
	for _, f := range fields {
		if f.Value != nil {
			m[f.Key] = f.Value
		}
	}
	return m
}

// mergeFields merges base fields with additional fields, with additional fields taking precedence.
func mergeFields(base, additional []Field) []Field {
	if len(additional) == 0 {
		return base
	}
	if len(base) == 0 {
		return additional
	}
	// Create a map for quick lookup of additional keys
	additionalKeys := make(map[string]struct{}, len(additional))
	for _, f := range additional {
		additionalKeys[f.Key] = struct{}{}
	}
	// Filter base fields that aren't overridden
	result := make([]Field, 0, len(base)+len(additional))
	for _, f := range base {
		if _, exists := additionalKeys[f.Key]; !exists {
			result = append(result, f)
		}
	}
	// Append all additional fields
	result = append(result, additional...)
	return result
}
