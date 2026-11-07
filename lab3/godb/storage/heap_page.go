package storage

import (
	"mit.edu/dsg/godb/common"
)

// HeapPage Layout:
// LSN (8) | RowSize (2) | NumSlots (2) |  NumUsed (2) | Padding (2) | allocation Bitmap | deleted Bitmap | rows
type HeapPage struct {
	*PageFrame
}

func (hp HeapPage) NumUsed() int {
	panic("unimplemented")
}

func (hp HeapPage) setNumUsed(numUsed int) {
	panic("unimplemented")
}

func (hp HeapPage) NumSlots() int {
	panic("unimplemented")
}

func (hp HeapPage) RowSize() int {
	panic("unimplemented")
}

func InitializeHeapPage(desc *RawTupleDesc, frame *PageFrame) {
	panic("unimplemented")
}

func (frame *PageFrame) AsHeapPage() HeapPage {
	panic("unimplemented")
}

func (hp HeapPage) FindFreeSlot() int {
	panic("unimplemented")
}

// IsAllocated checks the allocation bitmap to see if a slot is valid.
func (hp HeapPage) IsAllocated(rid common.RecordID) bool {
	panic("unimplemented")
}

func (hp HeapPage) MarkAllocated(rid common.RecordID, allocated bool) {
	panic("unimplemented")
}

func (hp HeapPage) IsDeleted(rid common.RecordID) bool {
	panic("unimplemented")
}

func (hp HeapPage) MarkDeleted(rid common.RecordID, deleted bool) {
	panic("unimplemented")
}

func (hp HeapPage) AccessTuple(rid common.RecordID) RawTuple {
	panic("unimplemented")
}
