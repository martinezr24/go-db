package execution

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"mit.edu/dsg/godb/catalog"
	"mit.edu/dsg/godb/common"
	"mit.edu/dsg/godb/indexing"
	"mit.edu/dsg/godb/planner"
	"mit.edu/dsg/godb/storage"
	"mit.edu/dsg/godb/transaction"
)

func createIndexNestedLoopJoin(leftExec Executor, rightTable *TableHeap, rightIndex indexing.Index, leftProbeKeys []planner.Expr) Executor {
	// The rightKeys argument in NewIndexNestedLoopJoinNode is actually "expressions from Left to probe the index"
	// based on the struct definition and usage in the executor.
	plan := planner.NewIndexNestedLoopJoinNode(
		leftExec.PlanNode(),
		rightTable.oid,
		common.ObjectID(0), // Dummy Index OID, as we pass the object directly
		leftProbeKeys,
		rightTable.StorageSchema().GetFieldTypes(),
		false,
	)
	return NewIndexJoinExecutor(plan, leftExec, rightIndex, rightTable)
}

func setUpIndexJoin(t *testing.T) (Executor, *TableHeap, *TableHeap, indexing.Index) {
	bp := storage.NewBufferPool(20, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{})
	sales := setupSalesTable(t, bp)
	shipment := setupShipmentTable(t, bp)

	// Create Hash Index on Shipment.sale_id (Column 1)
	idx := indexing.NewMemHashIndex(
		storage.NewRawTupleDesc([]common.Type{common.IntType}),
		[]int{1}, // sale_id is at index 1 in shipments
	)

	// Insert data and populate the index
	insertJoinData_Basic(t, sales, shipment, nil, []indexing.Index{idx})

	leftScan := NewSeqScanExecutor(
		planner.NewSeqScanNode(sales.oid, sales.StorageSchema().GetFieldTypes(), transaction.LockModeS),
		sales,
	)

	// Join Key: Sales.sale_id (Column 0 in Sales)
	leftKey := planner.NewColumnValueExpression(0, sales.StorageSchema().GetFieldTypes(), "sales.sale_id")

	join := createIndexNestedLoopJoin(leftScan, shipment, idx, []planner.Expr{leftKey})
	return join, sales, shipment, idx
}

// TestIndexNestedLoopJoin_BasicEqui tests an equality join (Sales.id = Shipments.sale_id) via Index Lookup.
func TestIndexNestedLoopJoin_BasicEqui(t *testing.T) {
	join, _, _, _ := setUpIndexJoin(t)
	expectedResults := []storage.Tuple{
		storage.FromValues(
			common.NewIntValue(1), common.NewIntValue(100), common.NewIntValue(50), common.NewStringValue("US"), common.NewStringValue("Clothing"),
			common.NewIntValue(101), common.NewIntValue(1), common.NewStringValue("FedEx"),
		),
		storage.FromValues(
			common.NewIntValue(3), common.NewIntValue(300), common.NewIntValue(150), common.NewStringValue("US"), common.NewStringValue("Books"),
			common.NewIntValue(102), common.NewIntValue(3), common.NewStringValue("UPS"),
		),
	}

	assert.NoError(t, join.Init(NewExecutorContext(nil)))
	checkJoinResult(t, join, expectedResults)

	// Restart
	assert.NoError(t, join.Init(NewExecutorContext(nil)))
	checkJoinResult(t, join, expectedResults)
	assert.NoError(t, join.Close())
}

