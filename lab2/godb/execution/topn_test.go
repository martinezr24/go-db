package execution

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"mit.edu/dsg/godb/common"
	"mit.edu/dsg/godb/planner"
	"mit.edu/dsg/godb/storage"
	"mit.edu/dsg/godb/transaction"
)

func TestTopN_Ascending(t *testing.T) {
	bp := storage.NewBufferPool(20, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{})
	sales := setupSalesTable(t, bp)

	// Insert data: 0, 200, 400, 600, 800
	insertSalesData(t, sales, nil, 5)

	scanNode := planner.NewSeqScanNode(sales.oid, sales.StorageSchema().GetFieldTypes(), transaction.LockModeS)
	scanExec := NewSeqScanExecutor(scanNode, sales)

	// Sort by Revenue Ascending (Index 1)
	orderBy := []planner.OrderByClause{
		{
			Expr:      planner.NewColumnValueExpression(1, sales.StorageSchema().GetFieldTypes(), "revenue"),
			Direction: planner.SortOrderAscending,
		},
	}

	// Limit 3
	limit := 3
	topNNode := planner.NewTopNNode(scanNode, limit, orderBy)
	topNExec := NewTopNExecutor(topNNode, scanExec)

	require.NoError(t, topNExec.Init(NewExecutorContext(nil)))

	// Expected Order: 0, 200, 400
	expectedRevenues := []int64{0, 200, 400}
	count := 0
	for topNExec.Next() {
		tuple := topNExec.Current()
		rev := tuple.GetValue(1).IntValue()
		assert.Equal(t, expectedRevenues[count], rev)
		count++
	}

	assert.Equal(t, limit, count)
	require.NoError(t, topNExec.Error())
	require.NoError(t, topNExec.Close())
}

func TestTopN_Descending(t *testing.T) {
	bp := storage.NewBufferPool(20, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{})
	sales := setupSalesTable(t, bp)
	insertSalesData(t, sales, nil, 5) // 0, 200, 400, 600, 800

	scanNode := planner.NewSeqScanNode(sales.oid, sales.StorageSchema().GetFieldTypes(), transaction.LockModeS)
	scanExec := NewSeqScanExecutor(scanNode, sales)

	// Sort by Revenue Descending
	orderBy := []planner.OrderByClause{
		{
			Expr:      planner.NewColumnValueExpression(1, sales.StorageSchema().GetFieldTypes(), "revenue"),
			Direction: planner.SortOrderDescending,
		},
	}

	// Limit 3
	limit := 3
	topNNode := planner.NewTopNNode(scanNode, limit, orderBy)
	topNExec := NewTopNExecutor(topNNode, scanExec)

	require.NoError(t, topNExec.Init(NewExecutorContext(nil)))

	// Expected Order: 800, 600, 400
	expectedRevenues := []int64{800, 600, 400}
	count := 0
	for topNExec.Next() {
		tuple := topNExec.Current()
		rev := tuple.GetValue(1).IntValue()
		assert.Equal(t, expectedRevenues[count], rev)
		count++
	}
	assert.Equal(t, limit, count)
	require.NoError(t, topNExec.Close())
}

