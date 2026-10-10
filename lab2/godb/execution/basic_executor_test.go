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

func TestBasicExecutor_SeqScan(t *testing.T) {
	th := setupSalesTable(t, storage.NewBufferPool(20, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{}))
	insertSalesData(t, th, nil, 10) // No indexes needed for Scan tests

	// Scan all columns
	scanNode := planner.NewSeqScanNode(th.oid, th.StorageSchema().GetFieldTypes(), transaction.LockModeS)
	scanExec := NewSeqScanExecutor(scanNode, th)
	require.NoError(t, scanExec.Init(NewExecutorContext(nil)))

	count := 0
	for scanExec.Next() {
		tup := scanExec.Current()

		// Verify Id
		assert.Equal(t, int64(count), tup.GetValue(0).IntValue())

		// Verify Revenue (i * 200)
		assert.Equal(t, int64(count*200), tup.GetValue(1).IntValue())

		// Verify Region logic (Even=US, Odd=EU)
		expectedRegion := "US"
		if count%2 != 0 {
			expectedRegion = "EU"
		}
		assert.Equal(t, expectedRegion, tup.GetValue(3).StringValue())

		count++
	}
	assert.Equal(t, 10, count)
}

func TestBasicExecutor_Filter(t *testing.T) {
	th := setupSalesTable(t, storage.NewBufferPool(20, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{}))
	insertSalesData(t, th, nil, 10) // No indexes needed for Scan tests

	scanNode := planner.NewSeqScanNode(th.oid, th.StorageSchema().GetFieldTypes(), transaction.LockModeS)
	scanExec := NewSeqScanExecutor(scanNode, th)

	// Filter: region = 'EU'
	// Region is column 3.
	regCol := planner.NewColumnValueExpression(3, th.StorageSchema().GetFieldTypes(), "region")
	constEU := planner.NewConstantValueExpression(common.NewStringValue("EU"))
	pred := planner.NewComparisonExpression(regCol, constEU, planner.Equal)

	filterExec := NewFilter(planner.NewFilterNode(scanNode, pred), scanExec)
	require.NoError(t, filterExec.Init(NewExecutorContext(nil)))

	count := 0
	for filterExec.Next() {
		tup := filterExec.Current()
		// Only odd IDs have region="EU"
		id := tup.GetValue(0).IntValue()
		assert.True(t, id%2 != 0, "Filter returned even ID %d which should be US", id)
		assert.Equal(t, "EU", tup.GetValue(3).StringValue())
		count++
	}
	assert.Equal(t, 5, count, "Should have 5 EU rows")
}

func TestBasicExecutor_Projection(t *testing.T) {
	th := setupSalesTable(t, storage.NewBufferPool(20, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{}))
	insertSalesData(t, th, nil, 10) // No indexes needed for Scan tests

	scanNode := planner.NewSeqScanNode(th.oid, th.StorageSchema().GetFieldTypes(), transaction.LockModeS)
	scanExec := NewSeqScanExecutor(scanNode, th)

	idCol := planner.NewColumnValueExpression(0, th.StorageSchema().GetFieldTypes(), "id")
	revCol := planner.NewColumnValueExpression(1, th.StorageSchema().GetFieldTypes(), "revenue")
	costCol := planner.NewColumnValueExpression(2, th.StorageSchema().GetFieldTypes(), "cost")

	// arithmetic: revenue - cost
	profitExpr := planner.NewArithmeticExpression(revCol, costCol, planner.Sub)

	projNode := planner.NewProjectionNode(scanNode, []planner.Expr{idCol, profitExpr})
	projExec := NewProjectionExecutor(projNode, scanExec)

	require.NoError(t, projExec.Init(NewExecutorContext(nil)))

	count := 0
	for projExec.Next() {
		tup := projExec.Current()

		id := tup.GetValue(0).IntValue()
		profit := tup.GetValue(1).IntValue()

		// Logic: rev = i*200, cost = i*100 -> profit = i*100
		expectedProfit := id * 100
		assert.Equal(t, expectedProfit, profit)
		count++
	}
	assert.Equal(t, 10, count)
}