// TestIndexNestedLoopJoin_Empty verifies that the join handles empty tables correctly.
func TestIndexNestedLoopJoin_Empty(t *testing.T) {
	// Scenario 1: Both tables empty
	bp := storage.NewBufferPool(20, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{})
	sales := setupSalesTable(t, bp)
	shipment := setupShipmentTable(t, bp)
	idx := indexing.NewMemHashIndex(storage.NewRawTupleDesc([]common.Type{common.IntType}), []int{1})

	leftScan := NewSeqScanExecutor(planner.NewSeqScanNode(sales.oid, sales.StorageSchema().GetFieldTypes(), transaction.LockModeS), sales)
	leftKey := planner.NewColumnValueExpression(0, sales.StorageSchema().GetFieldTypes(), "sales.sale_id")
	join := createIndexNestedLoopJoin(leftScan, shipment, idx, []planner.Expr{leftKey})

	require.NoError(t, join.Init(NewExecutorContext(nil)))
	count := 0
	for join.Next() {
		count++
	}
	assert.Equal(t, 0, count)
	join.Close()

	// Scenario 2: Right table empty (Index empty), Left has data
	insertSalesData(t, sales, nil, 5)
	require.NoError(t, join.Init(NewExecutorContext(nil)))
	count = 0
	for join.Next() {
		count++
	}
	assert.Equal(t, 0, count)
	join.Close()

	// Scenario 3: Left table empty, Right has data
	// (Recreate tables to clear left data cleanly)
	bp = storage.NewBufferPool(20, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{})
	sales = setupSalesTable(t, bp)
	shipment = setupShipmentTable(t, bp)
	idx = indexing.NewMemHashIndex(storage.NewRawTupleDesc([]common.Type{common.IntType}), []int{1})
	insertShipmentData(t, shipment, []indexing.Index{idx}, 5)

	leftScan = NewSeqScanExecutor(planner.NewSeqScanNode(sales.oid, sales.StorageSchema().GetFieldTypes(), transaction.LockModeS), sales)
	leftKey = planner.NewColumnValueExpression(0, sales.StorageSchema().GetFieldTypes(), "sales.sale_id")
	join = createIndexNestedLoopJoin(leftScan, shipment, idx, []planner.Expr{leftKey})

	require.NoError(t, join.Init(NewExecutorContext(nil)))
	count = 0
	for join.Next() {
		count++
	}
	assert.Equal(t, 0, count)
	join.Close()
}

// TestIndexNestedLoopJoin_Nulls verifies that NULLs in the join key do not match.
func TestIndexNestedLoopJoin_Nulls(t *testing.T) {
	bp := storage.NewBufferPool(20, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{})
	sales := setupSalesTable(t, bp)
	shipment := setupShipmentTable(t, bp)
	idx := indexing.NewMemHashIndex(storage.NewRawTupleDesc([]common.Type{common.IntType}), []int{1})

	// Sales (Left):
	// - 1: ID=1 (Valid)
	// - 2: ID=NULL (Should not trigger probe)
	salesRows := [][]planner.Expr{
		{planner.NewConstantValueExpression(common.NewIntValue(1)), planner.NewConstantValueExpression(common.NewIntValue(100)), planner.NewConstantValueExpression(common.NewIntValue(50)), planner.NewConstantValueExpression(common.NewStringValue("US")), planner.NewConstantValueExpression(common.NewStringValue("C"))},
		{planner.NewConstantValueExpression(common.NewNullInt()), planner.NewConstantValueExpression(common.NewIntValue(0)), planner.NewConstantValueExpression(common.NewIntValue(0)), planner.NewConstantValueExpression(common.NewStringValue("")), planner.NewConstantValueExpression(common.NewStringValue(""))},
	}
	insertRows(t, sales, salesRows, nil)

	// Shipments (Right):
	// - 1: Sale_ID=1 (Match)
	// - 2: Sale_ID=NULL (Should not match NULL)
	shipmentRows := [][]planner.Expr{
		{planner.NewConstantValueExpression(common.NewIntValue(101)), planner.NewConstantValueExpression(common.NewIntValue(1)), planner.NewConstantValueExpression(common.NewStringValue("FedEx"))},
		{planner.NewConstantValueExpression(common.NewIntValue(102)), planner.NewConstantValueExpression(common.NewNullInt()), planner.NewConstantValueExpression(common.NewStringValue("UPS"))},
	}
	insertRows(t, shipment, shipmentRows, []indexing.Index{idx})

	leftScan := NewSeqScanExecutor(planner.NewSeqScanNode(sales.oid, sales.StorageSchema().GetFieldTypes(), transaction.LockModeS), sales)
	leftKey := planner.NewColumnValueExpression(0, sales.StorageSchema().GetFieldTypes(), "sales.sale_id")
	join := createIndexNestedLoopJoin(leftScan, shipment, idx, []planner.Expr{leftKey})

	require.NoError(t, join.Init(NewExecutorContext(nil)))
	count := 0
	for join.Next() {
		count++
	}
	assert.Equal(t, 1, count, "Only 1=1 should match. NULLs should not match.")
	join.Close()
}

