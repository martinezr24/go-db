package indexing

import (
	"mit.edu/dsg/godb/common"
	"mit.edu/dsg/godb/storage"
	"mit.edu/dsg/godb/transaction"
)

type ScanDirection int

const (
	ScanDirectionForward ScanDirection = iota
	ScanDirectionBackward
)

// IndexMetadata describes the structure of the index and how it relates to the base table.
type IndexMetadata struct {
	// KeySchema describes the types and order of fields that make up the index key.
	KeySchema *storage.RawTupleDesc
	// ProjectionList maps the index key field index to the base table column index.
	// Entry i in ProjectionList means: "The i-th field in the Index Key corresponds to the table column at index ProjectionList[i]".
	ProjectionList []int
}

// KeySize returns the fixed size of the index key in bytes.
func (md *IndexMetadata) KeySize() int {
	return md.KeySchema.BytesPerTuple()
}

// AsKey builds a key from a raw tuple based on the schema size.
// Note: This assumes the `rawTuple` is already formatted or truncated to match the key schema.
func (md *IndexMetadata) AsKey(rawTuple storage.RawTuple) Key {
	return Key{RawTuple: rawTuple[:md.KeySchema.BytesPerTuple()], schema: md.KeySchema}
}

// Index defines the interface for database indexes (e.g., B+Tree, Hash).
// An index maps a Search Key (a projected subset of tuple fields) to one or more RecordIDs (RIDs).
type Index interface {
	// Metadata returns the metadata associated with this index (schema and mapping from base table).
	Metadata() *IndexMetadata

	// InsertEntry adds a mapping from the given key to the specified RecordID.
	// This operation may fail if there is an underlying I/O error or if unique constraints are violated
	// (though GoDB currently does not support constraints).
	InsertEntry(key Key, rid common.RecordID, txn *transaction.TransactionContext) error

	// DeleteEntry removes the mapping between the given key and the specified RecordID.
	DeleteEntry(key Key, rid common.RecordID, txn *transaction.TransactionContext) error

	// ScanKey performs a point lookup. It finds all RecordIDs associated with the exact `key`.
	// The results are appended to the provided `output` slice, which allows the caller
	// to reuse memory and avoid allocations.
	ScanKey(key Key, output []common.RecordID, txn *transaction.TransactionContext) ([]common.RecordID, error)

	// Scan returns an iterator that traverses the index starting from a specific key with the specified direction.
	// Forward Scan:
	//  - Positions the iterator at the first entry where Key >= startingPoint.
	//  - Iterates towards +Infinity.
	//  - If startingPoint is NilKey, starts from the beginning of the index (-Infinity).
	// Backward Scan:
	//  - Positions the iterator at the last entry where Key <= startingPoint.
	//  - Iterates towards -Infinity.
	//  - If startingPoint is NilKey, starts from the end of the index (+Infinity).
	Scan(start Key, direction ScanDirection, txn *transaction.TransactionContext) (ScanIterator, error)
}

// ScanIterator iterates over the results of a range scan.
// It follows the standard Iterator pattern (Init -> Next -> Close).
type ScanIterator interface {
	// Next advances the iterator to the next entry.
	// Returns true if an entry exists, false if the scan is exhausted.
	Next() bool

	// Key returns the current key at the cursor.
	Key() Key

	// Value returns the current value at the cursor.
	Value() common.RecordID

	// Error returns the first unexpected error encountered by the iterator.
	Error() error

	// Close releases any resources (latches, pins) held by the iterator.
	Close() error
}
