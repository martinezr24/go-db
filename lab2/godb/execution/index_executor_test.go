package execution

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"mit.edu/dsg/godb/common"
	"mit.edu/dsg/godb/indexing"
	"mit.edu/dsg/godb/planner"
	"mit.edu/dsg/godb/storage"
)

// createKey is a helper to construct an indexing.Key from values.
func createKey(idx indexing.Index, vals ...common.Value) indexing.Key {
	tup := storage.FromValues(vals...)
	keyBuf := make([]byte, idx.Metadata().KeySchema.BytesPerTuple())
	tup.WriteToBuffer(keyBuf, idx.Metadata().KeySchema)
	return idx.Metadata().AsKey(keyBuf)
}

func TestIndexExecutor_Lookup_Hash(t *testing.T) {
	th := setupSalesTable(t, storage.NewBufferPool(20, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{}))
	// Hash Index on ID (column 0)
	idxId := indexing.NewMemHashIndex(storage.NewRawTupleDesc([]common.Type{common.IntType}), []int{0})
	indexes := []indexing.Index{idxId}

	key5 := createKey(idxId, common.NewIntValue(5))
	lookupNode := planner.NewIndexLookupNode(
		common.ObjectID(uint32(0)),
		th.oid,
		th.StorageSchema().GetFieldTypes(),
		key5,
		false,
	)
	lookupExec := NewIndexLookupExecutor(lookupNode, idxId, th)
	require.NoError(t, lookupExec.Init(NewExecutorContext(nil)))
	assert.False(t, lookupExec.Next(), "Empty index should return false immediately")

	insertSalesData(t, th, indexes, 10)
	require.NoError(t, lookupExec.Init(NewExecutorContext(nil)))

	found := false
	for lookupExec.Next() {
		tup := lookupExec.Current()
		// Verify we fetched the FULL tuple from the heap, not just the index key.
		assert.Equal(t, int64(5), tup.GetValue(0).IntValue(), "ID mismatch")
		assert.Equal(t, int64(1000), tup.GetValue(1).IntValue(), "Revenue mismatch - did you fetch from heap?")
		assert.Equal(t, "EU", tup.GetValue(3).StringValue(), "Region mismatch")
		assert.Equal(t, "Clothing", tup.GetValue(4).StringValue(), "Category mismatch")
		found = true
	}
	assert.True(t, found, "Hash: Should find tuple with ID 5")

	// Negative Test
	key99 := createKey(idxId, common.NewIntValue(99))
	lookupNode2 := planner.NewIndexLookupNode(
		common.ObjectID(uint32(0)),
		th.oid,
		th.StorageSchema().GetFieldTypes(),
		key99,
		false,
	)
	lookupExec2 := NewIndexLookupExecutor(lookupNode2, idxId, th)
	require.NoError(t, lookupExec2.Init(NewExecutorContext(nil)))
	assert.False(t, lookupExec2.Next(), "Hash: Should not find tuple with ID 99")
}

func TestIndexExecutor_Lookup_BTree(t *testing.T) {
	th := setupSalesTable(t, storage.NewBufferPool(20, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{}))
	// BTree Index on ID (column 0)
	idxId := indexing.NewMemBTreeIndex(storage.NewRawTupleDesc([]common.Type{common.IntType}), []int{0})
	indexes := []indexing.Index{idxId}

	key3 := createKey(idxId, common.NewIntValue(3))
	lookupNode := planner.NewIndexLookupNode(
		common.ObjectID(uint32(0)),
		th.oid,
		th.StorageSchema().GetFieldTypes(),
		key3,
		false,
	)
	lookupExec := NewIndexLookupExecutor(lookupNode, idxId, th)
	require.NoError(t, lookupExec.Init(NewExecutorContext(nil)))
	assert.False(t, lookupExec.Next(), "Empty index should return false immediately")

	insertSalesData(t, th, indexes, 10)
	require.NoError(t, lookupExec.Init(NewExecutorContext(nil)))

	found := false
	for lookupExec.Next() {
		tup := lookupExec.Current()
		assert.Equal(t, int64(3), tup.GetValue(0).IntValue(), "ID mismatch")
		assert.Equal(t, int64(600), tup.GetValue(1).IntValue(), "Revenue mismatch - did you fetch from heap?")
		assert.Equal(t, "EU", tup.GetValue(3).StringValue(), "Region mismatch")
		assert.Equal(t, "Electronics", tup.GetValue(4).StringValue(), "Category mismatch")
		found = true
	}
	assert.True(t, found, "BTree: Should find tuple with ID 3")
}

