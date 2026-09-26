package common

import (
	"encoding/binary"
	"fmt"
	"math"
	"unsafe"
)

const (
	PageSize     int = 4096
	IntSize      int = 8
	StringLength int = 32
)

type Type int8

const (
	// For uninitialized Values
	DefaultType Type = iota
	IntType
	StringType
)

// Size returns the fixed-width storage size of the type in bytes
func (t Type) Size() int {
	switch t {
	case IntType:
		return IntSize
	case StringType:
		return StringLength
	default:
		panic("unknown type")
	}
}

func (t Type) String() string {
	switch t {
	case IntType:
		return "int"
	case StringType:
		return "string"
	}
	return "unknown"
}

// ObjectID is a unique identifier for a table/index/etc. in the database.
type ObjectID uint32

const InvalidObjectID ObjectID = 0

// PageID uniquely identifies a page within the database.
type PageID struct {
	Oid     ObjectID
	PageNum int32
}

// PageIDSize is the serialized size of a PageID (ObjectID (4) + PageNum (4) = 8)
const PageIDSize = 8

func (p *PageID) String() string {
	return fmt.Sprintf("Page(%d, %d)", p.Oid, p.PageNum)
}

// IsNil checks if the PageID is valid.
func (p *PageID) IsNil() bool {
	return p.Oid == 0
}

// WriteTo serializes the PageID into the provided buffer. The buffer must be large enough to hold a PageID.
func (p *PageID) WriteTo(data []byte) {
	if len(data) < PageIDSize {
		panic("buffer too small")
	}
	binary.LittleEndian.PutUint32(data, uint32(p.Oid))
	binary.LittleEndian.PutUint32(data[4:], uint32(p.PageNum))
}

// LoadFrom deserializes a PageID from the provided buffer. The buffer must be large enough to hold a PageID.
func (p *PageID) LoadFrom(data []byte) {
	if len(data) < PageIDSize {
		panic("buffer too small")
	}
	p.Oid = ObjectID(binary.LittleEndian.Uint32(data))
	p.PageNum = int32(binary.LittleEndian.Uint32(data[4:]))
}

// RecordID identifies a specific tuple (row) in the database via its PageID and Slot index.
type RecordID struct {
	PageID
	Slot int32
}

// RecordIDSize is the serialized size of a RecordID (PageID (8) + slot (4) = 12)
const RecordIDSize = 12

// IsNil checks if the RecordID refers to a valid page.
func (r *RecordID) IsNil() bool {
	return r.PageID.IsNil()
}

func (r *RecordID) String() string {
	return fmt.Sprintf("rid(%s, %d)", r.PageID.String(), r.Slot)
}

// WriteTo serializes the RecordID into the provided buffer. The buffer must be large enough to hold a RecordID.
func (r *RecordID) WriteTo(data []byte) {
	if len(data) < RecordIDSize {
		panic("buffer too small")
	}
	r.PageID.WriteTo(data)
	binary.LittleEndian.PutUint32(data[PageIDSize:], uint32(r.Slot))
}

// LoadFrom deserializes a RecordID from the provided buffer. The buffer must be large enough to hold a RecordID.
func (r *RecordID) LoadFrom(data []byte) {
	if len(data) < RecordIDSize {
		panic("buffer too small")
	}
	r.PageID.LoadFrom(data)
	r.Slot = int32(binary.LittleEndian.Uint32(data[PageIDSize:]))
}

type TransactionID uint64

const InvalidTransactionID TransactionID = 0

// Value represents a (deserialized) data item in a tuple.
// It uses specific sentinel values for NULL handling:
// - Int: math.MinInt64 represents NULL.
// - String: A byte of 0xFF at index 0 represents NULL.
type Value struct {
	t                Type
	safeString       bool
	null             bool
	underlyingInt    int64
	underlyingString string
}

// AsValue extracts a value from a raw storage buffer.
//
// SAFETY WARNING: For StringType, this function performs a ZERO-COPY read.
// The resulting Value holds a pointer to the provided `source` slice.
// If `source` is modified (e.g., BufferPool reuse), this Value will become corrupt.
// You must call .Copy() if the Value needs to outlive the buffer lock.
func AsValue(t Type, source []byte) Value {
	val := Value{t: t}
	switch t {
	case IntType:
		val.underlyingInt = int64(binary.LittleEndian.Uint64(source))
		if val.underlyingInt == math.MinInt64 {
			val.null = true
		}
	case StringType:
		if source[0] == 0xFF {
			val.null = true
			val.safeString = true
		} else {
			Assert(len(source) >= StringLength, "string too short")
			realLen := StringLength
			for i := 0; i < StringLength; i++ {
				if source[i] == 0 {
					realLen = i
					break
				}
			}

			// Create the Unsafe String View
			if realLen == 0 {
				val.underlyingString = ""
				val.safeString = true
			} else {
				val.underlyingString = unsafe.String(&source[0], realLen)
				val.safeString = false
			}
		}
	}
	return val
}

// IsNil returns true if the Value is nil and uninitialized. This is NOT to be confused with NULL values.
func (v Value) IsNil() bool {
	return v.t == DefaultType
}

// Copy returns a safe, heap-allocated copy of the value. It decouples the value from the underlying byte buffer.
// You MUST call this if you store the Value beyond the lifetime of a raw Tuple.
func (v Value) Copy() Value {
	if v.t == StringType && !v.null && !v.safeString {
		safeStr := string([]byte(v.underlyingString))
		return Value{
			t:                StringType,
			underlyingString: safeStr,
			null:             false,
			safeString:       true,
		}
	}
	// Integers and Nulls are already safe (passed by value)
	return v
}

