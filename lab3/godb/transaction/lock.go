package transaction

import (
	"fmt"
	"sync"

	"mit.edu/dsg/godb/common"
)

type lockState struct {
	holders map[common.TransactionID]DBLockMode
	waiters []lockRequest
	changed *sync.Cond
}

type lockRequest struct {
	tid  common.TransactionID
	tag  DBLockTag
	mode DBLockMode
}

// DBLockTag identifies a unique resource (Table or Tuple). It represents Tuple if it has a full RecordID, and
// represents a table if only the oid is set and the rest are set to -1
type DBLockTag struct {
	common.RecordID
}

// NewTableLockTag creates a DBLockTag representing a whole table.
func NewTableLockTag(oid common.ObjectID) DBLockTag {
	return DBLockTag{
		RecordID: common.RecordID{
			PageID: common.PageID{
				Oid:     oid,
				PageNum: -1,
			},
			Slot: -1,
		},
	}
}

// NewTupleLockTag creates a DBLockTag representing a specific tuple (row).
func NewTupleLockTag(rid common.RecordID) DBLockTag {
	return DBLockTag{
		RecordID: rid,
	}
}

func (t DBLockTag) String() string {
	if t.PageNum == -1 {
		return fmt.Sprintf("Table(%d)", t.Oid)
	}
	return fmt.Sprintf("Tuple(%d, %d, %d)", t.Oid, t.PageNum, t.Slot)
}

// DBLockMode represents the type of access a transaction is requesting.
// GoDB supports a standard Multi-Granularity Locking hierarchy.
type DBLockMode int

const (
	// LockModeS (Shared) allows reading a resource. Multiple transactions can hold S locks simultaneously.
	LockModeS DBLockMode = iota
	// LockModeX (Exclusive) allows modification. It is incompatible with all other modes.
	LockModeX
	// LockModeIS (Intent Shared) indicates the intention to read resources at a lower level (e.g., locking a table IS to read tuples).
	LockModeIS
	// LockModeIX (Intent Exclusive) indicates the intention to modify resources at a lower level (e.g., locking a table IX to modify tuples).
	LockModeIX
	// LockModeSIX (Shared Intent Exclusive) allows reading the resource (like S) AND the intention to modify lower-level resources (like IX).
	LockModeSIX
)

func (m DBLockMode) String() string {
	switch m {
	case LockModeS:
		return "LockModeS"
	case LockModeX:
		return "LockModeX"
	case LockModeIS:
		return "LockModeIS"
	case LockModeIX:
		return "LockModeIX"
	case LockModeSIX:
		return "LockModeSIX"
	}
	return "Unknown lock mode"
}

// LockManager manages the granting, releasing, and waiting of locks on database resources.
type LockManager struct {
	mu    sync.Mutex
	locks map[DBLockTag]*lockState
}

// NewLockManager initializes a new LockManager.
func NewLockManager() *LockManager {
	lm := &LockManager{
		locks: make(map[DBLockTag]*lockState),
	}
	return lm
}

func (lm *LockManager) newLockState() *lockState {
	return &lockState{
		holders: make(map[common.TransactionID]DBLockMode),
		changed: sync.NewCond(&lm.mu),
	}
}

func compatible(a, b DBLockMode) bool {
	switch a {
	case LockModeS:
		return b == LockModeS || b == LockModeIS
	case LockModeX:
		return false
	case LockModeIS:
		return b != LockModeX
	case LockModeIX:
		return b == LockModeIS || b == LockModeIX
	case LockModeSIX:
		return b == LockModeIS
	default:
		return false
	}
}

func canGrant(state *lockState, tid common.TransactionID, requested DBLockMode) bool {
	for holderID, heldMode := range state.holders {
		if holderID == tid {
			continue
		}
		if !compatible(requested, heldMode) {
			return false
		}
	}
	return true
}

func (lm *LockManager) canGrantQueued(state *lockState, index int) bool {
	request := state.waiters[index]
	if !canGrant(state, request.tid, request.mode) {
		return false
	}
	for i := 0; i < index; i++ {
		earlier := state.waiters[i]
		if earlier.tid != request.tid && !compatible(request.mode, earlier.mode) {
			return false
		}
	}
	return true
}

