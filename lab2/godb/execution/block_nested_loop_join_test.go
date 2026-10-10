package execution

import (
	"testing"

	"github.com/puzpuzpuz/xsync/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"mit.edu/dsg/godb/common"
	"mit.edu/dsg/godb/planner"
	"mit.edu/dsg/godb/storage"
	"mit.edu/dsg/godb/transaction"
)

func setUpJoin(t *testing.T, comparisonType planner.ComparisonType) Executor {
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

	joinedSchema := append(sales.StorageSchema().GetFieldTypes(), shipment.StorageSchema().GetFieldTypes()...)
	leftCol := planner.NewColumnValueExpression(0, joinedSchema, "sales.sale_id")
	rightCol := planner.NewColumnValueExpression(6, joinedSchema, "shipment.sale_id")
	predicate := planner.NewComparisonExpression(leftCol, rightCol, comparisonType)

	return NewBlockNestedLoopJoinExecutor(planner.NewBlockNestedLoopJoinNode(
		leftScan.PlanNode(),
		rightScan.PlanNode(),
		predicate,
	), leftScan, rightScan)
}

// TestBNLJ_BasicEqui tests an equality join (Sales.id = Shipments.sale_id).
// Expected Matches:
// - Sale 1 matches Ship 101
// - Sale 3 matches Ship 102
func TestBNLJ_BasicEqui(t *testing.T) {
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
	join := setUpJoin(t, planner.Equal)
	assert.NoError(t, join.Init(NewExecutorContext(nil)))
	checkJoinResult(t, join, expectedResults)

	// Check twice to ensure that init() works correctly when restarted
	assert.NoError(t, join.Init(NewExecutorContext(nil)))
	checkJoinResult(t, join, expectedResults)
	assert.NoError(t, join.Close())
}

// TestBNLJ_BasicLt tests a less-than join (Sales.id < Shipments.sale_id).
// Note: If the specific Index Nested Loop Join implementation only supports Equality,
// this test might fail or should be skipped for that specific executor.
func TestBNLJ_BasicLt(t *testing.T) {
	join := setUpJoin(t, planner.LessThan)
	expectedResults := []storage.Tuple{
		// 1 < 3
		storage.FromValues(
			common.NewIntValue(1), common.NewIntValue(100), common.NewIntValue(50), common.NewStringValue("US"), common.NewStringValue("Clothing"),
			common.NewIntValue(102), common.NewIntValue(3), common.NewStringValue("UPS"),
		),
		// 1 < 6
		storage.FromValues(
			common.NewIntValue(1), common.NewIntValue(100), common.NewIntValue(50), common.NewStringValue("US"), common.NewStringValue("Clothing"),
			common.NewIntValue(103), common.NewIntValue(6), common.NewStringValue("DHL"),
		),
		// 2 < 3
		storage.FromValues(
			common.NewIntValue(2), common.NewIntValue(200), common.NewIntValue(100), common.NewStringValue("EU"), common.NewStringValue("Electronics"),
			common.NewIntValue(102), common.NewIntValue(3), common.NewStringValue("UPS"),
		),
		// 2 < 6
		storage.FromValues(
			common.NewIntValue(2), common.NewIntValue(200), common.NewIntValue(100), common.NewStringValue("EU"), common.NewStringValue("Electronics"),
			common.NewIntValue(103), common.NewIntValue(6), common.NewStringValue("DHL"),
		),
		// 3 < 6
		storage.FromValues(
			common.NewIntValue(3), common.NewIntValue(300), common.NewIntValue(150), common.NewStringValue("US"), common.NewStringValue("Books"),
			common.NewIntValue(103), common.NewIntValue(6), common.NewStringValue("DHL"),
		),
		// 4 < 6
		storage.FromValues(
			common.NewIntValue(4), common.NewIntValue(400), common.NewIntValue(200), common.NewStringValue("EU"), common.NewStringValue("Clothing"),
			common.NewIntValue(103), common.NewIntValue(6), common.NewStringValue("DHL"),
		),
	}

	assert.NoError(t, join.Init(NewExecutorContext(nil)))
	checkJoinResult(t, join, expectedResults)
	assert.NoError(t, join.Close())
}