func TestIndexExecutor_Lookup_Duplicates(t *testing.T) {
	th := setupSalesTable(t, storage.NewBufferPool(20, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{}))

	// Index on (Region, Category)
	idxMulti := indexing.NewMemHashIndex(
		storage.NewRawTupleDesc([]common.Type{common.StringType, common.StringType}),
		[]int{3, 4},
	)
	indexes := []indexing.Index{idxMulti}
	insertSalesData(t, th, indexes, 15)

	keyTarget := createKey(idxMulti, common.NewStringValue("US"), common.NewStringValue("Electronics"))
	lookupNode := planner.NewIndexLookupNode(
		// Not needed, can initialize with default value
		common.ObjectID(uint32(0)),
		th.oid,
		th.StorageSchema().GetFieldTypes(),
		keyTarget,
		false,
	)
	exec := NewIndexLookupExecutor(lookupNode, idxMulti, th)
	require.NoError(t, exec.Init(NewExecutorContext(nil)))

	foundIds := []int64{}
	for exec.Next() {
		tup := exec.Current()
		foundIds = append(foundIds, tup.GetValue(0).IntValue())
	}
	sort.Slice(foundIds, func(i, j int) bool { return foundIds[i] < foundIds[j] })

	expectedIds := []int64{0, 6, 12}
	assert.Equal(t, expectedIds, foundIds, "Should find duplicates for multi-column hash key (multiples of 6)")
}

func TestIndexExecutor_Scan(t *testing.T) {
	th := setupSalesTable(t, storage.NewBufferPool(20, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{}))
	// Index on (Region, Revenue)
	idxMulti := indexing.NewMemBTreeIndex(
		storage.NewRawTupleDesc([]common.Type{common.StringType, common.IntType}),
		[]int{3, 1},
	)
	indexes := []indexing.Index{idxMulti}

	insertSalesData(t, th, indexes, 6)

	// We want to Scan >= ("EU", 500)
	//
	// Expected Order in Tree (Region ASC, Rev ASC):
	// 1. ("EU", 200)  [ID 1]
	// 2. ("EU", 600)  [ID 3] <--- Start Match
	// 3. ("EU", 1000) [ID 5]
	// 4. ("US", 0)    [ID 0]
	// 5. ("US", 400)  [ID 2]
	// 6. ("US", 800)  [ID 4]
	//
	// Result should be IDs: 3, 5, 0, 2, 4

	startKey := createKey(idxMulti, common.NewStringValue("EU"), common.NewIntValue(500))
	scanNode := planner.NewIndexScanNode(
		// Not needed, can initialize with default value
		common.ObjectID(uint32(0)),
		th.oid,
		th.StorageSchema().GetFieldTypes(),
		indexing.ScanDirectionForward,
		startKey,
		false,
	)

	exec := NewIndexScanExecutor(scanNode, idxMulti, th)
	require.NoError(t, exec.Init(NewExecutorContext(nil)))

	actualIds := []int64{}
	for exec.Next() {
		actualIds = append(actualIds, exec.Current().GetValue(0).IntValue())
	}

	expectedIds := []int64{3, 5, 0, 2, 4}
	assert.Equal(t, expectedIds, actualIds, "BTree Range Scan should respect composite sort order")

	// We want to Scan <= ("US", 400)
	//
	// Expected Order in Tree (Region ASC, Rev ASC):
	// 1. ("EU", 200)  [ID 1]
	// 2. ("EU", 600)  [ID 3]
	// 3. ("EU", 1000) [ID 5]
	// 4. ("US", 0)    [ID 0]
	// 5. ("US", 400)  [ID 2] <--- Start Match
	// 6. ("US", 800)  [ID 4]
	//
	// Result should be IDs: 3, 5, 0, 2, 4

	startKey = createKey(idxMulti, common.NewStringValue("US"), common.NewIntValue(400))
	scanNode = planner.NewIndexScanNode(
		// Not needed, can initialize with default value
		common.ObjectID(uint32(0)),
		th.oid,
		th.StorageSchema().GetFieldTypes(),
		indexing.ScanDirectionBackward,
		startKey,
		false,
	)

	exec = NewIndexScanExecutor(scanNode, idxMulti, th)
	require.NoError(t, exec.Init(NewExecutorContext(nil)))

	actualIds = []int64{}
	for exec.Next() {
		actualIds = append(actualIds, exec.Current().GetValue(0).IntValue())
	}

	expectedIds = []int64{2, 0, 5, 3, 1}
	assert.Equal(t, expectedIds, actualIds, "BTree Range Scan should respect composite sort order")
}

