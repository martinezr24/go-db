package indexing

import (
	"bytes"

	"mit.edu/dsg/godb/common"
	"mit.edu/dsg/godb/storage"
)

// Key represents a search key in an index.
type Key struct {
	storage.RawTuple
	// A key is a slice of a row
	schema *storage.RawTupleDesc
}

// NilKey represents an empty or uninitialized key.
// It is often used to represent open bounds (Infinity) in range scans.
var NilKey = Key{
	RawTuple: storage.RawTuple{},
	schema:   nil,
}

// NewKey creates a new Key object.
// It is required because the 'schema' field is unexported.
func NewKey(tuple storage.RawTuple, schema *storage.RawTupleDesc) Key {
	return Key{
		RawTuple: tuple,
		schema:   schema,
	}
}

// IsNil checks if the key is the NilKey (sentinel value).
func (k Key) IsNil() bool {
	return k.schema == nil
}

// Hash computes a hash value for the key based on its byte content.
// This is used by Hash Indexes.
func (k Key) Hash() uint64 {
	if k.IsNil() {
		return 0
	}
	return common.Hash(k.RawTuple)
}

// Equals checks if two keys are identical.
// Returns true only if they share the same schema and have identical byte content.
func (k Key) Equals(other Key) bool {
	// Only keys with the same schema are comparable on the byte level
	return k.schema == other.schema && bytes.Equal(k.RawTuple, other.RawTuple)
}

// Compare compares this key with another key.
// Returns:
//   - -1 if k < other
//   - 0 if k == other
//   - +1 if k > other
//
// It panics if the keys have different schemas.
func (k Key) Compare(other Key) int {
	common.Assert(k.schema == other.schema, "cannot compare keys of different schemas")

	for i := 0; i < k.schema.NumColumns(); i++ {
		v1 := k.schema.GetValue(k.RawTuple, i)
		v2 := other.schema.GetValue(other.RawTuple, i)

		if cmp := v1.Compare(v2); cmp != 0 {
			return cmp
		}
	}
	return 0
}

// DeepCopy creates a complete copy of the key, including its underlying byte array.
// This is necessary when storing keys in long-lived structures (like B+Tree nodes)
// where the original source byte slice (e.g., from a reusabled buffer in an executor) might be modified or invalidated.
func (k Key) DeepCopy() Key {
	if k.IsNil() {
		return NilKey
	}

	src := k.RawTuple
	// 2. Allocate new memory
	dst := make([]byte, len(src))
	// 3. Copy bytes
	copy(dst, src)

	// We assume storage.NewRowSlice or storage.AsRowSlice exists to wrap bytes
	return Key{
		RawTuple: dst,
		schema:   k.schema,
	}
}
