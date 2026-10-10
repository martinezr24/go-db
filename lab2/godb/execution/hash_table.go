package execution

import (
	"unsafe"

	"mit.edu/dsg/godb/common"
	"mit.edu/dsg/godb/storage"
)

// ExecutionHashTable is a generic, high-performance wrapper around a Go map.
// It is optimized for single-threaded execution operators (Aggregates, Hash Joins).
type ExecutionHashTable[T any] struct {
	// The map key is a string view of the tuple's raw bytes. Go does not support byte arrays for keys
	table     map[string]T
	keySchema *storage.RawTupleDesc

	// scratchBuffer is a reusable byte slice for serializing keys during lookups
	// to avoid allocation when the key data is already available in a Tuple.
	scratchBuffer []byte
}

func NewExecutionHashTable[T any](keySchema *storage.RawTupleDesc) *ExecutionHashTable[T] {
	return &ExecutionHashTable[T]{
		table:         make(map[string]T),
		keySchema:     keySchema,
		scratchBuffer: make([]byte, keySchema.BytesPerTuple()),
	}
}

// Insert adds a value to the hash table.
// This handles the necessary allocation to persist the key.
func (ht *ExecutionHashTable[T]) Insert(key storage.Tuple, value T) {
	key.WriteToBuffer(ht.scratchBuffer, ht.keySchema)

	// Standard conversion: allocates memory and copies bytes.
	// We need do this anyway because the map needs to own the key string,
	// and scratchBuffer will be overwritten.
	keyStr := string(ht.scratchBuffer)
	ht.table[keyStr] = value
}

// Get returns the slice of values matching the key.
// This uses unsafe string casting to avoid allocations during lookup.
func (ht *ExecutionHashTable[T]) Get(key storage.Tuple) (value T, exists bool) {
	key.WriteToBuffer(ht.scratchBuffer, ht.keySchema)
	// Go should automatically optimize and avoid a heap allocation here
	value, exists = ht.table[string(ht.scratchBuffer)]
	return
}

// Delete removes the key-value pair from the hash table.
func (ht *ExecutionHashTable[T]) Delete(key storage.Tuple) {
	key.WriteToBuffer(ht.scratchBuffer, ht.keySchema)
	// Go should automatically optimize and avoid a heap allocation here
	delete(ht.table, string(ht.scratchBuffer))
}

// Iterate loops over all key-value pairs in the hash table and calls the provided callback function for each.
func (ht *ExecutionHashTable[T]) Iterate(iter func(key storage.Tuple, value T)) {
	for key, value := range ht.table {
		// This usage here is fine because we do not modify the "physical" part of a tuple, making it essentially
		// a read-only view
		tuple := storage.FromRawTuple(unsafe.Slice(unsafe.StringData(key), len(key)), ht.keySchema, common.RecordID{})
		iter(tuple, value)
	}
}