// mustDie implements wait-die deadlock prevention. Transaction IDs increase
// over time, so a younger requester may not wait for an older conflicting
// transaction. Applying the rule to queued conflicts also prevents compatible
// newcomers from starving an older queued writer.
func mustDie(state *lockState, index int) bool {
	request := state.waiters[index]
	for holderID, heldMode := range state.holders {
		if holderID != request.tid && !compatible(request.mode, heldMode) && request.tid > holderID {
			return true
		}
	}
	for i := 0; i < index; i++ {
		earlier := state.waiters[i]
		if earlier.tid != request.tid && !compatible(request.mode, earlier.mode) && request.tid > earlier.tid {
			return true
		}
	}
	return false
}

// Lock acquires a lock on a specific resource (Table or Tuple) with the requested mode. If the lock cannot be acquired
// immediately, the transaction blocks until it is granted or aborted. It returns nil if the lock is successfully
// acquired, or GoDBError(DeadlockError) in case of a (potential or detected) deadlock.
func (lm *LockManager) Lock(tid common.TransactionID, tag DBLockTag, mode DBLockMode) error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	state := lm.locks[tag]
	if state == nil {
		state = lm.newLockState()
		lm.locks[tag] = state
	}

	if heldMode, held := state.holders[tid]; held {
		if heldMode == mode || heldMode == LockModeX ||
			(heldMode == LockModeSIX && (mode == LockModeS || mode == LockModeIS || mode == LockModeIX)) ||
			(heldMode == LockModeS && mode == LockModeIS) ||
			(heldMode == LockModeIX && mode == LockModeIS) {
			return nil
		}
	}

	if len(state.waiters) == 0 && canGrant(state, tid, mode) {
		state.holders[tid] = mode
		return nil
	}

	waiterIndex := -1
	for i, waiter := range state.waiters {
		if waiter.tid == tid {
			waiterIndex = i
			state.waiters[i].mode = mode
			break
		}
	}
	if waiterIndex == -1 {
		state.waiters = append(state.waiters, lockRequest{tid: tid, tag: tag, mode: mode})
		waiterIndex = len(state.waiters) - 1
	}

	if mustDie(state, waiterIndex) {
		state.waiters = append(state.waiters[:waiterIndex], state.waiters[waiterIndex+1:]...)
		if len(state.holders) == 0 && len(state.waiters) == 0 {
			delete(lm.locks, tag)
		}
		state.changed.Broadcast()
		return common.GoDBError{
			Code:      common.DeadlockError,
			ErrString: "younger transaction cannot wait for an older transaction",
		}
	}

	for {
		for i, waiter := range state.waiters {
			if waiter.tid != tid {
				continue
			}
			waiterIndex = i
			if lm.canGrantQueued(state, i) {
				state.waiters = append(state.waiters[:i], state.waiters[i+1:]...)
				state.holders[tid] = mode
				state.changed.Broadcast()
				return nil
			}
			break
		}

		state.changed.Wait()
	}
}

// Unlock releases the lock held by the transaction on the specified resource. If the requesting transaction does not
// hold the specified lock, it should return GoDBError(LockNotFoundError)
func (lm *LockManager) Unlock(tid common.TransactionID, tag DBLockTag) error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	state, ok := lm.locks[tag]
	if !ok {
		return common.GoDBError{
			Code:      common.LockNotFoundError,
			ErrString: "resource has no lock",
		}
	}

	if _, held := state.holders[tid]; !held {
		return common.GoDBError{
			Code:      common.LockNotFoundError,
			ErrString: "transaction does not hold this lock",
		}
	}

	delete(state.holders, tid)

	if len(state.holders) == 0 && len(state.waiters) == 0 {
		delete(lm.locks, tag)
	}

	state.changed.Broadcast()
	return nil
}

// LockHeld checks if any transaction currently holds a lock on the given resource.
func (lm *LockManager) LockHeld(tag DBLockTag) bool {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	state, ok := lm.locks[tag]
	return ok && len(state.holders) > 0
}