// NewIntValue creates a new integer Value.
func NewIntValue(v int64) Value {
	return Value{
		t:             IntType,
		underlyingInt: v,
		null:          false,
	}
}

// NewStringValue creates a new string Value.
func NewStringValue(v string) Value {
	if len(v) > StringLength {
		panic("string too long")
	}
	return Value{
		t:                StringType,
		underlyingString: v,
		null:             false,
		safeString:       true,
	}
}

// NewNullInt creates a NULL integer Value.
func NewNullInt() Value {
	return Value{
		t:    IntType,
		null: true,
	}
}

// NewNullString creates a NULL string Value.
func NewNullString() Value {
	return Value{
		t:          StringType,
		null:       true,
		safeString: true,
	}
}

// Type returns the type of the Value.
func (v Value) Type() Type {
	return v.t
}

// IsNull returns true if the Value is NULL.
func (v Value) IsNull() bool {
	return v.null
}

// IntValue returns the underlying (non-NULL) integer.
func (v Value) IntValue() int64 {
	Assert(v.t == IntType, "type mismatch in IntValue")
	Assert(!v.null, "accessing value of NULL int")
	return v.underlyingInt
}

// StringValue returns the underlying (non-NULL) string.
func (v Value) StringValue() string {
	Assert(v.t == StringType, "type mismatch in StringValue")
	Assert(!v.null, "accessing value of NULL string")
	return v.underlyingString
}

// Increment returns the strict next Value in type-specific ordering.
// It panics if the Value is NULL or cannot be incremented.
func (v Value) Increment() Value {
	Assert(!v.null, "incrementing NULL value")
	switch v.t {
	case IntType:
		Assert(v.underlyingInt < math.MaxInt64, "int overflow in Increment")
		return NewIntValue(v.underlyingInt + 1)
	case StringType:
		return NewStringValue(nextLexString(v.underlyingString))
	default:
		panic("unknown type")
	}
}

// Decrement returns the strict previous Value in type-specific ordering.
// It panics if the Value is NULL or cannot be decremented.
func (v Value) Decrement() Value {
	Assert(!v.null, "decrementing NULL value")
	switch v.t {
	case IntType:
		Assert(v.underlyingInt > math.MinInt64, "int underflow in Decrement")
		return NewIntValue(v.underlyingInt - 1)
	case StringType:
		return NewStringValue(prevLexString(v.underlyingString))
	default:
		panic("unknown type")
	}
}

// SizeInBytes returns the serialization size (fixed width).
func (v Value) SizeInBytes() int {
	return v.t.Size()
}

// WriteTo serializes the Value into storage format.
func (v Value) WriteTo(data []byte) {
	Assert(len(data) >= v.SizeInBytes(), "buffer too small")

	if v.null {
		// Write specific sentinels for disk storage
		switch v.t {
		case IntType:
			binary.LittleEndian.PutUint64(data, 0x8000000000000000)
		case StringType:
			data[0] = 0xFF
			for i := 1; i < StringLength; i++ {
				data[i] = 0
			}
		}
		return
	}

	switch v.t {
	case IntType:
		binary.LittleEndian.PutUint64(data, uint64(v.underlyingInt))
	case StringType:
		n := copy(data, v.underlyingString)
		for i := n; i < StringLength; i++ {
			data[i] = 0
		}
	}
}

// Compare compares two Values.
// Returns -1 if v < other, 0 if v == other, 1 if v > other.
// NULL is considered less than non-NULL values.
func (v Value) Compare(other Value) int {
	Assert(v.t == other.t, "type mismatch in comparison")

	if v.null && other.null {
		return 0
	}
	if v.null {
		return -1
	}
	if other.null {
		return 1
	}

	switch v.t {
	case IntType:
		if v.underlyingInt < other.underlyingInt {
			return -1
		}
		if v.underlyingInt > other.underlyingInt {
			return 1
		}
		return 0
	case StringType:
		if v.underlyingString < other.underlyingString {
			return -1
		}
		if v.underlyingString > other.underlyingString {
			return 1
		}
		return 0
	}
	panic("unreachable")
}

func (v Value) String() string {
	if v.Type() == StringType {
		if v.IsNull() {
			return "NULL(string)"
		}
		return fmt.Sprintf("'%s'", v.StringValue())
	}
	if v.IsNull() {
		return "NULL(int)"
	}
	return fmt.Sprintf("%d", v.IntValue())
}

// nextLexString returns the immediate successor in lexicographic order
// over strings with lengths in [0, StringLength].
func nextLexString(s string) string {
	b := []byte(s)
	Assert(len(b) <= StringLength, "string too long")

	if len(b) < StringLength {
		return string(append(b, 0x00))
	}

	// len == StringLength: increment with carry from the end
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] != 0xFF {
			b[i]++
			for j := i + 1; j < len(b); j++ {
				b[j] = 0x00
			}
			return string(b)
		}
	}

	panic("string overflow in Increment")
}

// prevLexString returns the immediate predecessor in lexicographic order
// over strings with lengths in [0, StringLength].
func prevLexString(s string) string {
	b := []byte(s)
	Assert(len(b) <= StringLength, "string too long")
	Assert(len(b) > 0, "string underflow in Decrement")

	// If the last byte is 0x00, the immediate predecessor is the shorter prefix.
	if b[len(b)-1] == 0x00 {
		return string(b[:len(b)-1])
	}

	// Decrement last byte and maximize the suffix to get the largest smaller string.
	b[len(b)-1]--
	for len(b) < StringLength {
		b = append(b, 0xFF)
	}
	return string(b)
}
