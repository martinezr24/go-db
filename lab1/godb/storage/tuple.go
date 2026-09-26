package storage

import (
	"fmt"

	"mit.edu/dsg/godb/common"
)

// RawTuple represents the "Physical View" of a row.
// It is simply a compact slice of bytes corresponding to the layout on a disk page. It does not know what data it
// contains. You need a RawTupleDesc to read it.
type RawTuple []byte

// RawTupleDesc describes the physical binary layout of a RawTuple.
type RawTupleDesc struct {
	fields      []common.Type
	offsets     []int // Cache of column_id => physical offset of first byte in RawTuple
	bytesPerRow int   // Fixed numBits of the tuple in bytes
}

func (desc *RawTupleDesc) String() string {
	return fmt.Sprintf("%v", desc.fields)
}

// NumColumns returns the number of fields in the physical schema.
func (desc *RawTupleDesc) NumColumns() int {
	return len(desc.fields)
}

// BytesPerTuple returns the fixed numBits in bytes required to store this tuple.
func (desc *RawTupleDesc) BytesPerTuple() int {
	return desc.bytesPerRow
}

// GetFieldType returns the type of the field at index i.
func (desc *RawTupleDesc) GetFieldType(i int) common.Type {
	return desc.fields[i]
}

func (desc *RawTupleDesc) GetFieldTypes() []common.Type {
	return desc.fields
}

// GetFieldOffset returns the byte offset where field i begins.
func (desc *RawTupleDesc) GetFieldOffset(i int) int {
	return desc.offsets[i]
}

// GetValue deserializes the value at index i from the given physical byte slice.
func (desc *RawTupleDesc) GetValue(t RawTuple, i int) common.Value {
	return common.AsValue(desc.fields[i], t[desc.offsets[i]:])
}

// SetValue serializes the value val into the correct position in the physical byte slice t.
func (desc *RawTupleDesc) SetValue(t RawTuple, i int, val common.Value) {
	common.Assert(val.Type() == desc.fields[i], "type mismatch")
	val.WriteTo(t[desc.offsets[i]:])
}

// NewRawTupleDesc creates a descriptor for the given list of field types.
// It calculates offsets and total numBits, ensuring 8-byte alignment for the tuple.
func NewRawTupleDesc(fields []common.Type) *RawTupleDesc {
	size := 0
	offsetOfField := make([]int, len(fields))
	for i := 0; i < len(fields); i++ {
		offsetOfField[i] = size
		switch fields[i] {
		case common.IntType:
			size += common.IntSize
		case common.StringType:
			size += common.StringLength
		default:
			common.Assert(false, "unknown field type")
		}
	}
	// Align to 8 bytes
	common.Assert(common.AlignedTo8(size), "tuple numBits should always be aligned to 8 bytes in our system")
	common.Assert(size <= common.PageSize-32, "tuple numBits should never exceed page numBits")
	return &RawTupleDesc{fields, offsetOfField, size}
}

// Tuple represents the "Logical View" of a row in the database.
// It is the primary data structure exchanged between query operators (e.g., Filter, Join), and bridges the
// storage layer with the rest of GoDB
//
// Note that Tuple is different from RawTuple and is specifically designed to be "higher-level":
//  1. Unified Interface: Query operators (like Aggregates) produce new values that don't exist
//     on disk. Tuple abstracts this, treating physically stored columns and virtual computed
//     columns uniformly via GetValue().
//  3. Self-describing: A tuple knows how to interpret and manipulate its fields, whereas the RawTuple is
//     merely a block of bytes opaque to the logic of the storage engine
//  2. Lazy Deserialization: A Tuple backed by a RawTuple does not immediately convert bytes
//     into Go objects. It remains a lightweight wrapper around the byte slice. Deserialization
//     happens only when a specific field is accessed. In GoDB, because our types are simple, this is
//     not important. However, in a production system where data types can be large and layouts complex,
//     this distinction is often essential for performance.
type Tuple struct {
	// rawTuple holds the "Physical View" (raw bytes) if this tuple is backed by a disk page.
	// If nil, this tuple is purely virtual (e.g., created by an aggregate operator).
	rawTuple RawTuple
	// rawDesc describes the binary schema of rawTuple. It is required to interpret the bytes.
	rawDesc *RawTupleDesc

	// extraValues holds "Virtual Columns" that are not stored physically.
	// These are Go objects created during query execution (e.g., the result of "A + B").
	extraValues []common.Value

	// rid identifies the permanent location of this tuple on disk.
	// It is invalid (nil) for virtual tuples or intermediate results.
	rid common.RecordID
}

// FromRawTuple creates a Tuple backed by physically stored bytes (Zero-Copy).
//
// This is the standard way to "load" a tuple from a HeapPage. It creates a lightweight
// wrapper that points to the copied bytes without interpreting them.
func FromRawTuple(rawTuple RawTuple, desc *RawTupleDesc, rid common.RecordID) Tuple {
	return Tuple{rawTuple: rawTuple, rawDesc: desc, rid: rid}
}

// FromValues creates a purely virtual Tuple from a list of Go values.
// This is used when a query operator creates a brand new row (e.g., "SELECT 1, 'hello'").
func FromValues(values ...common.Value) Tuple {
	return Tuple{
		extraValues: values,
	}
}