func TestTopN_MultiColumn(t *testing.T) {
	bp := storage.NewBufferPool(20, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{})
	sales := setupSalesTable(t, bp)

	// Custom data for multi-column sort
	// Schema: (sale_id, revenue, cost, region, category)
	rows := [][]planner.Expr{
		{ // US, 100
			planner.NewConstantValueExpression(common.NewIntValue(1)),
			planner.NewConstantValueExpression(common.NewIntValue(100)),
			planner.NewConstantValueExpression(common.NewIntValue(0)),
			planner.NewConstantValueExpression(common.NewStringValue("US")),
			planner.NewConstantValueExpression(common.NewStringValue("C")),
		},
		{ // EU, 300
			planner.NewConstantValueExpression(common.NewIntValue(2)),
			planner.NewConstantValueExpression(common.NewIntValue(300)),
			planner.NewConstantValueExpression(common.NewIntValue(0)),
			planner.NewConstantValueExpression(common.NewStringValue("EU")),
			planner.NewConstantValueExpression(common.NewStringValue("C")),
		},
		{ // US, 500
			planner.NewConstantValueExpression(common.NewIntValue(3)),
			planner.NewConstantValueExpression(common.NewIntValue(500)),
			planner.NewConstantValueExpression(common.NewIntValue(0)),
			planner.NewConstantValueExpression(common.NewStringValue("US")),
			planner.NewConstantValueExpression(common.NewStringValue("C")),
		},
		{ // EU, 50
			planner.NewConstantValueExpression(common.NewIntValue(4)),
			planner.NewConstantValueExpression(common.NewIntValue(50)),
			planner.NewConstantValueExpression(common.NewIntValue(0)),
			planner.NewConstantValueExpression(common.NewStringValue("EU")),
			planner.NewConstantValueExpression(common.NewStringValue("C")),
		},
	}
	insertRows(t, sales, rows, nil)

	scanNode := planner.NewSeqScanNode(sales.oid, sales.StorageSchema().GetFieldTypes(), transaction.LockModeS)
	scanExec := NewSeqScanExecutor(scanNode, sales)

	// Sort: Region ASC (Index 3), then Revenue DESC (Index 1)
	orderBy := []planner.OrderByClause{
		{
			Expr:      planner.NewColumnValueExpression(3, sales.StorageSchema().GetFieldTypes(), "region"),
			Direction: planner.SortOrderAscending,
		},
		{
			Expr:      planner.NewColumnValueExpression(1, sales.StorageSchema().GetFieldTypes(), "revenue"),
			Direction: planner.SortOrderDescending,
		},
	}

	// Full Sorted Order:
	// 1. EU, 300
	// 2. EU, 50
	// 3. US, 500
	// 4. US, 100

	// Limit 2 -> Should just get the EU rows
	limit := 2
	topNNode := planner.NewTopNNode(scanNode, limit, orderBy)
	topNExec := NewTopNExecutor(topNNode, scanExec)

	require.NoError(t, topNExec.Init(NewExecutorContext(nil)))

	type Expected struct {
		Region  string
		Revenue int64
	}
	expected := []Expected{
		{"EU", 300},
		{"EU", 50},
	}

	count := 0
	for topNExec.Next() {
		tuple := topNExec.Current()
		rev := tuple.GetValue(1).IntValue()
		reg := tuple.GetValue(3).StringValue()
		assert.Equal(t, expected[count].Region, reg)
		assert.Equal(t, expected[count].Revenue, rev)
		count++
	}
	assert.Equal(t, limit, count)
	require.NoError(t, topNExec.Close())
}

func TestTopN_LimitLargerThanInput(t *testing.T) {
	bp := storage.NewBufferPool(20, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{})
	sales := setupSalesTable(t, bp)
	// Insert 5 rows
	insertSalesData(t, sales, nil, 5)

	scanNode := planner.NewSeqScanNode(sales.oid, sales.StorageSchema().GetFieldTypes(), transaction.LockModeS)
	scanExec := NewSeqScanExecutor(scanNode, sales)

	orderBy := []planner.OrderByClause{
		{
			Expr:      planner.NewColumnValueExpression(1, sales.StorageSchema().GetFieldTypes(), "revenue"),
			Direction: planner.SortOrderAscending,
		},
	}

	// Limit 10 (Input size is 5)
	limit := 10
	topNNode := planner.NewTopNNode(scanNode, limit, orderBy)
	topNExec := NewTopNExecutor(topNNode, scanExec)

	require.NoError(t, topNExec.Init(NewExecutorContext(nil)))

	// Expected Order: 0, 200, 400, 600, 800
	count := 0
	expectedRev := int64(0)
	for topNExec.Next() {
		tuple := topNExec.Current()
		rev := tuple.GetValue(1).IntValue()
		assert.Equal(t, expectedRev, rev)
		expectedRev += 200
		count++
	}
	// Should retrieve all 5
	assert.Equal(t, 5, count)
	require.NoError(t, topNExec.Close())
}

func TestTopN_Empty(t *testing.T) {
	bp := storage.NewBufferPool(20, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{})
	sales := setupSalesTable(t, bp)
	// No data inserted

	scanNode := planner.NewSeqScanNode(sales.oid, sales.StorageSchema().GetFieldTypes(), transaction.LockModeS)
	scanExec := NewSeqScanExecutor(scanNode, sales)

	orderBy := []planner.OrderByClause{
		{
			Expr:      planner.NewColumnValueExpression(1, sales.StorageSchema().GetFieldTypes(), "revenue"),
			Direction: planner.SortOrderAscending,
		},
	}

	limit := 5
	topNNode := planner.NewTopNNode(scanNode, limit, orderBy)
	topNExec := NewTopNExecutor(topNNode, scanExec)

	require.NoError(t, topNExec.Init(NewExecutorContext(nil)))

	count := 0
	for topNExec.Next() {
		count++
	}
	assert.Equal(t, 0, count)
	require.NoError(t, topNExec.Close())
}
