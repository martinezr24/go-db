package transaction

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/puzpuzpuz/xsync/v3"
	"mit.edu/dsg/godb/common"
	"mit.edu/dsg/godb/storage"
)

// activeTxnEntry tracks a running transaction and its starting point in the log.
type activeTxnEntry struct {
	txn      *TransactionContext
	startLsn storage.LSN

	commitLSN   storage.LSN
	hasCommit   bool
	abortLSN    storage.LSN
	hasAbort    bool
	undoCursor  int
	undoStarted bool
}

// TransactionManager is the central component managing the lifecycle of transactions.
// It coordinates with the LockManager for concurrency control and the LogManager for
// Write-Ahead Logging (WAL) and recovery.
type TransactionManager struct {
	// activeTxns maps TransactionIDs to their runtime context and metadata
	activeTxns *xsync.MapOf[common.TransactionID, activeTxnEntry]

	logManager  storage.LogManager
	bufferPool  *storage.BufferPool
	lockManager *LockManager

	nextTxnID atomic.Uint64
	// Pool to recycle transaction contexts
	txnPool sync.Pool

	// snapshotMu guards the ATT snapshot invariant.
	// Commits/Aborts hold RLock across [Append(CommitRecord/AbortRecord) … Delete(tid)].
	// GetActiveTransactionsSnapshot holds Lock for the entire Range() call.
	// This ensures no transaction can have its Commit record at LSN < checkpointLSN
	// while still appearing in the snapshot (un-deleted from activeTxns).
	snapshotMu sync.RWMutex
}

// NewTransactionManager initializes the transaction manager.
func NewTransactionManager(logManager storage.LogManager, bufferPool *storage.BufferPool, lockManager *LockManager) *TransactionManager {
	tm := &TransactionManager{
		activeTxns:  xsync.NewMapOf[common.TransactionID, activeTxnEntry](),
		logManager:  logManager,
		bufferPool:  bufferPool,
		lockManager: lockManager,
	}
	tm.txnPool.New = func() any {
		return &TransactionContext{
			lm:         lockManager,
			logRecords: newLogRecordBuffer(),
			heldLocks:  make(map[DBLockTag]DBLockMode),
		}
	}
	return tm
}

// Begin starts a new transaction and returns the initialized context.
func (tm *TransactionManager) Begin() (*TransactionContext, error) {
	txnID := common.TransactionID(tm.nextTxnID.Add(1))
	txn := tm.txnPool.Get().(*TransactionContext)
	txn.lm = tm.lockManager
	if txn.logRecords == nil {
		txn.logRecords = newLogRecordBuffer()
	}
	if txn.heldLocks == nil {
		txn.heldLocks = make(map[DBLockTag]DBLockMode)
	}
	txn.Reset(txnID)

	record := txn.NewBeginTransactionRecord()
	startLSN, err := tm.logManager.Append(record)
	if err != nil {
		txn.logRecords.pop()
		txn.Reset(0)
		tm.txnPool.Put(txn)
		return nil, err
	}

	tm.snapshotMu.RLock()
	tm.activeTxns.Store(txnID, activeTxnEntry{
		txn:        txn,
		startLsn:   startLSN,
		undoCursor: -1,
	})
	tm.snapshotMu.RUnlock()
	return txn, nil
}

// Commit completes a transaction and makes its effects durable and visible.
//
// TODO(ordering-race): change signature to `(common.LSN, error)` and return
// the commit record's LSN (already captured as `lsn` from logManager.Append
// below). Callers can then resolve per-key WAL ordering after commit using
// the X-lock chain: for two committers on the same key, commit-LSN order
// matches release order matches per-key WAL order. The recovery E2E tests
// (recovery_e2e_test.go) currently have a latent ordering race in their
// live `expected`-map updates because they cannot observe this LSN — see
// the TODO(ordering-race) comments there. Holding back because the change
// touches ~139 call sites, including student-facing code, and these tests
// are already shipped in the autograder.
func (tm *TransactionManager) Commit(txn *TransactionContext) error {
	if txn == nil {
		return fmt.Errorf("cannot commit a nil transaction")
	}
	tm.snapshotMu.RLock()
	defer tm.snapshotMu.RUnlock()

	entry, ok := tm.activeTxns.Load(txn.ID())
	if !ok || entry.txn != txn {
		return fmt.Errorf("transaction %d is not active", txn.ID())
	}

	if !entry.hasCommit {
		record := txn.NewCommitRecord()
		lsn, err := tm.logManager.Append(record)
		if err != nil {
			txn.logRecords.pop()
			return err
		}
		entry.commitLSN = lsn
		entry.hasCommit = true
		tm.activeTxns.Store(txn.ID(), entry)
	}
	if err := tm.logManager.WaitUntilFlushed(entry.commitLSN); err != nil {
		return err
	}

	// Deferred index deletions must run while this transaction still holds its locks.
	for _, task := range txn.commitActions {
		task.Target.Invoke(task.Type, task.Key, task.RID)
	}

	txn.ReleaseAllLocks()
	tm.activeTxns.Delete(txn.ID())
	txn.Reset(0)
	tm.txnPool.Put(txn)
	return nil
}

