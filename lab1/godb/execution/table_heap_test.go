package execution

import (
	"errors"
	"fmt"
	"math/rand"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"mit.edu/dsg/godb/catalog"
	"mit.edu/dsg/godb/common"
	"mit.edu/dsg/godb/storage"
	"mit.edu/dsg/godb/transaction"
)

func makeTestDeps(t *testing.T) (*storage.BufferPool, *TableHeap) {
	testDir := t.TempDir()
	sm := storage.NewDiskStorageManager(testDir)
	bp := storage.NewBufferPool(10, sm, storage.NoopLogManager{})

	lockManager := transaction.NewLockManager()

	table := &catalog.Table{
		Oid:     100,
		Name:    "test_table",
		Columns: []catalog.Column{{Name: "id", Type: common.IntType}, {Name: "name", Type: common.StringType}},
	}
	th, err := NewTableHeap(table, bp, storage.NoopLogManager{}, lockManager)
	require.NoError(t, err)
	return bp, th
}

// TestTableHeap_SimpleLifecycle verifies the basic functionality of the TableHeap.
// It checks that:
// 1. Tuples can be inserted and retrieved correctly, crossing page boundaries.
// 2. Updates to existing tuples are persisted and retrieved correctly.
// 3. Deletions are respected by the iterator.
// 4. The iterator correctly traverses the heap, skipping deleted tuples and reflecting updates.
func TestTableHeap_SimpleLifecycle(t *testing.T) {
	_, th := makeTestDeps(t)
	desc := th.StorageSchema()

	numTuples := 500
	rids := make([]common.RecordID, numTuples)

	for i := 0; i < numTuples; i++ {
		tup := storage.FromValues(common.NewIntValue(int64(i)), common.NewStringValue(fmt.Sprintf("val-%d", i)))
		raw := make([]byte, desc.BytesPerTuple())
		tup.WriteToBuffer(raw, desc)

		rid, err := th.InsertTuple(nil, raw)
		require.NoError(t, err)
		rids[i] = rid
	}

	readBuf := make([]byte, desc.BytesPerTuple())
	for i := 0; i < numTuples; i += 10 {
		err := th.ReadTuple(nil, rids[i], readBuf, false)
		require.NoError(t, err)

		readTup := storage.FromRawTuple(readBuf, desc, rids[i])

		value0 := readTup.GetValue(0)
		assert.Equal(t, int64(i), value0.IntValue(), "ReadTuple ID mismatch at index %d", i)

		expectedStr := fmt.Sprintf("val-%d", i)
		value1 := readTup.GetValue(1)
		assert.Equal(t, expectedStr, value1.StringValue(), "ReadTuple String mismatch at index %d", i)
	}

	for i := 50; i < 100; i++ {
		tupUp := storage.FromValues(common.NewIntValue(int64(i)), common.NewStringValue("updated"))
		rawUp := make([]byte, desc.BytesPerTuple())
		tupUp.WriteToBuffer(rawUp, desc)

		err := th.UpdateTuple(nil, rids[i], rawUp)
		require.NoError(t, err, "UpdateTuple failed for index %d", i)
	}

	for i := 100; i < 150; i++ {
		err := th.DeleteTuple(nil, rids[i])
		require.NoError(t, err, "DeleteTuple failed for index %d", i)
	}

	iterBuf := make([]byte, desc.BytesPerTuple())
	iter, err := th.Iterator(nil, transaction.LockModeS, iterBuf)
	require.NoError(t, err)

	count := 0
	foundUpdates := 0

	for iter.Next() {
		curr := storage.FromRawTuple(iter.CurrentTuple(), desc, iter.CurrentRID())
		value0 := curr.GetValue(0)
		value1 := curr.GetValue(1)

		if value0.IntValue() >= 50 && value0.IntValue() < 100 {
			assert.Equal(t, "updated", value1.StringValue(), "Tuple %d should have been updated", value0.IntValue())
			foundUpdates++
		} else if value0.IntValue() >= 100 && value0.IntValue() < 150 {
			assert.Fail(t, "Found deleted tuple", "Tuple %d should have been deleted", value0.IntValue())
			expectedOriginal := fmt.Sprintf("val-%d", value0.IntValue())
			assert.Equal(t, expectedOriginal, value1.StringValue(), "Tuple %d content mismatch", value0.IntValue())
		}
		count++
	}
	_ = iter.Close()

	assert.Equal(t, 450, count, "Iterator count mismatch")
	assert.Equal(t, 50, foundUpdates, "Should find exactly 50 updated tuples")
}

