package storage

import (
	"bytes"
	"fmt"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"mit.edu/dsg/godb/common"
)

func TestHeapPageSimple(t *testing.T) {
	desc := NewRawTupleDesc([]common.Type{common.IntType, common.StringType})
	frame := &PageFrame{}
	InitializeHeapPage(desc, frame)
	hp := frame.AsHeapPage()
	numSlots := hp.NumSlots()
	assert.Greater(t, numSlots, 0, "Page should have slots available")
	assert.Equal(t, 0, hp.NumUsed(), "Page should be empty at start")

	for i := 0; i < numSlots; i++ {
		slot := hp.FindFreeSlot()
		assert.NotEqual(t, -1, slot, "Page should have free slots at this time")

		rid := common.RecordID{Slot: int32(slot)}
		hp.MarkAllocated(rid, true)

		tuple := hp.AccessTuple(rid)
		desc.SetValue(tuple, 0, common.NewIntValue(int64(i)))
		desc.SetValue(tuple, 1, common.NewStringValue(fmt.Sprintf("val-%d", i)))
		assert.Equal(t, i+1, hp.NumUsed(), "num used should increase by 1 for each new tuple")
	}

	assert.Equal(t, numSlots, hp.NumUsed())
	assert.Equal(t, -1, hp.FindFreeSlot(), "Page should be full at this point")

	for i := 0; i < numSlots; i++ {
		rid := common.RecordID{Slot: int32(i)}
		assert.True(t, hp.IsAllocated(rid), fmt.Sprintf("Slot %d should be marked allocated", i))
		assert.False(t, hp.IsDeleted(rid), fmt.Sprintf("Slot %d should not be marked deleted", i))

		tuple := hp.AccessTuple(rid)
		val0 := desc.GetValue(tuple, 0)
		val1 := desc.GetValue(tuple, 1)
		assert.Equal(t, int64(i), val0.IntValue(), "Int value mismatch at i %d", i)
		assert.Equal(t, fmt.Sprintf("val-%d", i), val1.StringValue(), "String value mismatch at i %d", i)
	}

	for i := 0; i < numSlots; i += 3 {
		rid := common.RecordID{Slot: int32(i)}
		hp.MarkDeleted(rid, true)
		assert.True(t, hp.IsDeleted(rid), "Slot %d should be marked deleted", i)
		assert.True(t, hp.IsAllocated(rid), "Slot %d should still be marked allocated (soft delete)", i)
	}
	assert.Equal(t, numSlots, hp.NumUsed(), "num used should not change (soft delete should not affect it)")
	assert.Equal(t, -1, hp.FindFreeSlot(), "Should not be able to allocate soft-deleted slots")

	for i := 0; i < numSlots; i += 3 {
		rid := common.RecordID{Slot: int32(i)}
		hp.MarkAllocated(rid, false)

		assert.False(t, hp.IsAllocated(rid), "Slot %d should now be free", i)
		assert.False(t, hp.IsDeleted(rid), "Freeing i %d should clear the deleted bit", i)
		assert.Equal(t, numSlots-i/3-1, hp.NumUsed(), "num used should decrease by 1 for each deallocated tuple")
	}

	slotToIdMap := make(map[int]int)
	for i := 0; i < numSlots; i += 3 {
		slot := hp.FindFreeSlot()
		assert.NotEqual(t, -1, slot, "Should be able to find freed slots")

		slotToIdMap[slot] = i

		rid := common.RecordID{Slot: int32(slot)}
		hp.MarkAllocated(rid, true)

		tup := hp.AccessTuple(rid)
		desc.SetValue(tup, 0, common.NewIntValue(int64(i+5000)))
		desc.SetValue(tup, 1, common.NewStringValue(fmt.Sprintf("new-val-%d", i)))
	}

	assert.Equal(t, numSlots, hp.NumUsed())
	assert.Equal(t, -1, hp.FindFreeSlot())

	for i := 0; i < numSlots; i++ {
		rid := common.RecordID{Slot: int32(i)}
		assert.True(t, hp.IsAllocated(rid))
		assert.False(t, hp.IsDeleted(rid))
		tuple := hp.AccessTuple(rid)
		val0 := desc.GetValue(tuple, 0)
		val1 := desc.GetValue(tuple, 1)

		if i%3 == 0 {
			originalID, ok := slotToIdMap[i]
			assert.True(t, ok, "Slot %d should have been re-allocated", i)

			assert.Equal(t, int64(originalID+5000), val0.IntValue(), "Final check: Int mismatch at slot %d", i)
			assert.Equal(t, fmt.Sprintf("new-val-%d", originalID), val1.StringValue(), "Final check: String mismatch at slot %d", i)
		} else {
			assert.Equal(t, int64(i), val0.IntValue(), "Final check: Int mismatch at slot %d", i)
			assert.Equal(t, fmt.Sprintf("val-%d", i), val1.StringValue(), "Final check: String mismatch at slot %d", i)
		}
	}
}
func TestHeapPageLoad(t *testing.T) {
	desc := NewRawTupleDesc([]common.Type{common.IntType, common.StringType})
	frame := &PageFrame{}
	InitializeHeapPage(desc, frame)
	hp1 := frame.AsHeapPage()

	numSlots := hp1.NumSlots()

	// Create a complex pattern on hp1 (Allocation + Data + Deletion)
	for i := 0; i < numSlots; i++ {
		rid := common.RecordID{Slot: int32(i)}

		if i%2 == 0 {
			hp1.MarkAllocated(rid, true)

			tup := hp1.AccessTuple(rid)
			desc.SetValue(tup, 0, common.NewIntValue(int64(i*100)))
			desc.SetValue(tup, 1, common.NewStringValue(fmt.Sprintf("val-%d", i)))

			if i%3 == 0 {
				hp1.MarkDeleted(rid, true)
			}
		}
	}

	// "Load" the page again from the same raw bytes
	// This simulates fetching the page frame from the BufferPool or Disk. The two should be identical
	hp2 := frame.AsHeapPage()

	assert.Equal(t, hp1.NumUsed(), hp2.NumUsed(), "NumUsed mismatch on reload")
	assert.Equal(t, hp1.NumSlots(), hp2.NumSlots(), "NumSlots mismatch on reload")

	for i := 0; i < numSlots; i++ {
		rid := common.RecordID{Slot: int32(i)}

		// Check Metadata (Allocated/Deleted)
		assert.Equal(t, hp1.IsAllocated(rid), hp2.IsAllocated(rid), "Allocation mismatch at slot %d", i)
		assert.Equal(t, hp1.IsDeleted(rid), hp2.IsDeleted(rid), "Deletion mismatch at slot %d", i)
		if hp1.IsAllocated(rid) && !hp1.IsDeleted(rid) {
			assert.Equal(t, hp1.AccessTuple(rid), hp2.AccessTuple(rid), "Tuple mismatch at slot %d", i)
		}
	}
}