func TestIndexExecutor_Nulls(t *testing.T) {
	th := setupSalesTable(t, storage.NewBufferPool(20, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{}))
	// Index on Cost (Int) which allows Nulls
	idxCost := indexing.NewMemBTreeIndex(
		storage.NewRawTupleDesc([]common.Type{common.IntType}),
		[]int{2},
	)
	indexes := []indexing.Index{idxCost}

	rowA := []planner.Expr{
		planner.NewConstantValueExpression(common.NewIntValue(100)), // ID
		planner.NewConstantValueExpression(common.NewIntValue(0)),   // Rev
		planner.NewConstantValueExpression(common.NewNullInt()),     // Cost = NULL
		planner.NewConstantValueExpression(common.NewStringValue("US")),
		planner.NewConstantValueExpression(common.NewStringValue("A")),
	}
	// Row B: Cost=NULL
	rowB := []planner.Expr{
		planner.NewConstantValueExpression(common.NewIntValue(101)), // ID
		planner.NewConstantValueExpression(common.NewIntValue(0)),   // Rev
		planner.NewConstantValueExpression(common.NewNullInt()),     // Cost = NULL
		planner.NewConstantValueExpression(common.NewStringValue("US")),
		planner.NewConstantValueExpression(common.NewStringValue("A")),
	}
	insertRows(t, th, [][]planner.Expr{rowA, rowB}, indexes)

	keyNull := createKey(idxCost, common.NewNullInt())
	lookupNode := planner.NewIndexLookupNode(
		// Not needed, can initialize with default value
		common.ObjectID(uint32(0)),
		th.oid,
		th.StorageSchema().GetFieldTypes(),
		keyNull,
		false,
	)
	exec := NewIndexLookupExecutor(lookupNode, idxCost, th)
	require.NoError(t, exec.Init(NewExecutorContext(nil)))

	foundIds := []int64{}
	for exec.Next() {
		tup := exec.Current()
		foundIds = append(foundIds, tup.GetValue(0).IntValue())
		assert.True(t, tup.GetValue(2).IsNull(), "Retrieved value should be Null")
	}
	sort.Slice(foundIds, func(i, j int) bool { return foundIds[i] < foundIds[j] })
	assert.Equal(t, []int64{100, 101}, foundIds, "Should find multiple NULL keys")
}

func TestIndexExecutor_Restart(t *testing.T) {
	th := setupSalesTable(t, storage.NewBufferPool(20, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{}))
	// BTree Index on ID
	idxId := indexing.NewMemBTreeIndex(storage.NewRawTupleDesc([]common.Type{common.IntType}), []int{0})
	indexes := []indexing.Index{idxId}

	insertSalesData(t, th, indexes, 5) // IDs 0, 1, 2, 3, 4

	startKey := createKey(idxId, common.NewIntValue(2))
	scanNode := planner.NewIndexScanNode(
		common.ObjectID(uint32(0)),
		th.oid,
		th.StorageSchema().GetFieldTypes(),
		indexing.ScanDirectionForward,
		startKey,
		false,
	)

	exec := NewIndexScanExecutor(scanNode, idxId, th)
	require.NoError(t, exec.Init(NewExecutorContext(nil)))

	// First Pass
	count := 0
	for exec.Next() {
		count++
	}
	assert.Equal(t, 3, count, "First pass should find 3 tuples (2, 3, 4)")

	require.NoError(t, exec.Init(NewExecutorContext(nil)))
	// Second Pass
	count = 0
	for exec.Next() {
		count++
	}
	assert.Equal(t, 3, count, "Second pass after Init() should find 3 tuples again")
}

