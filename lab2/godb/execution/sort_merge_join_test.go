package execution

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"mit.edu/dsg/godb/catalog"
	"mit.edu/dsg/godb/common"
	"mit.edu/dsg/godb/planner"
	"mit.edu/dsg/godb/storage"
	"mit.edu/dsg/godb/transaction"
)

func createSortMergeJoin(leftExec Executor, rightExec Executor, leftKeys []planner.Expr, rightKeys []planner.Expr) Executor {
	// Wrap Left in Sort
	var leftOrderBy []planner.OrderByClause
	for _, k := range leftKeys {
		leftOrderBy = append(leftOrderBy, planner.OrderByClause{Expr: k, Direction: planner.SortOrderAscending})
	}
	leftSort := NewSortExecutor(planner.NewSortNode(leftExec.PlanNode(), leftOrderBy), leftExec)

	// Wrap Right in Sort
	var rightOrderBy []planner.OrderByClause
	for _, k := range rightKeys {
		rightOrderBy = append(rightOrderBy, planner.OrderByClause{Expr: k, Direction: planner.SortOrderAscending})
	}
	rightSort := NewSortExecutor(planner.NewSortNode(rightExec.PlanNode(), rightOrderBy), rightExec)

	plan := planner.NewSortMergeJoinNode(leftSort.PlanNode(), rightSort.PlanNode(), leftKeys, rightKeys)
	return NewSortMergeJoinExecutor(plan, leftSort, rightSort)
}

func setUpSortMergeJoin(t *testing.T) Executor {
	bp := storage.NewBufferPool(20, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{})
	sales := setupSalesTable(t, bp)
	shipment := setupShipmentTable(t, bp)
	insertJoinData_Basic(t, sales, shipment, nil, nil)
	leftScan := NewSeqScanExecutor(
		planner.NewSeqScanNode(sales.oid, sales.StorageSchema().GetFieldTypes(), transaction.LockModeS),
		sales,
	)
	rightScan := NewSeqScanExecutor(
		planner.NewSeqScanNode(shipment.oid, shipment.StorageSchema().GetFieldTypes(), transaction.LockModeS),
		shipment,
	)

	leftKey := planner.NewColumnValueExpression(0, sales.StorageSchema().GetFieldTypes(), "sales.sale_id")
	rightKey := planner.NewColumnValueExpression(1, shipment.StorageSchema().GetFieldTypes(), "shipment.sale_id")

	return createSortMergeJoin(leftScan, rightScan, []planner.Expr{leftKey}, []planner.Expr{rightKey})
}

// TestSortMergeJoin_BasicEqui tests an equality join (Sales.id = Shipments.sale_id).
func TestSortMergeJoin_BasicEqui(t *testing.T) {
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
	join := setUpSortMergeJoin(t)
	assert.NoError(t, join.Init(NewExecutorContext(nil)))
	checkJoinResult(t, join, expectedResults)

	// Check twice to ensure that init() works correctly when restarted
	assert.NoError(t, join.Init(NewExecutorContext(nil)))
	checkJoinResult(t, join, expectedResults)
	assert.NoError(t, join.Close())
}