// Abort stops a transaction and ensures its effects are rolled back
func (tm *TransactionManager) Abort(txn *TransactionContext) error {
	if txn == nil {
		return fmt.Errorf("cannot abort a nil transaction")
	}
	tm.snapshotMu.RLock()
	defer tm.snapshotMu.RUnlock()

	entry, ok := tm.activeTxns.Load(txn.ID())
	if !ok || entry.txn != txn {
		return fmt.Errorf("transaction %d is not active", txn.ID())
	}
	if !entry.undoStarted {
		entry.undoCursor = txn.logRecords.len() - 1
		entry.undoStarted = true
		tm.activeTxns.Store(txn.ID(), entry)
	}

	for entry.undoCursor >= 0 {
		record := txn.logRecords.get(entry.undoCursor)
		switch record.RecordType() {
		case storage.LogInsert, storage.LogUpdate, storage.LogDelete:
			if err := tm.undoRecord(txn, record); err != nil {
				return err
			}
		}
		entry.undoCursor--
		tm.activeTxns.Store(txn.ID(), entry)
	}

	if !entry.hasAbort {
		abortRecord := txn.NewAbortRecord()
		lsn, err := tm.logManager.Append(abortRecord)
		if err != nil {
			txn.logRecords.pop()
			return err
		}
		entry.abortLSN = lsn
		entry.hasAbort = true
		tm.activeTxns.Store(txn.ID(), entry)
	}
	if err := tm.logManager.WaitUntilFlushed(entry.abortLSN); err != nil {
		return err
	}

	// Rollback in-memory index inserts only after the durable abort decision, while
	// the transaction still owns its locks.
	for i := len(txn.abortActions) - 1; i >= 0; i-- {
		task := txn.abortActions[i]
		task.Target.Invoke(task.Type, task.Key, task.RID)
	}

	txn.ReleaseAllLocks()
	tm.activeTxns.Delete(txn.ID())
	txn.Reset(0)
	tm.txnPool.Put(txn)
	return nil
}

func (tm *TransactionManager) undoRecord(txn *TransactionContext, record storage.LogRecord) error {
	rid := record.RID()
	frame, err := tm.bufferPool.GetPage(rid.PageID)
	if err != nil {
		return err
	}

	frame.PageLatch.Lock()
	var clr storage.LogRecord
	switch record.RecordType() {
	case storage.LogInsert:
		clr = txn.NewInsertCLR(record)
	case storage.LogUpdate:
		clr = txn.NewUpdateCLR(record)
	case storage.LogDelete:
		clr = txn.NewDeleteCLR(record)
	default:
		frame.PageLatch.Unlock()
		tm.bufferPool.UnpinPage(frame, false)
		return nil
	}

	lsn, err := tm.logManager.Append(clr)
	if err != nil {
		txn.logRecords.pop()
		frame.PageLatch.Unlock()
		tm.bufferPool.UnpinPage(frame, false)
		return err
	}
	frame.MonotonicallyUpdateLSN(lsn)

	hp := frame.AsHeapPage()
	switch record.RecordType() {
	case storage.LogInsert:
		hp.MarkDeleted(rid, true)
	case storage.LogUpdate:
		copy(hp.AccessTuple(rid), clr.AfterImage())
	case storage.LogDelete:
		hp.MarkDeleted(rid, false)
	}

	frame.PageLatch.Unlock()
	tm.bufferPool.UnpinPage(frame, true)
	return nil
}

// RestartTransactionForRecovery is used during database recovery (ARIES Redo phase).
// It reconstructs a TransactionContext for a transaction that was active at the time of the crash.
//
// Hint: You do not need to worry about this function until lab 4
func (tm *TransactionManager) RestartTransactionForRecovery(txnId common.TransactionID) *TransactionContext {
	panic("unimplemented")
}

// ATTEntry represents a snapshot of an active transaction for the Active Transaction Table (ATT).
type ATTEntry struct {
	ID       common.TransactionID
	StartLSN storage.LSN
}

// GetActiveTransactionsSnapshot returns a snapshot of currently active transaction IDs and their start LSNs.
//
// Hint: You do not need to worry about this function until lab 4
func (tm *TransactionManager) GetActiveTransactionsSnapshot() []ATTEntry {
	panic("unimplemented")
}
