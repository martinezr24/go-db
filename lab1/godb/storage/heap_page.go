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
	low := uint16(hp.Bytes[12])
	high := uint16(hp.Bytes[13]) << 8
	return int(low | high)
}

func (hp HeapPage) setNumUsed(numUsed int) {
	setAmount := uint16(numUsed)

	low := byte(setAmount)
	high := byte(setAmount >> 8)

	hp.Bytes[12] = low
	hp.Bytes[13] = high
}

func (hp HeapPage) NumSlots() int {
	low := uint16(hp.Bytes[10])
	high := uint16(hp.Bytes[11]) << 8
	return int(low | high)
}

func (hp HeapPage) RowSize() int {
	low := uint16(hp.Bytes[8])
	high := uint16(hp.Bytes[9]) << 8
	return int(low | high)
}

func InitializeHeapPage(desc *RawTupleDesc, frame *PageFrame) {
	frame.Bytes = [common.PageSize]byte{}

	hp := HeapPage{PageFrame: frame}

	tupleSize := desc.BytesPerTuple()

	common.Assert(tupleSize > 0, "tuple size has to be positive")

	numSlots := (common.PageSize - 16) / tupleSize

	for {
		bitmapBytes := ((numSlots + 63) / 64) * 8

		totalBytes := 16 + 2*bitmapBytes + numSlots*tupleSize

		if totalBytes <= common.PageSize {
			break
		}

		numSlots--
	}

	hp.Bytes[8] = byte(tupleSize)
	hp.Bytes[9] = byte(tupleSize >> 8)

	hp.Bytes[10] = byte(numSlots)
	hp.Bytes[11] = byte(numSlots >> 8)

	hp.setNumUsed(0)
}

func (frame *PageFrame) AsHeapPage() HeapPage {
	return HeapPage{PageFrame: frame}
}

func (hp HeapPage) FindFreeSlot() int {
	if hp.PageFrame == nil {
		return -1
	}

	numSlots := hp.NumSlots()
	if numSlots <= 0 {
		return -1
	}

	bmBytes := ((numSlots + 63) / 64) * 8
	allocation := AsBitmap(hp.Bytes[16:16+bmBytes], numSlots)

	return allocation.FindFirstZero(0)
}

// IsAllocated checks the allocation bitmap to see if a slot is valid.
func (hp HeapPage) IsAllocated(rid common.RecordID) bool {
	if hp.PageFrame == nil {
		return false
	}

	slot := int(rid.Slot)
	numSlots := hp.NumSlots()
	if slot < 0 || slot >= numSlots || numSlots <= 0 {
		return false
	}

	bmBytes := ((numSlots + 63) / 64) * 8
	allocation := AsBitmap(hp.Bytes[16:16+bmBytes], numSlots)

	return allocation.LoadBit(slot)
}

func (hp HeapPage) MarkAllocated(rid common.RecordID, allocated bool) {
	slot := int(rid.Slot)
	numSlots := hp.NumSlots()
	if hp.PageFrame == nil || slot < 0 || slot >= numSlots || numSlots <= 0 {
		return
	}

	bmBytes := ((numSlots + 63) / 64) * 8
	allocation := AsBitmap(hp.Bytes[16:16+bmBytes], numSlots)

	wasAllocated := allocation.SetBit(slot, allocated)

	if wasAllocated != allocated {
		if allocated {
			hp.setNumUsed(hp.NumUsed() + 1)
		} else {
			hp.setNumUsed(hp.NumUsed() - 1)

			deletedStart := 16 + bmBytes
			deleted := AsBitmap(hp.Bytes[deletedStart:deletedStart+bmBytes], numSlots)

			deleted.SetBit(slot, false)
		}
	}
}

func (hp HeapPage) IsDeleted(rid common.RecordID) bool {
	if hp.PageFrame == nil {
		return false
	}

	slot := int(rid.Slot)
	numSlots := hp.NumSlots()
	if slot < 0 || slot >= numSlots || numSlots <= 0 {
		return false
	}

	bmBytes := ((numSlots + 63) / 64) * 8
	deletedStart := 16 + bmBytes
	deletedBM := AsBitmap(hp.Bytes[deletedStart:deletedStart+bmBytes], numSlots)

	return deletedBM.LoadBit(slot)
}

func (hp HeapPage) MarkDeleted(rid common.RecordID, deleted bool) {
	slot := int(rid.Slot)
	numSlots := hp.NumSlots()
	if hp.PageFrame == nil || slot < 0 || slot >= numSlots || numSlots <= 0 {
		return
	}

	bmBytes := ((numSlots + 63) / 64) * 8
	deletedStart := 16 + bmBytes
	deletedBM := AsBitmap(hp.Bytes[deletedStart:deletedStart+bmBytes], numSlots)

	deletedBM.SetBit(slot, deleted)
}

func (hp HeapPage) AccessTuple(rid common.RecordID) RawTuple {
	if hp.PageFrame == nil {
		return nil
	}

	slot := int(rid.Slot)
	numSlots := hp.NumSlots()
	if slot < 0 || slot >= numSlots || numSlots <= 0 {
		return nil
	}

	bmBytes := ((numSlots + 63) / 64) * 8
	rowsStart := 16 + (2 * bmBytes)
	start := rowsStart + slot*hp.RowSize()
	if start < 0 || start+hp.RowSize() > len(hp.Bytes) {
		return nil
	}

	return RawTuple(hp.Bytes[start : start+hp.RowSize()])
}