// TestBNLJ_Empty verifies that the join handles empty tables/indexes correctly.
func TestBNLJ_Empty(t *testing.T) {
	// Scenario 1: Both tables/indexes are empty
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

	joinedSchema := append(sales.StorageSchema().GetFieldTypes(), shipment.StorageSchema().GetFieldTypes()...)
	leftCol := planner.NewColumnValueExpression(0, joinedSchema, "sales.sale_id")
	rightCol := planner.NewColumnValueExpression(6, joinedSchema, "shipment.sale_id")
	predicate := planner.NewComparisonExpression(leftCol, rightCol, planner.Equal)

	joinTree := NewBlockNestedLoopJoinExecutor(planner.NewBlockNestedLoopJoinNode(
		leftScan.PlanNode(),
		rightScan.PlanNode(),
		predicate,
	), leftScan, rightScan)

	require.NoError(t, joinTree.Init(NewExecutorContext(nil)))
	count := 0
	for joinTree.Next() {
		count++
	}
	require.NoError(t, joinTree.Error())
	require.NoError(t, joinTree.Close())
	assert.Equal(t, 0, count, "Expected 0 results when both tables are empty")

	// Scenario 2: Right table is empty, Left table has data
	insertSalesData(t, sales, nil, 5)
	require.NoError(t, joinTree.Init(NewExecutorContext(nil)))
	count = 0
	for joinTree.Next() {
		count++
	}
	require.NoError(t, joinTree.Error())
	require.NoError(t, joinTree.Close())
	assert.Equal(t, 0, count, "Expected 0 results when right table is empty")

	// Scenario 3: Left table is empty, Right table has data
	bp = storage.NewBufferPool(20, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{})
	sales = setupSalesTable(t, bp)
	insertShipmentData(t, shipment, nil, 5)
	joinTree = NewBlockNestedLoopJoinExecutor(planner.NewBlockNestedLoopJoinNode(
		leftScan.PlanNode(),
		rightScan.PlanNode(),
		predicate,
	), NewSeqScanExecutor(
		planner.NewSeqScanNode(sales.oid, sales.StorageSchema().GetFieldTypes(), transaction.LockModeS),
		sales,
	), rightScan)
	require.NoError(t, joinTree.Init(NewExecutorContext(nil)))

	count = 0
	for joinTree.Next() {
		count++
	}
	require.NoError(t, joinTree.Error())
	require.NoError(t, joinTree.Close())

	assert.Equal(t, 0, count, "Expected 0 results when left table is empty")
}

// TestBNLJ_Nulls verifies that NULLs in the join key do not match.
func TestBNLJ_Nulls(t *testing.T) {
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

	joinedSchema := append(sales.StorageSchema().GetFieldTypes(), shipment.StorageSchema().GetFieldTypes()...)
	leftCol := planner.NewColumnValueExpression(0, joinedSchema, "sales.sale_id")
	rightCol := planner.NewColumnValueExpression(6, joinedSchema, "shipment.sale_id")
	predicate := planner.NewComparisonExpression(leftCol, rightCol, planner.Equal)

	joinTree := NewBlockNestedLoopJoinExecutor(planner.NewBlockNestedLoopJoinNode(
		leftScan.PlanNode(),
		rightScan.PlanNode(),
		predicate,
	), leftScan, rightScan)

	require.NoError(t, joinTree.Init(NewExecutorContext(nil)))
	count := 0
	for joinTree.Next() {
		count++
	}
	require.NoError(t, joinTree.Error())
	require.NoError(t, joinTree.Close())

	assert.Equal(t, 1, count, "Expected exactly 1 match (1=1). NULL=NULL should not match.")
}