// TestSortMergeJoin_Empty verifies that the join handles empty tables correctly.
func TestSortMergeJoin_Empty(t *testing.T) {
	bp := storage.NewBufferPool(20, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{})
	sales := setupSalesTable(t, bp)
	shipment := setupShipmentTable(t, bp)
	leftScan := NewSeqScanExecutor(
		planner.NewSeqScanNode(sales.oid, sales.StorageSchema().GetFieldTypes(), transaction.LockModeS),
		sales,
	)
	rightScan := NewSeqScanExecutor(
		planner.NewSeqScanNode(shipment.oid, shipment.StorageSchema().GetFieldTypes(), transaction.LockModeS),
		shipment,
	)

	leftKey := planner.NewColumnValueExpression(0, sales.StorageSchema().GetFieldTypes(), "sales.sale_id")
	rightKey := planner.NewColumnValueExpression(1, shipment.StorageSchema().GetFieldTypes(), "shipment.sale_id")

	joinTree := createSortMergeJoin(leftScan, rightScan, []planner.Expr{leftKey}, []planner.Expr{rightKey})

	require.NoError(t, joinTree.Init(NewExecutorContext(nil)))
	count := 0
	for joinTree.Next() {
		count++
	}
	require.NoError(t, joinTree.Error())
	require.NoError(t, joinTree.Close())
	assert.Equal(t, 0, count)

	// Scenario 2: Right table empty, Left table has data
	insertSalesData(t, sales, nil, 5)
	require.NoError(t, joinTree.Init(NewExecutorContext(nil)))
	count = 0
	for joinTree.Next() {
		count++
	}
	require.NoError(t, joinTree.Error())
	require.NoError(t, joinTree.Close())
	assert.Equal(t, 0, count)

	// Scenario 3: Left table empty, Right table has data
	bp = storage.NewBufferPool(20, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{})
	sales = setupSalesTable(t, bp)
	insertShipmentData(t, shipment, nil, 5)
	// Re-create executors attached to new BP/Tables
	leftScan = NewSeqScanExecutor(
		planner.NewSeqScanNode(sales.oid, sales.StorageSchema().GetFieldTypes(), transaction.LockModeS),
		sales,
	)
	rightScan = NewSeqScanExecutor(
		planner.NewSeqScanNode(shipment.oid, shipment.StorageSchema().GetFieldTypes(), transaction.LockModeS),
		shipment,
	)
	joinTree = createSortMergeJoin(leftScan, rightScan, []planner.Expr{leftKey}, []planner.Expr{rightKey})

	require.NoError(t, joinTree.Init(NewExecutorContext(nil)))
	count = 0
	for joinTree.Next() {
		count++
	}
	require.NoError(t, joinTree.Error())
	require.NoError(t, joinTree.Close())
	assert.Equal(t, 0, count)
}

// TestSortMergeJoin_Nulls verifies that NULLs in the join key do not match.
func TestSortMergeJoin_Nulls(t *testing.T) {
	bp := storage.NewBufferPool(20, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{})
	sales := setupSalesTable(t, bp)
	shipment := setupShipmentTable(t, bp)

	// Sales (Left):
	// - 1: ID=1 (Valid)
	// - 2: ID=NULL (Should not match anything)
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
	insertRows(t, shipment, shipmentRows, nil)

	leftScan := NewSeqScanExecutor(
		planner.NewSeqScanNode(sales.oid, sales.StorageSchema().GetFieldTypes(), transaction.LockModeS),
		sales,
	)
	rightScan := NewSeqScanExecutor(
		planner.NewSeqScanNode(shipment.oid, shipment.StorageSchema().GetFieldTypes(), transaction.LockModeS),
		shipment,
	)

	leftKey := planner.NewColumnValueExpression(0, sales.StorageSchema().GetFieldTypes(), "sales.sale_id")
	rightKey := planner.NewColumnValueExpression(1, shipment.StorageSchema().GetFieldTypes(), "shipment.sale_id")

	joinTree := createSortMergeJoin(leftScan, rightScan, []planner.Expr{leftKey}, []planner.Expr{rightKey})

	require.NoError(t, joinTree.Init(NewExecutorContext(nil)))
	count := 0
	for joinTree.Next() {
		count++
	}
	require.NoError(t, joinTree.Error())
	require.NoError(t, joinTree.Close())

	assert.Equal(t, 1, count, "Expected exactly 1 match (1=1). NULL=NULL should not match.")
}