var EmptyTuple = FromValues()

// Extend returns a NEW Tuple consisting of the current tuple's fields
// followed by the provided newValues.
func (t Tuple) Extend(newValues []common.Value) Tuple {
	t.extraValues = append(t.extraValues, newValues...)
	return t
}

// IsNil checks if the tuple is uninitialized.
func (t Tuple) IsNil() bool {
	return t.rawDesc == nil && t.extraValues == nil
}

// WriteToBuffer serializes the entire Tuple (Physical + Virtual fields) into a single byte buffer.
//
// This essentially "materializes" a hybrid Tuple into a purely physical RawTuple.
// It is used when we need to write a computed result back to storage (either a table heap or an index)
func (t Tuple) WriteToBuffer(buf []byte, desc *RawTupleDesc) Tuple {
	common.Assert(len(buf) >= desc.BytesPerTuple(), "buffer too small")
	common.Assert(t.NumColumns() == desc.NumColumns(), "tuple descriptor mismatch")

	numPhysicalColumns := 0
	if t.rawDesc != nil {
		numPhysicalColumns = t.rawDesc.NumColumns()
		// Fast-path: direct memcpy
		copy(buf, t.rawTuple)
	}

	for i := numPhysicalColumns; i < desc.NumColumns(); i++ {
		// The value is computed/virtual, so we must serialize it.
		desc.SetValue(buf, i, t.extraValues[i-numPhysicalColumns])
	}
	return FromRawTuple(buf, desc, t.rid)
}

// MergeTuples serializes two tuples (left and right) directly into a single output buffer.
// It assumes the 'desc' describes the combined schema (Left fields followed by Right fields).
// This avoids allocating intermediate structures and is equivalent to calling WriteToBuffer on both.
func MergeTuples(buf []byte, desc *RawTupleDesc, left Tuple, right Tuple) Tuple {
	common.Assert(len(buf) >= desc.BytesPerTuple(), "buffer too small")
	common.Assert(left.NumColumns()+right.NumColumns() == desc.NumColumns(), "tuple descriptor mismatch")

	if left.extraValues == nil && right.extraValues == nil {
		// Fast path -- simply stitch the two tuples together.
		copy(buf, left.rawTuple)
		copy(buf[len(left.rawTuple):], right.rawTuple)
	} else {
		leftNumCols := left.NumColumns()
		rightNumCols := right.NumColumns()
		// Slow path -- serialize the two tuples into the buffer.
		for i := 0; i < leftNumCols; i++ {
			desc.SetValue(buf, i, left.GetValue(i))
		}
		for i := 0; i < rightNumCols; i++ {
			desc.SetValue(buf, leftNumCols+i, right.GetValue(i))
		}
	}
	return FromRawTuple(buf, desc, common.RecordID{})
}

// RID returns the RecordID of the tuple, or an invalid/nil ID if virtual.
func (t Tuple) RID() common.RecordID {
	return t.rid
}

// NumColumns returns the total number of fields (Physical + Virtual) in the tuple.
func (t Tuple) NumColumns() int {
	if t.rawDesc == nil {
		return len(t.extraValues)
	}
	return len(t.extraValues) + t.rawDesc.NumColumns()
}

// GetValue retrieves the value at index i.
func (t Tuple) GetValue(i int) common.Value {
	// Calculate the boundary between Physical and Virtual address space
	physCols := 0
	if t.rawDesc != nil {
		physCols = t.rawDesc.NumColumns()
	}

	// Resolve the "Joint Index"
	// If projectionList is nil, we assume Identity Mapping (Output i -> Joint i)
	jointIdx := i
	// Fetch from Physical
	if jointIdx < physCols {
		return t.rawDesc.GetValue(t.rawTuple, jointIdx)
	}

	// Fetch from Virtual
	// The virtual store starts at indexing 'physCols' in the joint address space
	return t.extraValues[jointIdx-physCols]
}

// DeepCopy creates a fully independent, physically materialized copy of the Tuple.
//
// Note that this would allocate new memory. It is used when the original buffer might be reused, but should not
// be blindly called for performance reasons.
func (t Tuple) DeepCopy(desc *RawTupleDesc) Tuple {
	common.Assert(t.NumColumns() == desc.NumColumns(), "tuple descriptor mismatch")
	dest := make([]byte, desc.BytesPerTuple())
	// Serialize the current tuple into the buffer (writes to physical format)
	// This handles converting any virtual values to their physical representation
	t.WriteToBuffer(dest, desc)
	// Return a new Tuple backed by the physical buffer
	// We preserve the RID of the original tuple, as this is just a copy
	return FromRawTuple(dest, desc, t.rid)
}

// Equals compares this tuple with another tuple for logical equality.
// Two tuples are considered equal if they have the same number of fields
// and all corresponding fields compare equal, regardless of their physical storage format.
func (t Tuple) Equals(other Tuple) bool {
	if t.NumColumns() != other.NumColumns() {
		return false
	}

	for i := 0; i < t.NumColumns(); i++ {
		v1 := t.GetValue(i)
		v2 := other.GetValue(i)

		if v1.Type() != v2.Type() || v1.Compare(v2) != 0 {
			return false
		}
	}

	return true
}