// TestTableHeap_UpdateDeletedTuple ensures that operations on deleted tuples fail appropriately.
// It verifies that:
// 1. Updating a deleted tuple returns ErrTupleDeleted.
// 2. Reading a deleted tuple returns ErrTupleDeleted.
// 3. Deleting an already deleted tuple returns ErrTupleDeleted.
// 4. Operations on neighboring valid tuples remain unaffected.
func TestTableHeap_UpdateDeletedTuple(t *testing.T) {
	_, th := makeTestDeps(t)
	desc := th.StorageSchema()

	rids := make([]common.RecordID, 3)
	for i := 0; i < 3; i++ {
		tup := storage.FromValues(common.NewIntValue(int64(i)), common.NewStringValue(fmt.Sprintf("val-%d", i)))
		raw := make([]byte, desc.BytesPerTuple())
		tup.WriteToBuffer(raw, desc)
		rid, err := th.InsertTuple(nil, raw)
		require.NoError(t, err)
		rids[i] = rid
	}

	targetRID := rids[1]

	err := th.DeleteTuple(nil, targetRID)
	require.NoError(t, err)

	newRaw := make([]byte, desc.BytesPerTuple())
	updateTuple := storage.FromValues(common.NewIntValue(999), common.NewStringValue("phantom"))
	updateTuple.WriteToBuffer(newRaw, desc)

	err = th.UpdateTuple(nil, targetRID, newRaw)
	assert.ErrorIs(t, err, ErrTupleDeleted, "UpdateTuple on a deleted record should return ErrTupleDeleted")

	readBuf := make([]byte, desc.BytesPerTuple())
	err = th.ReadTuple(nil, targetRID, readBuf, false)
	assert.ErrorIs(t, err, ErrTupleDeleted, "ReadTuple on a deleted record should return ErrTupleDeleted")

	err = th.DeleteTuple(nil, targetRID)
	assert.ErrorIs(t, err, ErrTupleDeleted, "DeleteTuple on an already deleted record should return ErrTupleDeleted")

	// Check Neighbor 0
	err = th.ReadTuple(nil, rids[0], readBuf, false)
	require.NoError(t, err)
	t0 := storage.FromRawTuple(readBuf, desc, rids[0])
	value0 := t0.GetValue(0)
	assert.Equal(t, int64(0), value0.IntValue())

	// Check Neighbor 2
	err = th.ReadTuple(nil, rids[2], readBuf, false)
	require.NoError(t, err)
	t2 := storage.FromRawTuple(readBuf, desc, rids[2])
	value2 := t2.GetValue(0)
	assert.Equal(t, int64(2), value2.IntValue())
}

// TestTableHeap_Persistence verifies that data stored in the TableHeap is durable.
//
// Scenario:
// 1. Insert data into a TableHeap backed by the BufferPool.
// 2. Force the BufferPool to flush all dirty pages to disk.
// 3. Re-open the TableHeap from the same file/OID.
// 4. Append new data to the restored heap.
//
// Assertion:
// The new TableHeap instance must contain both the restored data and the newly appended data.
func TestTableHeap_Persistence(t *testing.T) {
	bp, th := makeTestDeps(t)
	desc := th.StorageSchema()

	for i := 0; i < 50; i++ {
		tup := storage.FromValues(common.NewIntValue(int64(i)), common.NewStringValue("old"))
		raw := make([]byte, desc.BytesPerTuple())
		tup.WriteToBuffer(raw, desc)
		_, err := th.InsertTuple(nil, raw)
		require.NoError(t, err)
	}

	// Flush to disk
	err := bp.FlushAllPages()
	require.NoError(t, err)

	table := &catalog.Table{
		Oid:     100, // Same OID
		Name:    "test_table",
		Columns: []catalog.Column{{Name: "id", Type: common.IntType}, {Name: "name", Type: common.StringType}},
	}
	th2, err := NewTableHeap(table, bp, storage.NoopLogManager{}, transaction.NewLockManager())
	require.NoError(t, err)

	for i := 50; i < 60; i++ {
		tup := storage.FromValues(common.NewIntValue(int64(i)), common.NewStringValue("new"))
		raw := make([]byte, desc.BytesPerTuple())
		tup.WriteToBuffer(raw, desc)
		_, err := th2.InsertTuple(nil, raw)
		require.NoError(t, err)
	}

	iter, _ := th2.Iterator(nil, transaction.LockModeS, make([]byte, desc.BytesPerTuple()))
	count := 0
	for iter.Next() {
		count++
	}
	assert.Equal(t, 60, count, "Should have persisted old data and appended new data")
}