// TestSortMergeJoin_Duplicates verifies that the join logic handles a mix of duplicate and unique keys.
func TestSortMergeJoin_Duplicates(t *testing.T) {
	bp := storage.NewBufferPool(20, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{})
	sales := setupSalesTable(t, bp)
	shipment := setupShipmentTable(t, bp)

	// --- Left Table (Sales) ---
	// ID=1 (3 duplicates)
	// ID=2 (1 unique)
	// ID=3 (No match)
	salesRows := [][]planner.Expr{
		{planner.NewConstantValueExpression(common.NewIntValue(1)), planner.NewConstantValueExpression(common.NewIntValue(100)), planner.NewConstantValueExpression(common.NewIntValue(50)), planner.NewConstantValueExpression(common.NewStringValue("US")), planner.NewConstantValueExpression(common.NewStringValue("C"))},
		{planner.NewConstantValueExpression(common.NewIntValue(1)), planner.NewConstantValueExpression(common.NewIntValue(100)), planner.NewConstantValueExpression(common.NewIntValue(50)), planner.NewConstantValueExpression(common.NewStringValue("US")), planner.NewConstantValueExpression(common.NewStringValue("C"))},
		{planner.NewConstantValueExpression(common.NewIntValue(1)), planner.NewConstantValueExpression(common.NewIntValue(100)), planner.NewConstantValueExpression(common.NewIntValue(50)), planner.NewConstantValueExpression(common.NewStringValue("US")), planner.NewConstantValueExpression(common.NewStringValue("C"))},
		{planner.NewConstantValueExpression(common.NewIntValue(2)), planner.NewConstantValueExpression(common.NewIntValue(200)), planner.NewConstantValueExpression(common.NewIntValue(100)), planner.NewConstantValueExpression(common.NewStringValue("EU")), planner.NewConstantValueExpression(common.NewStringValue("E"))},
		{planner.NewConstantValueExpression(common.NewIntValue(3)), planner.NewConstantValueExpression(common.NewIntValue(300)), planner.NewConstantValueExpression(common.NewIntValue(150)), planner.NewConstantValueExpression(common.NewStringValue("US")), planner.NewConstantValueExpression(common.NewStringValue("B"))},
	}
	insertRows(t, sales, salesRows, nil)

	// --- Right Table (Shipments) ---
	// Sale_ID=1 (2 duplicates)
	// Sale_ID=2 (1 unique)
	// Sale_ID=4 (No match)
	shipmentRows := [][]planner.Expr{
		{planner.NewConstantValueExpression(common.NewIntValue(101)), planner.NewConstantValueExpression(common.NewIntValue(1)), planner.NewConstantValueExpression(common.NewStringValue("FedEx"))},
		{planner.NewConstantValueExpression(common.NewIntValue(102)), planner.NewConstantValueExpression(common.NewIntValue(1)), planner.NewConstantValueExpression(common.NewStringValue("UPS"))},
		{planner.NewConstantValueExpression(common.NewIntValue(103)), planner.NewConstantValueExpression(common.NewIntValue(2)), planner.NewConstantValueExpression(common.NewStringValue("DHL"))},
		{planner.NewConstantValueExpression(common.NewIntValue(104)), planner.NewConstantValueExpression(common.NewIntValue(4)), planner.NewConstantValueExpression(common.NewStringValue("USPS"))},
	}
	insertRows(t, shipment, shipmentRows, nil)

	leftScan := NewSeqScanExecutor(
		planner.NewSeqScanNode(sales.oid, sales.StorageSchema().GetFieldTypes(), transaction.LockModeS),
		sales,
	)
	rightScan := NewSeqScanExecutor(
		planner.NewSeqScanNode(shipment.oid, shipment.StorageSchema().GetFieldTypes(), transaction.LockModeS),
		shipment,
	)

	leftKey := planner.NewColumnValueExpression(0, sales.StorageSchema().GetFieldTypes(), "sales.sale_id")
	rightKey := planner.NewColumnValueExpression(1, shipment.StorageSchema().GetFieldTypes(), "shipment.sale_id")

	joinTree := createSortMergeJoin(leftScan, rightScan, []planner.Expr{leftKey}, []planner.Expr{rightKey})

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
	assert.NoError(t, joinTree.Init(NewExecutorContext(nil)))
	checkJoinResult(t, joinTree, expectedResults)
	assert.NoError(t, joinTree.Close())
}

// TestSortMergeJoin_Large tests the join on a dataset larger than a single page.
func TestSortMergeJoin_Large(t *testing.T) {
	numTuples := 2000
	bp := storage.NewBufferPool(50, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{})
	sales := setupSalesTable(t, bp)
	shipment := setupShipmentTable(t, bp)

	insertSalesData(t, sales, nil, numTuples)
	insertShipmentData(t, shipment, nil, numTuples)

	leftScan := NewSeqScanExecutor(
		planner.NewSeqScanNode(sales.oid, sales.StorageSchema().GetFieldTypes(), transaction.LockModeS),
		sales,
	)
	rightScan := NewSeqScanExecutor(
		planner.NewSeqScanNode(shipment.oid, shipment.StorageSchema().GetFieldTypes(), transaction.LockModeS),
		shipment,
	)

	leftKey := planner.NewColumnValueExpression(0, sales.StorageSchema().GetFieldTypes(), "sales.sale_id")
	rightKey := planner.NewColumnValueExpression(1, shipment.StorageSchema().GetFieldTypes(), "shipment.sale_id")
	// For validation using SimpleNLJ, we still need a predicate
	joinedSchema := append(sales.StorageSchema().GetFieldTypes(), shipment.StorageSchema().GetFieldTypes()...)
	leftColPred := planner.NewColumnValueExpression(0, joinedSchema, "sales.sale_id")
	rightColPred := planner.NewColumnValueExpression(6, joinedSchema, "shipment.sale_id")
	predicate := planner.NewComparisonExpression(leftColPred, rightColPred, planner.Equal)

	joinTree := createSortMergeJoin(leftScan, rightScan, []planner.Expr{leftKey}, []planner.Expr{rightKey})

	expected := simpleNestedLoopJoin(t, sales, shipment, predicate)

	assert.NoError(t, joinTree.Init(NewExecutorContext(nil)))
	checkJoinResult(t, joinTree, expected)
	assert.NoError(t, joinTree.Close())
}