// TestIndexNestedLoopJoin_Duplicates verifies that the join logic handles a mix of duplicate and unique keys.
func TestIndexNestedLoopJoin_Duplicates(t *testing.T) {
	bp := storage.NewBufferPool(20, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{})
	sales := setupSalesTable(t, bp)
	shipment := setupShipmentTable(t, bp)
	idx := indexing.NewMemHashIndex(storage.NewRawTupleDesc([]common.Type{common.IntType}), []int{1})

	// --- Left Table (Sales) ---
	salesRows := [][]planner.Expr{
		{planner.NewConstantValueExpression(common.NewIntValue(1)), planner.NewConstantValueExpression(common.NewIntValue(100)), planner.NewConstantValueExpression(common.NewIntValue(50)), planner.NewConstantValueExpression(common.NewStringValue("US")), planner.NewConstantValueExpression(common.NewStringValue("C"))},
		{planner.NewConstantValueExpression(common.NewIntValue(1)), planner.NewConstantValueExpression(common.NewIntValue(100)), planner.NewConstantValueExpression(common.NewIntValue(50)), planner.NewConstantValueExpression(common.NewStringValue("US")), planner.NewConstantValueExpression(common.NewStringValue("C"))},
		{planner.NewConstantValueExpression(common.NewIntValue(1)), planner.NewConstantValueExpression(common.NewIntValue(100)), planner.NewConstantValueExpression(common.NewIntValue(50)), planner.NewConstantValueExpression(common.NewStringValue("US")), planner.NewConstantValueExpression(common.NewStringValue("C"))},
		{planner.NewConstantValueExpression(common.NewIntValue(2)), planner.NewConstantValueExpression(common.NewIntValue(200)), planner.NewConstantValueExpression(common.NewIntValue(100)), planner.NewConstantValueExpression(common.NewStringValue("EU")), planner.NewConstantValueExpression(common.NewStringValue("E"))},
		{planner.NewConstantValueExpression(common.NewIntValue(3)), planner.NewConstantValueExpression(common.NewIntValue(300)), planner.NewConstantValueExpression(common.NewIntValue(150)), planner.NewConstantValueExpression(common.NewStringValue("US")), planner.NewConstantValueExpression(common.NewStringValue("B"))},
	}
	insertRows(t, sales, salesRows, nil)

	// --- Right Table (Shipments) ---
	shipmentRows := [][]planner.Expr{
		{planner.NewConstantValueExpression(common.NewIntValue(101)), planner.NewConstantValueExpression(common.NewIntValue(1)), planner.NewConstantValueExpression(common.NewStringValue("FedEx"))},
		{planner.NewConstantValueExpression(common.NewIntValue(102)), planner.NewConstantValueExpression(common.NewIntValue(1)), planner.NewConstantValueExpression(common.NewStringValue("UPS"))},
		{planner.NewConstantValueExpression(common.NewIntValue(103)), planner.NewConstantValueExpression(common.NewIntValue(2)), planner.NewConstantValueExpression(common.NewStringValue("DHL"))},
		{planner.NewConstantValueExpression(common.NewIntValue(104)), planner.NewConstantValueExpression(common.NewIntValue(4)), planner.NewConstantValueExpression(common.NewStringValue("USPS"))},
	}
	insertRows(t, shipment, shipmentRows, []indexing.Index{idx})

	leftScan := NewSeqScanExecutor(planner.NewSeqScanNode(sales.oid, sales.StorageSchema().GetFieldTypes(), transaction.LockModeS), sales)
	leftKey := planner.NewColumnValueExpression(0, sales.StorageSchema().GetFieldTypes(), "sales.sale_id")
	join := createIndexNestedLoopJoin(leftScan, shipment, idx, []planner.Expr{leftKey})

	expectedResults := []storage.Tuple{
		// 3 Left (ID=1) * 2 Right (ID=1) = 6 Matches.
		storage.FromValues(common.NewIntValue(1), common.NewIntValue(100), common.NewIntValue(50), common.NewStringValue("US"), common.NewStringValue("C"), common.NewIntValue(101), common.NewIntValue(1), common.NewStringValue("FedEx")),
		storage.FromValues(common.NewIntValue(1), common.NewIntValue(100), common.NewIntValue(50), common.NewStringValue("US"), common.NewStringValue("C"), common.NewIntValue(101), common.NewIntValue(1), common.NewStringValue("FedEx")),
		storage.FromValues(common.NewIntValue(1), common.NewIntValue(100), common.NewIntValue(50), common.NewStringValue("US"), common.NewStringValue("C"), common.NewIntValue(101), common.NewIntValue(1), common.NewStringValue("FedEx")),
		storage.FromValues(common.NewIntValue(1), common.NewIntValue(100), common.NewIntValue(50), common.NewStringValue("US"), common.NewStringValue("C"), common.NewIntValue(102), common.NewIntValue(1), common.NewStringValue("UPS")),
		storage.FromValues(common.NewIntValue(1), common.NewIntValue(100), common.NewIntValue(50), common.NewStringValue("US"), common.NewStringValue("C"), common.NewIntValue(102), common.NewIntValue(1), common.NewStringValue("UPS")),
		storage.FromValues(common.NewIntValue(1), common.NewIntValue(100), common.NewIntValue(50), common.NewStringValue("US"), common.NewStringValue("C"), common.NewIntValue(102), common.NewIntValue(1), common.NewStringValue("UPS")),
		// 1 Left (ID=2) * 1 Right (ID=2) = 1 Match
		storage.FromValues(common.NewIntValue(2), common.NewIntValue(200), common.NewIntValue(100), common.NewStringValue("EU"), common.NewStringValue("E"), common.NewIntValue(103), common.NewIntValue(2), common.NewStringValue("DHL")),
	}

	require.NoError(t, join.Init(NewExecutorContext(nil)))
	checkJoinResult(t, join, expectedResults)
	join.Close()
}