// TestTableHeap_IteratorSkipsEmptyPages checks that the iterator gracefully handles gaps.
//
// Scenario:
// 1. Insert a continuous range of tuples.
// 2. Delete a large contiguous chunk in the middle, potentially creating empty pages.
//
// Assertion:
// The iterator must skip over the deleted tuples (and empty pages) and resume returning
// valid tuples from the next available slot, matching the expected count.
func TestTableHeap_IteratorSkipsEmptyPages(t *testing.T) {
	_, th := makeTestDeps(t)
	desc := th.StorageSchema()

	n := 500
	rids := make([]common.RecordID, n)
	for i := 0; i < n; i++ {
		tup := storage.FromValues(common.NewIntValue(int64(i)), common.NewStringValue("data"))
		raw := make([]byte, desc.BytesPerTuple())
		tup.WriteToBuffer(raw, desc)
		rids[i], _ = th.InsertTuple(nil, raw)
	}

	for i := 120; i < 400; i++ {
		_ = th.DeleteTuple(nil, rids[i])
	}

	iter, _ := th.Iterator(nil, transaction.LockModeS, make([]byte, desc.BytesPerTuple()))
	count := 0
	for iter.Next() {
		curr := storage.FromRawTuple(iter.CurrentTuple(), desc, iter.CurrentRID())
		value0 := curr.GetValue(0)
		assert.False(t, 120 <= value0.IntValue() && value0.IntValue() < 400, "Iterator returned a deleted tuple")
		count++
	}
	assert.Equal(t, n-280, count)
}

// TestTableHeap_Vacuum verifies the functionality of VacuumPage.
//
// Scenario:
// 1. Insert tuples and then delete every other tuple.
// 2. Call VacuumPage on affected pages.
//
// Assertions:
// 1. Low-level check: The HeapPage bitmap/metadata correctly reflects that deleted slots are unallocated.
// 2. High-level check: The iterator still returns all active tuples correctly.
func TestTableHeap_Vacuum(t *testing.T) {
	bp, th := makeTestDeps(t)
	desc := th.StorageSchema()

	numTuples := 400
	rids := make([]common.RecordID, numTuples)

	for i := 0; i < numTuples; i++ {
		tup := storage.FromValues(common.NewIntValue(int64(i)), common.NewStringValue(fmt.Sprintf("val-%d", i)))
		raw := make([]byte, desc.BytesPerTuple())
		tup.WriteToBuffer(raw, desc)
		rid, err := th.InsertTuple(nil, raw)
		require.NoError(t, err)
		rids[i] = rid
	}

	for i := 0; i < numTuples; i += 2 {
		err := th.DeleteTuple(nil, rids[i])
		require.NoError(t, err)
	}

	pageSet := make(map[common.PageID]any)
	for _, rid := range rids {
		pageSet[rid.PageID] = nil
	}

	for pid := range pageSet {
		err := th.VacuumPage(pid)
		require.NoError(t, err, "VacuumPage failed for page %v", pid)
	}

	for i := 0; i < numTuples; i++ {
		rid := rids[i]

		page, err := bp.GetPage(rid.PageID)
		require.NoError(t, err)
		hp := page.AsHeapPage()

		if i%2 == 0 {
			assert.False(t, hp.IsAllocated(rid), "Tuple %d (deleted) should be deallocated after Vacuum", i)
		} else {
			assert.True(t, hp.IsAllocated(rid), "Tuple %d (active) should still be allocated", i)
		}
		bp.UnpinPage(page, false)
	}

	iter, err := th.Iterator(nil, transaction.LockModeS, make([]byte, desc.BytesPerTuple()))
	require.NoError(t, err)

	count := 0
	for iter.Next() {
		tup := storage.FromRawTuple(iter.CurrentTuple(), desc, iter.CurrentRID())
		value := tup.GetValue(0)
		val := value.IntValue()

		assert.Equal(t, int64(1), val%2, "Iterator returned an even (deleted) tuple ID: %d", val)
		count++
	}
	_ = iter.Close()

	assert.Equal(t, numTuples/2, count, "Iterator should return exactly the remaining half of tuples")
}

