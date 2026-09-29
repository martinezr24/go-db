package execution

import (
	"errors"
	"sync"

	"mit.edu/dsg/godb/catalog"
	"mit.edu/dsg/godb/common"
	"mit.edu/dsg/godb/storage"
	"mit.edu/dsg/godb/transaction"
)

// TableHeap represents a physical table stored as a heap file on disk.
// It handles the insertion, update, deletion, and reading of tuples, managing
// interactions with the BufferPool, LockManager, and LogManager.
type TableHeap struct {
	oid         common.ObjectID
	desc        *storage.RawTupleDesc
	bufferPool  *storage.BufferPool
	logManager  storage.LogManager
	lockManager *transaction.LockManager
	insertMutex sync.Mutex
}

// NewTableHeap creates a TableHeap and performs a metadata scan to initialize stats.
func NewTableHeap(table *catalog.Table, bufferPool *storage.BufferPool, logManager storage.LogManager, lockManager *transaction.LockManager) (*TableHeap, error) {
	fieldTypes := make([]common.Type, len(table.Columns))
	for i, column := range table.Columns {
		fieldTypes[i] = column.Type
	}

	th := &TableHeap{
		oid:         table.Oid,
		desc:        storage.NewRawTupleDesc(fieldTypes),
		bufferPool:  bufferPool,
		logManager:  logManager,
		lockManager: lockManager,
	}
	return th, nil
}

// StorageSchema returns the physical byte-layout descriptor of the tuples in this table.
func (tableHeap *TableHeap) StorageSchema() *storage.RawTupleDesc {
	return tableHeap.desc
}

// InsertTuple inserts a tuple into the TableHeap. It should find a free space, allocating if needed, and return the found slot.
func (tableHeap *TableHeap) InsertTuple(txn *transaction.TransactionContext, row storage.RawTuple) (common.RecordID, error) {
	tableHeap.insertMutex.Lock()

	file, err := tableHeap.bufferPool.StorageManager().GetDBFile(tableHeap.oid)
	if err != nil {
		tableHeap.insertMutex.Unlock()
		return common.RecordID{}, err
	}

	numPages, err := file.NumPages()
	if err != nil {
		tableHeap.insertMutex.Unlock()
		return common.RecordID{}, err
	}

	if numPages > 0 {
		pageID := common.PageID{
			Oid:     tableHeap.oid,
			PageNum: int32(numPages - 1),
		}

		frame, err := tableHeap.bufferPool.GetPage(pageID)
		if err != nil {
			tableHeap.insertMutex.Unlock()
			return common.RecordID{}, err
		}

		frame.PageLatch.Lock()
		hp := frame.AsHeapPage()
		slot := hp.FindFreeSlot()

		if slot >= 0 {
			rid := common.RecordID{PageID: pageID, Slot: int32(slot)}
			hp.MarkAllocated(rid, true)
			copy(hp.AccessTuple(rid), row)

			frame.PageLatch.Unlock()
			tableHeap.bufferPool.UnpinPage(frame, true)
			tableHeap.insertMutex.Unlock()
			return rid, nil
		}

		frame.PageLatch.Unlock()
		tableHeap.bufferPool.UnpinPage(frame, false)
	}

	pageNum, err := file.AllocatePage(1)
	if err != nil {
		tableHeap.insertMutex.Unlock()
		return common.RecordID{}, err
	}

	pageID := common.PageID{
		Oid:     tableHeap.oid,
		PageNum: int32(pageNum),
	}

	frame, err := tableHeap.bufferPool.GetPage(pageID)
	if err != nil {
		tableHeap.insertMutex.Unlock()
		return common.RecordID{}, err
	}

	frame.PageLatch.Lock()
	storage.InitializeHeapPage(tableHeap.desc, frame)

	hp := frame.AsHeapPage()
	rid := common.RecordID{PageID: pageID, Slot: 0}
	hp.MarkAllocated(rid, true)
	copy(hp.AccessTuple(rid), row)

	frame.PageLatch.Unlock()
	tableHeap.bufferPool.UnpinPage(frame, true)
	tableHeap.insertMutex.Unlock()
	return rid, nil
}