// TestIndexNestedLoopJoin_Large tests the join on a dataset larger than a single page.
func TestIndexNestedLoopJoin_Large(t *testing.T) {
	numTuples := 1000
	bp := storage.NewBufferPool(50, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{})
	sales := setupSalesTable(t, bp)
	shipment := setupShipmentTable(t, bp)
	idx := indexing.NewMemHashIndex(storage.NewRawTupleDesc([]common.Type{common.IntType}), []int{1})

	insertSalesData(t, sales, nil, numTuples)
	insertShipmentData(t, shipment, []indexing.Index{idx}, numTuples)

	leftScan := NewSeqScanExecutor(planner.NewSeqScanNode(sales.oid, sales.StorageSchema().GetFieldTypes(), transaction.LockModeS), sales)
	leftKey := planner.NewColumnValueExpression(0, sales.StorageSchema().GetFieldTypes(), "sales.sale_id")
	join := createIndexNestedLoopJoin(leftScan, shipment, idx, []planner.Expr{leftKey})

	// Verification: Use SimpleNLJ with a predicate
	joinedSchema := append(sales.StorageSchema().GetFieldTypes(), shipment.StorageSchema().GetFieldTypes()...)
	leftColPred := planner.NewColumnValueExpression(0, joinedSchema, "sales.sale_id")
	rightColPred := planner.NewColumnValueExpression(6, joinedSchema, "shipment.sale_id")
	predicate := planner.NewComparisonExpression(leftColPred, rightColPred, planner.Equal)

	expected := simpleNestedLoopJoin(t, sales, shipment, predicate)

	require.NoError(t, join.Init(NewExecutorContext(nil)))
	checkJoinResult(t, join, expected)
	join.Close()
}