// TestTableHeap_ConcurrentMixedWorkload stresses the TableHeap with concurrent operations.
//
// Scenario:
// 1. Growers: Continuously insert new tuples.
// 2. Accessors: Randomly Read, Update, or Delete existing tuples (tracked via activeRIDs).
// 3. Scanners: Continuously scan the entire table to verify data integrity.
//
// Assertions:
// - Data Consistency: Values read always match the key (val-{ID}).
// - Isolation: Deletion and Updates are handled safely without race conditions causing panics or corruption.
// - Final State: The number of items remaining in the heap matches the tracked state (Inserts - Deletes).
func TestTableHeap_ConcurrentMixedWorkload(t *testing.T) {
	_, th := makeTestDeps(t)
	desc := th.StorageSchema()

	numGrowers := runtime.NumCPU()
	numAccessers := runtime.NumCPU() * 2
	numScanners := 1
	workDuration := 5 * time.Second

	var ridLock sync.RWMutex
	var activeRIDs []common.RecordID
	deleted := make(map[common.RecordID]bool)

	start := time.Now()
	var wg sync.WaitGroup

	// Continuously insert new tuples.
	for i := 0; i < numGrowers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(int64(id)))
			for time.Since(start) < workDuration {
				// Pattern: { ID, "val-{ID}" }
				key := int64(rng.Uint32())
				valStr := fmt.Sprintf("val-%d", key)

				tup := storage.FromValues(common.NewIntValue(key), common.NewStringValue(valStr))
				raw := make([]byte, desc.BytesPerTuple())
				tup.WriteToBuffer(raw, desc)

				rid, err := th.InsertTuple(nil, raw)
				assert.NoError(t, err, "Insert failed on grower %d", id)

				ridLock.Lock()
				activeRIDs = append(activeRIDs, rid)
				deleted[rid] = false
				ridLock.Unlock()

				runtime.Gosched()
			}
		}(i)
	}

	// Reads, Updates, or Deletes existing tuples.
	for i := 0; i < numAccessers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(int64(id) + 100))
			opBuf := make([]byte, desc.BytesPerTuple())

			for time.Since(start) < workDuration {
				var rid common.RecordID
				op := rng.Intn(100)

				ridLock.RLock()
				if len(activeRIDs) == 0 {
					ridLock.RUnlock()
					runtime.Gosched()
					continue
				}

				idx := rng.Intn(len(activeRIDs))
				rid = activeRIDs[idx]
				ridLock.RUnlock()

				if op < 50 {
					err := th.ReadTuple(nil, rid, opBuf, false)
					if err != nil {
						assert.True(t, errors.Is(err, ErrTupleDeleted), fmt.Sprintf("Accessor Read failed with unexpected error: %v", err))
						ridLock.RLock()
						assert.True(t, deleted[rid], "Unexpected deletion of tuple")
						ridLock.RUnlock()
					} else {
						tup := storage.FromRawTuple(opBuf, desc, rid)
						value0 := tup.GetValue(0)
						key := value0.IntValue()
						value1 := tup.GetValue(1)
						val := value1.StringValue()
						assert.Equal(t, fmt.Sprintf("val-%d", key), val, "Data integrity violation")
					}

				} else if op < 80 {
					newKey := int64(rng.Uint32())
					newVal := fmt.Sprintf("val-%d", newKey)

					newTup := storage.FromValues(common.NewIntValue(newKey), common.NewStringValue(newVal))
					newTup.WriteToBuffer(opBuf, desc)

					err := th.UpdateTuple(nil, rid, opBuf)
					if err != nil {
						assert.True(t, errors.Is(err, ErrTupleDeleted), fmt.Sprintf("Accessor Read failed with unexpected error: %v", err))
						ridLock.RLock()
						assert.True(t, deleted[rid], "Unexpeceted deletion of tuple")
						ridLock.RUnlock()
					}
				} else {
					ridLock.Lock()
					deleted[rid] = true
					ridLock.Unlock()
					err := th.DeleteTuple(nil, rid)
					if err != nil {
						assert.True(t, errors.Is(err, ErrTupleDeleted), fmt.Sprintf("Accessor Read failed with unexpected error: %v", err))
					}
				}
			}
		}(i)
	}

	// Continuous background validation of the entire heap.
	for i := 0; i < numScanners; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			iterBuf := make([]byte, desc.BytesPerTuple())
			for time.Since(start) < workDuration {
				iter, err := th.Iterator(nil, transaction.LockModeS, iterBuf)
				assert.NoError(t, err, "Scanner failed to create iterator")

				for iter.Next() {
					tup := storage.FromRawTuple(iter.CurrentTuple(), desc, iter.CurrentRID())
					value0 := tup.GetValue(0)
					key := value0.IntValue()
					value1 := tup.GetValue(1)
					val := value1.StringValue()

					assert.Equal(t, fmt.Sprintf("val-%d", key), val, "Scanner saw corrupted tuple")
				}
				_ = iter.Close()
			}
		}(i)
	}

	wg.Wait()

	finalIterBuf := make([]byte, desc.BytesPerTuple())
	iter, err := th.Iterator(nil, transaction.LockModeS, finalIterBuf)
	require.NoError(t, err)
	seenCount := 0
	for iter.Next() {
		assert.False(t, deleted[iter.CurrentRID()], "Found d tuple")
		seenCount++
	}
	_ = iter.Close()

	expectedCount := 0
	for _, d := range deleted {
		if !d {
			expectedCount++
		}
	}

	assert.Equal(t, expectedCount, seenCount, "Expected Balance vs Actual DB Count Mismatch. Possible lost insert or phantom delete.")
}