var ErrTupleDeleted = errors.New("tuple has been deleted")

// DeleteTuple marks a tuple as deleted in the TableHeap. If the tuple has been deleted, return ErrTupleDeleted
func (tableHeap *TableHeap) DeleteTuple(txn *transaction.TransactionContext, rid common.RecordID) error {
	frame, err := tableHeap.bufferPool.GetPage(rid.PageID)
	if err != nil {
		return err
	}

	frame.PageLatch.Lock()
	hp := frame.AsHeapPage()

	if !hp.IsAllocated(rid) || hp.IsDeleted(rid) {
		frame.PageLatch.Unlock()
		tableHeap.bufferPool.UnpinPage(frame, false)
		return ErrTupleDeleted
	}

	hp.MarkDeleted(rid, true)

	frame.PageLatch.Unlock()
	tableHeap.bufferPool.UnpinPage(frame, true)
	return nil
}

// ReadTuple reads the physical bytes of a tuple into the provided buffer. If forUpdate is true, read should acquire
// exclusive lock instead of shared. If the tuple has been deleted, return ErrTupleDeleted
func (tableHeap *TableHeap) ReadTuple(txn *transaction.TransactionContext, rid common.RecordID, buffer []byte, forUpdate bool) error {
	frame, err := tableHeap.bufferPool.GetPage(rid.PageID)
	if err != nil {
		return err
	}

	frame.PageLatch.RLock()
	hp := frame.AsHeapPage()

	if !hp.IsAllocated(rid) || hp.IsDeleted(rid) {
		frame.PageLatch.RUnlock()
		tableHeap.bufferPool.UnpinPage(frame, false)
		return ErrTupleDeleted
	}

	copy(buffer, hp.AccessTuple(rid))

	frame.PageLatch.RUnlock()
	tableHeap.bufferPool.UnpinPage(frame, false)
	return nil
}

// UpdateTuple updates a tuple in-place with new binary data. If the tuple has been deleted, return ErrTupleDeleted.
func (tableHeap *TableHeap) UpdateTuple(txn *transaction.TransactionContext, rid common.RecordID, updatedTuple storage.RawTuple) error {
	frame, err := tableHeap.bufferPool.GetPage(rid.PageID)
	if err != nil {
		return err
	}

	frame.PageLatch.Lock()
	hp := frame.AsHeapPage()

	if !hp.IsAllocated(rid) || hp.IsDeleted(rid) {
		frame.PageLatch.Unlock()
		tableHeap.bufferPool.UnpinPage(frame, false)
		return ErrTupleDeleted
	}

	copy(hp.AccessTuple(rid), updatedTuple)

	frame.PageLatch.Unlock()
	tableHeap.bufferPool.UnpinPage(frame, true)
	return nil
}

// VacuumPage attempts to clean up deleted slots on a specific page.
// If slots are deleted AND no transaction holds a lock on them, they are marked as free.
// This is used to reclaim space in the background.
func (tableHeap *TableHeap) VacuumPage(pageID common.PageID) error {
	frame, err := tableHeap.bufferPool.GetPage(pageID)
	if err != nil {
		return err
	}

	frame.PageLatch.Lock()
	hp := frame.AsHeapPage()
	changed := false

	for slot := 0; slot < hp.NumSlots(); slot++ {
		rid := common.RecordID{
			PageID: pageID,
			Slot:   int32(slot),
		}

		if hp.IsAllocated(rid) && hp.IsDeleted(rid) {
			hp.MarkAllocated(rid, false)
			changed = true
		}
	}

	frame.PageLatch.Unlock()
	tableHeap.bufferPool.UnpinPage(frame, changed)
	return nil
}