// TestSortMergeJoin_MultiKey verifies that the join works with composite keys (e.g. JOIN ON a=x AND b=y)
func TestSortMergeJoin_MultiKey(t *testing.T) {
	bp := storage.NewBufferPool(20, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{})

	t1Schema := &catalog.Table{
		Oid:  100,
		Name: "t1",
		Columns: []catalog.Column{
			{Name: "id1", Type: common.IntType},
			{Name: "id2", Type: common.IntType},
			{Name: "val", Type: common.StringType},
		},
	}
	t1, err := NewTableHeap(t1Schema, bp, storage.NoopLogManager{}, nil)
	require.NoError(t, err)

	t2Schema := &catalog.Table{
		Oid:  101,
		Name: "t2",
		Columns: []catalog.Column{
			{Name: "id1", Type: common.IntType},
			{Name: "id2", Type: common.IntType},
			{Name: "val", Type: common.StringType},
		},
	}
	t2, err := NewTableHeap(t2Schema, bp, storage.NoopLogManager{}, nil)
	require.NoError(t, err)

	t1Rows := [][]planner.Expr{
		{planner.NewConstantValueExpression(common.NewIntValue(1)), planner.NewConstantValueExpression(common.NewIntValue(10)), planner.NewConstantValueExpression(common.NewStringValue("Match"))},
		{planner.NewConstantValueExpression(common.NewIntValue(1)), planner.NewConstantValueExpression(common.NewIntValue(20)), planner.NewConstantValueExpression(common.NewStringValue("Mismatch_T2"))},
		{planner.NewConstantValueExpression(common.NewIntValue(2)), planner.NewConstantValueExpression(common.NewIntValue(10)), planner.NewConstantValueExpression(common.NewStringValue("Mismatch_T1"))},
	}
	insertRows(t, t1, t1Rows, nil)
	t2Rows := [][]planner.Expr{
		{planner.NewConstantValueExpression(common.NewIntValue(1)), planner.NewConstantValueExpression(common.NewIntValue(10)), planner.NewConstantValueExpression(common.NewStringValue("Match"))},
		{planner.NewConstantValueExpression(common.NewIntValue(1)), planner.NewConstantValueExpression(common.NewIntValue(30)), planner.NewConstantValueExpression(common.NewStringValue("NoMatch"))},
	}
	insertRows(t, t2, t2Rows, nil)

	leftScan := NewSeqScanExecutor(planner.NewSeqScanNode(t1.oid, t1.desc.GetFieldTypes(), transaction.LockModeS), t1)
	rightScan := NewSeqScanExecutor(planner.NewSeqScanNode(t2.oid, t2.desc.GetFieldTypes(), transaction.LockModeS), t2)

	// Join Keys: (t1.id1, t1.id2) = (t2.id1, t2.id2)
	leftKeys := []planner.Expr{
		planner.NewColumnValueExpression(0, t1.desc.GetFieldTypes(), "t1.id1"),
		planner.NewColumnValueExpression(1, t1.desc.GetFieldTypes(), "t1.id2"),
	}
	rightKeys := []planner.Expr{
		planner.NewColumnValueExpression(0, t2.desc.GetFieldTypes(), "t2.id1"),
		planner.NewColumnValueExpression(1, t2.desc.GetFieldTypes(), "t2.id2"),
	}

	joinTree := createSortMergeJoin(leftScan, rightScan, leftKeys, rightKeys)

	// Expected Result: Only (1, 10) matches.
	expectedResults := []storage.Tuple{
		storage.FromValues(
			common.NewIntValue(1), common.NewIntValue(10), common.NewStringValue("Match"),
			common.NewIntValue(1), common.NewIntValue(10), common.NewStringValue("Match"),
		),
	}

	// Execute and Verify
	require.NoError(t, joinTree.Init(NewExecutorContext(nil)))
	checkJoinResult(t, joinTree, expectedResults)
	require.NoError(t, joinTree.Close())
}