// TestIndexNestedLoopJoin_MultiKey verifies that the join works with composite keys.
func TestIndexNestedLoopJoin_MultiKey(t *testing.T) {
	bp := storage.NewBufferPool(20, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{})

	// T1 (Left)
	t1Schema := &catalog.Table{Oid: 100, Name: "t1", Columns: []catalog.Column{
		{Name: "id1", Type: common.IntType}, {Name: "id2", Type: common.IntType}, {Name: "val", Type: common.StringType},
	}}
	t1, err := NewTableHeap(t1Schema, bp, storage.NoopLogManager{}, nil)
	require.NoError(t, err)

	// T2 (Right)
	t2Schema := &catalog.Table{Oid: 101, Name: "t2", Columns: []catalog.Column{
		{Name: "id1", Type: common.IntType}, {Name: "id2", Type: common.IntType}, {Name: "val", Type: common.StringType},
	}}
	t2, err := NewTableHeap(t2Schema, bp, storage.NoopLogManager{}, nil)
	require.NoError(t, err)

	// Composite Index on T2(id1, id2) (Columns 0 and 1)
	idx := indexing.NewMemHashIndex(
		storage.NewRawTupleDesc([]common.Type{common.IntType, common.IntType}),
		[]int{0, 1},
	)

	// Insert Data
	t1Rows := [][]planner.Expr{
		{planner.NewConstantValueExpression(common.NewIntValue(1)), planner.NewConstantValueExpression(common.NewIntValue(10)), planner.NewConstantValueExpression(common.NewStringValue("Match"))},
		{planner.NewConstantValueExpression(common.NewIntValue(1)), planner.NewConstantValueExpression(common.NewIntValue(20)), planner.NewConstantValueExpression(common.NewStringValue("Mismatch"))},
	}
	insertRows(t, t1, t1Rows, nil)

	t2Rows := [][]planner.Expr{
		{planner.NewConstantValueExpression(common.NewIntValue(1)), planner.NewConstantValueExpression(common.NewIntValue(10)), planner.NewConstantValueExpression(common.NewStringValue("Match"))},
		{planner.NewConstantValueExpression(common.NewIntValue(1)), planner.NewConstantValueExpression(common.NewIntValue(30)), planner.NewConstantValueExpression(common.NewStringValue("NoMatch"))},
	}
	insertRows(t, t2, t2Rows, []indexing.Index{idx})

	leftScan := NewSeqScanExecutor(planner.NewSeqScanNode(t1.oid, t1.desc.GetFieldTypes(), transaction.LockModeS), t1)

	// Probe Keys: t1.id1, t1.id2
	probeKeys := []planner.Expr{
		planner.NewColumnValueExpression(0, t1.desc.GetFieldTypes(), "t1.id1"),
		planner.NewColumnValueExpression(1, t1.desc.GetFieldTypes(), "t1.id2"),
	}

	join := createIndexNestedLoopJoin(leftScan, t2, idx, probeKeys)

	// Expected: Only (1, 10) matches
	expectedResults := []storage.Tuple{
		storage.FromValues(common.NewIntValue(1), common.NewIntValue(10), common.NewStringValue("Match"), common.NewIntValue(1), common.NewIntValue(10), common.NewStringValue("Match")),
	}

	require.NoError(t, join.Init(NewExecutorContext(nil)))
	checkJoinResult(t, join, expectedResults)
	join.Close()
}