// Iterator creates a new TableHeapIterator to scan the table. It acquires the supplied lock on the table (S, X, or SIX),
// and uses the supplied byte slice to fetch tuples in the returned iterator (for zero-allocation scanning).
func (tableHeap *TableHeap) Iterator(txn *transaction.TransactionContext, mode transaction.DBLockMode, buffer []byte) (TableHeapIterator, error) {
	file, err := tableHeap.bufferPool.StorageManager().GetDBFile(tableHeap.oid)
	if err != nil {
		return TableHeapIterator{}, err
	}

	numPages, err := file.NumPages()
	if err != nil {
		return TableHeapIterator{}, err
	}

	if len(buffer) < tableHeap.desc.BytesPerTuple() {
		return TableHeapIterator{}, errors.New("iterator buffer is too small")
	}

	return TableHeapIterator{
		tableHeap: tableHeap,
		numPages:  numPages,
		pageNum:   0,
		slot:      -1,
		buffer:    buffer,
	}, nil
}

// TableHeapIterator iterates over all valid (allocated and non-deleted) tuples in the heap.
type TableHeapIterator struct {
	tableHeap *TableHeap
	numPages  int
	pageNum   int
	slot      int
	frame     *storage.PageFrame
	curRid    common.RecordID
	curTuple  storage.RawTuple
	buffer    []byte
	err       error
	closed    bool
}

// IsNil returns true if the TableHeapIterator is the default, uninitialized value
func (it *TableHeapIterator) IsNil() bool {
	return it == nil || it.tableHeap == nil
}

// Next advances the iterator to the next valid tuple.
// It manages page pins automatically (unpinning the old page when moving to a new one).
func (it *TableHeapIterator) Next() bool {
	if it.IsNil() || it.closed {
		return false
	}

	it.curTuple = nil

	for it.pageNum < it.numPages {
		if it.frame == nil {
			pageID := common.PageID{Oid: it.tableHeap.oid, PageNum: int32(it.pageNum)}

			frame, err := it.tableHeap.bufferPool.GetPage(pageID)
			if err != nil {
				it.err = err
				return false
			}

			it.frame = frame
			it.slot = -1
		}

		it.frame.PageLatch.RLock()
		hp := it.frame.AsHeapPage()

		for it.slot++; it.slot < hp.NumSlots(); it.slot++ {
			rid := common.RecordID{PageID: common.PageID{Oid: it.tableHeap.oid, PageNum: int32(it.pageNum)}, Slot: int32(it.slot)}

			if hp.IsAllocated(rid) && !hp.IsDeleted(rid) {
				rowSize := it.tableHeap.desc.BytesPerTuple()
				copy(it.buffer[:rowSize], hp.AccessTuple(rid))
				it.curRid = rid
				it.curTuple = storage.RawTuple(it.buffer[:rowSize])

				it.frame.PageLatch.RUnlock()
				return true
			}
		}

		it.frame.PageLatch.RUnlock()
		it.tableHeap.bufferPool.UnpinPage(it.frame, false)
		it.frame = nil
		it.pageNum++
		it.slot = -1
	}

	return false
}

// CurrentTuple returns the raw bytes of the tuple at the current cursor position.
// The bytes are valid only until Next() is called again.
func (it *TableHeapIterator) CurrentTuple() storage.RawTuple {
	return it.curTuple
}

// CurrentRID returns the RecordID of the current tuple.
func (it *TableHeapIterator) CurrentRID() common.RecordID {
	return it.curRid
}

// CurrentRID returns the first error encountered during iteration, if any.
func (it *TableHeapIterator) Error() error {
	if it == nil {
		return nil
	}
	return it.err
}

// Close releases any resources associated with the TableHeapIterator
func (it *TableHeapIterator) Close() error {
	if it == nil || it.closed {
		return nil
	}

	if it.frame != nil {
		it.tableHeap.bufferPool.UnpinPage(it.frame, false)
		it.frame = nil
	}

	it.curTuple = nil
	it.closed = true
	return nil
}
