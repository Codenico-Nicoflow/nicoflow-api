// Package optional provides a JSON field wrapper that distinguishes three PATCH
// states: absent (leave unchanged), explicit null (clear), and a value (set).
// A plain *T collapses absent and null into nil, which makes "clear this field"
// impossible to express on a partial update.
package optional

import (
	"bytes"
	"encoding/json"
)

// Field is a nullable, presence-aware JSON value. Set reports whether the key
// appeared in the payload at all; Value holds the decoded value (nil when the
// key was present but JSON null).
type Field[T any] struct {
	Set   bool
	Value *T
}

// UnmarshalJSON records that the key was present and decodes null vs value.
func (f *Field[T]) UnmarshalJSON(data []byte) error {
	f.Set = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		f.Value = nil
		return nil
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	f.Value = &v
	return nil
}

// Get returns the decoded value and whether a non-null value is present.
func (f Field[T]) Get() (T, bool) {
	if f.Value == nil {
		var zero T
		return zero, false
	}
	return *f.Value, true
}