func TestBasicExecutor_Limit(t *testing.T) {
	th := setupSalesTable(t, storage.NewBufferPool(20, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{}))
	insertSalesData(t, th, nil, 10) // No indexes needed for Scan tests

	scanNode := planner.NewSeqScanNode(th.oid, th.StorageSchema().GetFieldTypes(), transaction.LockModeS)
	scanExec := NewSeqScanExecutor(scanNode, th)

	limitExec := NewLimitExecutor(planner.NewLimitNode(scanNode, 7), scanExec)
	require.NoError(t, limitExec.Init(NewExecutorContext(nil)))

	count := 0
	for limitExec.Next() {
		count++
	}
	assert.Equal(t, 7, count)
}

func TestBasicExecutor_BasicPipeline(t *testing.T) {
	th := setupSalesTable(t, storage.NewBufferPool(20, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{}))
	insertSalesData(t, th, nil, 10) // No indexes needed for Scan tests

	// Scan
	scanNode := planner.NewSeqScanNode(th.oid, th.StorageSchema().GetFieldTypes(), transaction.LockModeS)
	scanExec := NewSeqScanExecutor(scanNode, th)

	// 1. Filter
	revCol := planner.NewColumnValueExpression(1, th.StorageSchema().GetFieldTypes(), "revenue")
	const1000 := planner.NewConstantValueExpression(common.NewIntValue(1000))
	pred := planner.NewComparisonExpression(revCol, const1000, planner.GreaterThan)
	filterNode := planner.NewFilterNode(scanNode, pred)
	filterExec := NewFilter(filterNode, scanExec)

	// 2. Project (category is col 4, id is col 0)
	catCol := planner.NewColumnValueExpression(4, th.StorageSchema().GetFieldTypes(), "category")
	idCol := planner.NewColumnValueExpression(0, th.StorageSchema().GetFieldTypes(), "id")
	projNode := planner.NewProjectionNode(filterNode, []planner.Expr{catCol, idCol})
	projExec := NewProjectionExecutor(projNode, filterExec)

	// 3. Limit
	limitNode := planner.NewLimitNode(projNode, 3)
	limitExec := NewLimitExecutor(limitNode, projExec)

	require.NoError(t, limitExec.Init(NewExecutorContext(nil)))

	// Expected Rows: i=6, i=7, i=8
	// i=6: 6*200=1200 (>1000). Category=Electronics (6%3==0)
	// i=7: 7*200=1400. Category=Books (7%3==1)
	// i=8: 8*200=1600. Category=Clothing (8%3==2)

	expectedIds := []int64{6, 7, 8}
	expectedCats := []string{"Electronics", "Books", "Clothing"}

	idx := 0
	for limitExec.Next() {
		tup := limitExec.Current()

		catVal := tup.GetValue(0).StringValue()
		idVal := tup.GetValue(1).IntValue()

		assert.Equal(t, expectedIds[idx], idVal)
		assert.Equal(t, expectedCats[idx], catVal)
		idx++
	}
	assert.Equal(t, 3, idx)
}

func TestBasicExecutor_Restart(t *testing.T) {
	th := setupSalesTable(t, storage.NewBufferPool(20, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{}))
	insertSalesData(t, th, nil, 10) // No indexes needed for Scan tests

	scanNode := planner.NewSeqScanNode(th.oid, th.StorageSchema().GetFieldTypes(), transaction.LockModeS)
	scanExec := NewSeqScanExecutor(scanNode, th)
	limitExec := NewLimitExecutor(planner.NewLimitNode(scanNode, 2), scanExec)

	ctx := NewExecutorContext(nil)

	// Run 1
	require.NoError(t, limitExec.Init(ctx))
	assert.True(t, limitExec.Next())
	assert.True(t, limitExec.Next())
	assert.False(t, limitExec.Next())

	// Run 2 (Restart)
	require.NoError(t, limitExec.Init(ctx))
	assert.True(t, limitExec.Next())
	// Should be the first row again
	tup := limitExec.Current()
	assert.Equal(t, int64(0), tup.GetValue(0).IntValue())
}