func TestIndexExecutor_Scan_EdgeCases(t *testing.T) {
	th := setupSalesTable(t, storage.NewBufferPool(20, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{}))
	// BTree Index on ID (Column 0)
	idxId := indexing.NewMemBTreeIndex(storage.NewRawTupleDesc([]common.Type{common.IntType}), []int{0})
	indexes := []indexing.Index{idxId}

	// 1. Insert known values: 0, 10, 20, 30, 40
	// We manually insert to ensure specific gaps exist.
	var rows [][]planner.Expr
	for _, val := range []int64{0, 10, 20, 30, 40} {
		row := []planner.Expr{
			planner.NewConstantValueExpression(common.NewIntValue(val)),         // ID
			planner.NewConstantValueExpression(common.NewIntValue(0)),           // Revenue
			planner.NewConstantValueExpression(common.NewIntValue(0)),           // Cost
			planner.NewConstantValueExpression(common.NewStringValue("Region")), // Region
			planner.NewConstantValueExpression(common.NewStringValue("Cat")),    // Category
		}
		rows = append(rows, row)
	}
	insertRows(t, th, rows, indexes)

	runScan := func(start int64, dir indexing.ScanDirection) []int64 {
		key := createKey(idxId, common.NewIntValue(start))
		node := planner.NewIndexScanNode(
			common.ObjectID(uint32(0)),
			th.oid,
			th.StorageSchema().GetFieldTypes(),
			dir,
			key,
			false,
		)
		exec := NewIndexScanExecutor(node, idxId, th)
		_ = exec.Init(NewExecutorContext(nil))

		var ids []int64
		for exec.Next() {
			ids = append(ids, exec.Current().GetValue(0).IntValue())
		}
		return ids
	}

	// --- FORWARD SCANS (>= StartKey) ---
	// Case 1: Exact Match Start
	// Start 10 -> Expect 10, 20, 30, 40
	assert.Equal(t, []int64{10, 20, 30, 40}, runScan(10, indexing.ScanDirectionForward), "Forward Exact Start")

	// Case 2: Gap Match (Inside Range)
	// Start 15 -> Expect 20, 30, 40
	assert.Equal(t, []int64{20, 30, 40}, runScan(15, indexing.ScanDirectionForward), "Forward Gap Start")

	// Case 3: Out of Range (Too High)
	// Start 50 -> Expect Empty
	assert.Equal(t, []int64(nil), runScan(50, indexing.ScanDirectionForward), "Forward Past End")

	// Case 4: Before All Values
	// Start -5 -> Expect 0, 10, 20, 30, 40
	assert.Equal(t, []int64{0, 10, 20, 30, 40}, runScan(-5, indexing.ScanDirectionForward), "Forward Before Start")

	// --- BACKWARD SCANS (<= StartKey) ---
	// Case 5: Exact Match Start
	// Start 30 -> Expect 30, 20, 10, 0
	assert.Equal(t, []int64{30, 20, 10, 0}, runScan(30, indexing.ScanDirectionBackward), "Backward Exact Start")

	// Case 6: Gap Match (Inside Range)
	// Start 25 -> Expect 20, 10, 0
	assert.Equal(t, []int64{20, 10, 0}, runScan(25, indexing.ScanDirectionBackward), "Backward Gap Start")

	// Case 7: Out of Range (Too Low)
	// Start -5 -> Expect Empty
	assert.Equal(t, []int64(nil), runScan(-5, indexing.ScanDirectionBackward), "Backward Past End")

	// Case 8: After All Values
	// Start 100 -> Expect 40, 30, 20, 10, 0
	assert.Equal(t, []int64{40, 30, 20, 10, 0}, runScan(100, indexing.ScanDirectionBackward), "Backward From After End")

	// Create a fresh empty table
	thEmpty := setupSalesTable(t, storage.NewBufferPool(20, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{}))
	idxIdEmpty := indexing.NewMemBTreeIndex(storage.NewRawTupleDesc([]common.Type{common.IntType}), []int{0})

	key := createKey(idxIdEmpty, common.NewIntValue(10))
	node := planner.NewIndexScanNode(
		common.ObjectID(uint32(0)),
		thEmpty.oid,
		thEmpty.StorageSchema().GetFieldTypes(),
		indexing.ScanDirectionForward,
		key,
		false,
	)
	execEmpty := NewIndexScanExecutor(node, idxIdEmpty, thEmpty)
	require.NoError(t, execEmpty.Init(NewExecutorContext(nil)))
	assert.False(t, execEmpty.Next(), "Scan on empty index should return false")
}