// Helper to generate a random tuple byte slice based on the descriptor
func generateRandomTupleData(r *rand.Rand, desc *RawTupleDesc) []byte {
	buf := make([]byte, desc.BytesPerTuple())

	for i := 0; i < desc.NumColumns(); i++ {
		fieldType := desc.GetFieldType(i)
		switch fieldType {
		case common.IntType:
			val := common.NewIntValue(r.Int63())
			desc.SetValue(buf, i, val)
		case common.StringType:
			// Generate random string
			strLen := r.Intn(10) + 1
			strBytes := make([]byte, strLen)
			r.Read(strBytes)
			val := common.NewStringValue(string(strBytes))
			desc.SetValue(buf, i, val)
		}
	}
	return buf
}

// runRandomizedHeapPageTest executes a randomized test on the HeapPage implementation against a reference (Slot -> []byte) map
//
// Scenario:
// 1. Setup: Creates a HeapPage with a specific schema (RawTupleDesc).
// 3. Operations: Performs 50,000 random operations:
//   - Allocate: Finds a free slot, marks it used, writes random data, updates Shadow.
//   - Free: Deallocates a random existing slot, removes from Shadow.
//   - Soft Delete: Marks a slot as deleted without freeing it (verifies IsDeleted flag).
//   - Un-Delete: Clears the deleted flag.
//   - Invariant Check: Periodically scans the ENTIRE page to verify every slot matches the Shadow map.
//
// Assertions:
// - Data Integrity: Data read from the page via AccessTuple must exactly match the Shadow map.
// - Metadata Consistency: IsAllocated/IsDeleted must match the Shadow state.
// - Slot Management: FindFreeSlot must return -1 iff the Shadow map numBits equals NumSlots.
// - Persistence: Data must persist correctly across operations until explicitly freed.
func runRandomizedHeapPageTest(t *testing.T, desc *RawTupleDesc, seed int64) {
	r := rand.New(rand.NewSource(seed))
	frame := &PageFrame{}
	InitializeHeapPage(desc, frame)
	hp := frame.AsHeapPage()

	numSlots := hp.NumSlots()

	// Treat the page as a Map of RID -> Bytes
	// keys are slot indices
	shadowData := make(map[int32][]byte)
	shadowDeleted := make(map[int32]bool)

	iterations := 50000
	for i := 0; i < iterations; i++ {
		op := r.Intn(5)

		switch op {
		case 0: // Allocate & Write
			slot := hp.FindFreeSlot()
			if slot != -1 {
				rid := common.RecordID{Slot: int32(slot)}
				_, exists := shadowData[rid.Slot]
				assert.False(t, exists, "FindFreeSlot returned slot %d which shadow thinks is occupied (iter %d)", slot, i)
				hp.MarkAllocated(rid, true)
				data := generateRandomTupleData(r, desc)
				tup := hp.AccessTuple(rid)
				copy(tup, data)
				shadowData[rid.Slot] = data
				delete(shadowDeleted, rid.Slot) // New allocations are never deleted
			} else {
				assert.Equal(t, numSlots, len(shadowData), "FindFreeSlot returned -1 but shadow map has numBits %d/%d", len(shadowData), numSlots)
			}

		case 1: // Free
			if len(shadowData) == 0 {
				continue
			}
			var victimSlot int32
			for k := range shadowData {
				victimSlot = k
				break
			}

			rid := common.RecordID{Slot: victimSlot}
			hp.MarkAllocated(rid, false)

			// Remove from shadow maps
			delete(shadowData, victimSlot)
			delete(shadowDeleted, victimSlot)

		case 2: // Mark Deleted
			if len(shadowData) == 0 {
				continue
			}
			for k := range shadowData {
				// Only mark deleted if not already deleted (to vary the ops, though re-marking is valid)
				if !shadowDeleted[k] {
					rid := common.RecordID{Slot: k}
					hp.MarkDeleted(rid, true)
					shadowDeleted[k] = true
					break
				}
			}

		case 3: // Un-Delete
			if len(shadowDeleted) == 0 {
				continue
			}
			for k := range shadowDeleted {
				rid := common.RecordID{Slot: k}
				hp.MarkDeleted(rid, false)
				delete(shadowDeleted, k)
				break
			}
		case 4: // Invariant check
			for slot := 0; slot < numSlots; slot++ {
				rid := common.RecordID{Slot: int32(slot)}

				expectedData, exists := shadowData[int32(slot)]

				if exists {
					assert.True(t, hp.IsAllocated(rid), "Slot %d should be allocated", slot)
					actualData := hp.AccessTuple(rid)
					assert.True(t, bytes.Equal(expectedData, actualData), "Data mismatch at slot %d", slot)

					isDeleted := shadowDeleted[int32(slot)]
					assert.Equal(t, isDeleted, hp.IsDeleted(rid), "Deleted status mismatch at slot %d", slot)
				} else {
					assert.False(t, hp.IsAllocated(rid), "Slot %d should be free", slot)
				}
			}
			assert.Equal(t, len(shadowData), hp.NumUsed(), "NumUsed mismatch")
		}
	}
}