// TestBNLJ_Duplicates verifies that the join logic handles a mix of duplicate and unique keys.
// Scenario:
// - key=1: 3 matches on Left * 2 matches on Right = 6 result rows
// - key=2: 1 match on Left  * 1 match on Right  = 1 result row
// - key=3: 1 row on Left, 0 on Right (No match)
// - key=4: 0 on Left, 1 on Right (No match)
func TestBNLJ_Duplicates(t *testing.T) {
	bp := storage.NewBufferPool(20, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{})
	sales := setupSalesTable(t, bp)
	shipment := setupShipmentTable(t, bp)

	// --- Left Table (Sales) ---
	// ID=1 (3 duplicates)
	// ID=2 (1 unique)
	// ID=3 (No match)
	salesRows := [][]planner.Expr{
		// 3 Duplicates of ID 1
		{planner.NewConstantValueExpression(common.NewIntValue(1)), planner.NewConstantValueExpression(common.NewIntValue(100)), planner.NewConstantValueExpression(common.NewIntValue(50)), planner.NewConstantValueExpression(common.NewStringValue("US")), planner.NewConstantValueExpression(common.NewStringValue("C"))},
		{planner.NewConstantValueExpression(common.NewIntValue(1)), planner.NewConstantValueExpression(common.NewIntValue(100)), planner.NewConstantValueExpression(common.NewIntValue(50)), planner.NewConstantValueExpression(common.NewStringValue("US")), planner.NewConstantValueExpression(common.NewStringValue("C"))},
		{planner.NewConstantValueExpression(common.NewIntValue(1)), planner.NewConstantValueExpression(common.NewIntValue(100)), planner.NewConstantValueExpression(common.NewIntValue(50)), planner.NewConstantValueExpression(common.NewStringValue("US")), planner.NewConstantValueExpression(common.NewStringValue("C"))},
		// 1 Unique ID 2
		{planner.NewConstantValueExpression(common.NewIntValue(2)), planner.NewConstantValueExpression(common.NewIntValue(200)), planner.NewConstantValueExpression(common.NewIntValue(100)), planner.NewConstantValueExpression(common.NewStringValue("EU")), planner.NewConstantValueExpression(common.NewStringValue("E"))},
		// 1 Unmatched ID 3
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

	joinedSchema := append(sales.StorageSchema().GetFieldTypes(), shipment.StorageSchema().GetFieldTypes()...)
	leftCol := planner.NewColumnValueExpression(0, joinedSchema, "sales.sale_id")
	rightCol := planner.NewColumnValueExpression(6, joinedSchema, "shipment.sale_id")
	predicate := planner.NewComparisonExpression(leftCol, rightCol, planner.Equal)

	joinTree := NewBlockNestedLoopJoinExecutor(planner.NewBlockNestedLoopJoinNode(
		leftScan.PlanNode(),
		rightScan.PlanNode(),
		predicate,
	), leftScan, rightScan)

	expectedResults := []storage.Tuple{
		// 3 Left (ID=1) * 2 Right (ID=1) = 6 Matches.
		// Left: {1, 100, 50, "US", "C"}
		// Right 1: {101, 1, "FedEx"}
		storage.FromValues(
			common.NewIntValue(1), common.NewIntValue(100), common.NewIntValue(50), common.NewStringValue("US"), common.NewStringValue("C"),
			common.NewIntValue(101), common.NewIntValue(1), common.NewStringValue("FedEx"),
		),
		storage.FromValues(
			common.NewIntValue(1), common.NewIntValue(100), common.NewIntValue(50), common.NewStringValue("US"), common.NewStringValue("C"),
			common.NewIntValue(101), common.NewIntValue(1), common.NewStringValue("FedEx"),
		),
		storage.FromValues(
			common.NewIntValue(1), common.NewIntValue(100), common.NewIntValue(50), common.NewStringValue("US"), common.NewStringValue("C"),
			common.NewIntValue(101), common.NewIntValue(1), common.NewStringValue("FedEx"),
		),
		// Right 2: {102, 1, "UPS"}
		storage.FromValues(
			common.NewIntValue(1), common.NewIntValue(100), common.NewIntValue(50), common.NewStringValue("US"), common.NewStringValue("C"),
			common.NewIntValue(102), common.NewIntValue(1), common.NewStringValue("UPS"),
		),
		storage.FromValues(
			common.NewIntValue(1), common.NewIntValue(100), common.NewIntValue(50), common.NewStringValue("US"), common.NewStringValue("C"),
			common.NewIntValue(102), common.NewIntValue(1), common.NewStringValue("UPS"),
		),
		storage.FromValues(
			common.NewIntValue(1), common.NewIntValue(100), common.NewIntValue(50), common.NewStringValue("US"), common.NewStringValue("C"),
			common.NewIntValue(102), common.NewIntValue(1), common.NewStringValue("UPS"),
		),
		// 1 Left (ID=2) * 1 Right (ID=2) = 1 Match
		// Left: {2, 200, 100, "EU", "E"}
		// Right: {103, 2, "DHL"}
		storage.FromValues(
			common.NewIntValue(2), common.NewIntValue(200), common.NewIntValue(100), common.NewStringValue("EU"), common.NewStringValue("E"),
			common.NewIntValue(103), common.NewIntValue(2), common.NewStringValue("DHL"),
		),
	}
	assert.NoError(t, joinTree.Init(NewExecutorContext(nil)))
	checkJoinResult(t, joinTree, expectedResults)
	assert.NoError(t, joinTree.Close())
}

// TestBNLJ_MultiBlock tests the join across block boundaries using deterministic data.
func TestBNLJ_MultiBlock(t *testing.T) {
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
	joinedSchema := append(sales.StorageSchema().GetFieldTypes(), shipment.StorageSchema().GetFieldTypes()...)
	leftCol := planner.NewColumnValueExpression(0, joinedSchema, "sales.sale_id")
	rightCol := planner.NewColumnValueExpression(6, joinedSchema, "shipment.sale_id")
	predicate := planner.NewComparisonExpression(leftCol, rightCol, planner.Equal)

	joinTree := NewBlockNestedLoopJoinExecutor(planner.NewBlockNestedLoopJoinNode(
		leftScan.PlanNode(),
		rightScan.PlanNode(),
		predicate,
	), leftScan, rightScan)

	expected := simpleNestedLoopJoin(t, sales, shipment, predicate)

	assert.NoError(t, joinTree.Init(NewExecutorContext(nil)))
	checkJoinResult(t, joinTree, expected)
	assert.NoError(t, joinTree.Close())
}

// TestBNLJ_ScanCount_Optimization verifies that the Block Nested Loop Join reduces I/O access
// compared to a Tuple-Based Nested Loop Join.
func TestBNLJ_ScanCount_Optimization(t *testing.T) {
	numTuples := 2000
	numRightTuples := 200

	rootPath := t.TempDir()
	realSm := storage.NewDiskStorageManager(rootPath)
	statsSm := &storage.StatsDBFileManager{
		Inner: realSm,
		Files: xsync.NewMapOf[common.ObjectID, *storage.StatsDBFile](),
	}

	bp := storage.NewBufferPool(2, statsSm, storage.NoopLogManager{})
	sales := setupSalesTable(t, bp)
	shipment := setupShipmentTable(t, bp)

	insertSalesData(t, sales, nil, numTuples)
	insertShipmentData(t, shipment, nil, numRightTuples)

	statsFile, ok := statsSm.Files.Load(shipment.oid)
	require.True(t, ok)
	statsFile.ReadCnt.Store(0)

	leftScan := NewSeqScanExecutor(
		planner.NewSeqScanNode(sales.oid, sales.StorageSchema().GetFieldTypes(), transaction.LockModeS),
		sales,
	)
	rightScan := NewSeqScanExecutor(
		planner.NewSeqScanNode(shipment.oid, shipment.StorageSchema().GetFieldTypes(), transaction.LockModeS),
		shipment,
	)

	joinedSchema := append(sales.StorageSchema().GetFieldTypes(), shipment.StorageSchema().GetFieldTypes()...)
	leftCol := planner.NewColumnValueExpression(0, joinedSchema, "sales.sale_id")
	rightCol := planner.NewColumnValueExpression(6, joinedSchema, "shipment.sale_id")
	predicate := planner.NewComparisonExpression(leftCol, rightCol, planner.Equal)

	joinTree := NewBlockNestedLoopJoinExecutor(planner.NewBlockNestedLoopJoinNode(
		leftScan.PlanNode(),
		rightScan.PlanNode(),
		predicate,
	), leftScan, rightScan)

	require.NoError(t, joinTree.Init(NewExecutorContext(nil)))
	for joinTree.Next() {
	}
	require.NoError(t, joinTree.Close())

	// Analysis by approximating the number of reads using blocks
	tuplesPerBlock := blockSize / sales.desc.BytesPerTuple()
	numBlocks := numTuples / tuplesPerBlock
	// Use an estimated 95% storage efficiency to account for metadata and padding
	leftTuplesPerPage := common.PageSize * 95 / 100 / sales.desc.BytesPerTuple()
	rightTuplesPerPage := common.PageSize * 95 / 100 / shipment.desc.BytesPerTuple()
	readPerBlock := tuplesPerBlock/leftTuplesPerPage + numRightTuples/rightTuplesPerPage
	expectedReads := numBlocks * readPerBlock
	actualReads := statsFile.ReadCnt.Load()
	assert.Less(t, int(actualReads), expectedReads, "BNLJ did not reduce disk I/O sufficiently compared to simple-NLJ!")
}