func TestHeapPageRandomized(t *testing.T) {
	masterSeed := int64(42)
	r := rand.New(rand.NewSource(masterSeed))

	// Define test strategies
	// 0: Tiny (Single Int) - Tests Max Slots
	// 1: Small Mixed (1-5 cols) - Standard Case
	// 2: Medium Mixed (5-20 cols)
	// 3: Wide Ints (Many columns) - Tests column offset logic
	// 4: Edge Case: Max Size - Tuple nearly fills page (Tests NumSlots=1 or 2)
	strategies := []int{0, 1, 2, 3, 4}

	for _, strategy := range strategies {
		var fields []common.Type

		switch strategy {
		case 0: // Tiny
			fields = []common.Type{common.IntType}

		case 1: // Small Mixed
			n := r.Intn(5) + 1
			for k := 0; k < n; k++ {
				if r.Intn(2) == 0 {
					fields = append(fields, common.IntType)
				} else {
					fields = append(fields, common.StringType)
				}
			}

		case 2: // Medium Mixed
			n := r.Intn(15) + 5
			for k := 0; k < n; k++ {
				if r.Intn(2) == 0 {
					fields = append(fields, common.IntType)
				} else {
					fields = append(fields, common.StringType)
				}
			}

		case 3: // Wide Ints (Many Columns)
			// Generate enough Ints to fill about half the page
			// Safe estimate: PageSize / 2 / IntSize
			n := (common.PageSize - 32) / 8
			for k := 0; k < n; k++ {
				fields = append(fields, common.IntType)
			}

		case 4: // Edge Case: Max Size (Near Page Limit)
			currentSize := 0
			limit := common.PageSize - 32

			for currentSize < limit {
				// Randomly pick type
				t := common.IntType
				sizeInc := 8
				if r.Intn(2) == 1 {
					t = common.StringType
					sizeInc = common.StringLength
				}

				if currentSize+sizeInc >= limit {
					break
				}
				fields = append(fields, t)
				currentSize += sizeInc
			}
		}

		desc := NewRawTupleDesc(fields)
		runSeed := r.Int63()
		testName := fmt.Sprintf("Strat%d_Cols%d_Size%d", strategy, len(fields), desc.BytesPerTuple())

		t.Run(testName, func(t *testing.T) {
			runRandomizedHeapPageTest(t, desc, runSeed)
		})
	}
}
