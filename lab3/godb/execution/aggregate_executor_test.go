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

func TestAggregate_Global_Sum(t *testing.T) {
	bp := storage.NewBufferPool(20, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{})
	sales := setupSalesTable(t, bp)
	// Insert 5 rows. Revenue = i * 200.
	// 0: 0
	// 1: 200
	// 2: 400
	// 3: 600
	// 4: 800
	// Total: 2000
	insertSalesData(t, sales, nil, 5)

	scanNode := planner.NewSeqScanNode(sales.oid, sales.StorageSchema().GetFieldTypes(), transaction.LockModeS)
	scanExec := NewSeqScanExecutor(scanNode, sales)

	// SELECT SUM(revenue) FROM sales
	aggClauses := []planner.AggregateClause{
		{
			Type: planner.AggSum,
			Expr: planner.NewColumnValueExpression(1, sales.StorageSchema().GetFieldTypes(), "revenue"),
		},
	}

	// No Group By
	aggNode := planner.NewAggregateNode(scanNode, nil, aggClauses)
	aggExec := NewAggregateExecutor(aggNode, scanExec)

	require.NoError(t, aggExec.Init(NewExecutorContext(nil)))

	expected := []storage.Tuple{
		storage.FromValues(common.NewIntValue(2000)),
	}

	checkAggregateResult(t, aggExec, expected)
}

func TestAggregate_GroupBy_Single(t *testing.T) {
	bp := storage.NewBufferPool(20, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{})
	sales := setupSalesTable(t, bp)

	// Custom data
	// Region "US": Rev 100, 200
	// Region "EU": Rev 50, 50, 50
	rows := [][]planner.Expr{
		{planner.NewConstantValueExpression(common.NewIntValue(1)), planner.NewConstantValueExpression(common.NewIntValue(100)), planner.NewConstantValueExpression(common.NewIntValue(0)), planner.NewConstantValueExpression(common.NewStringValue("US")), planner.NewConstantValueExpression(common.NewStringValue("C"))},
		{planner.NewConstantValueExpression(common.NewIntValue(2)), planner.NewConstantValueExpression(common.NewIntValue(200)), planner.NewConstantValueExpression(common.NewIntValue(0)), planner.NewConstantValueExpression(common.NewStringValue("US")), planner.NewConstantValueExpression(common.NewStringValue("C"))},
		{planner.NewConstantValueExpression(common.NewIntValue(3)), planner.NewConstantValueExpression(common.NewIntValue(50)), planner.NewConstantValueExpression(common.NewIntValue(0)), planner.NewConstantValueExpression(common.NewStringValue("EU")), planner.NewConstantValueExpression(common.NewStringValue("C"))},
		{planner.NewConstantValueExpression(common.NewIntValue(4)), planner.NewConstantValueExpression(common.NewIntValue(50)), planner.NewConstantValueExpression(common.NewIntValue(0)), planner.NewConstantValueExpression(common.NewStringValue("EU")), planner.NewConstantValueExpression(common.NewStringValue("C"))},
		{planner.NewConstantValueExpression(common.NewIntValue(5)), planner.NewConstantValueExpression(common.NewIntValue(50)), planner.NewConstantValueExpression(common.NewIntValue(0)), planner.NewConstantValueExpression(common.NewStringValue("EU")), planner.NewConstantValueExpression(common.NewStringValue("C"))},
	}
	insertRows(t, sales, rows, nil)

	scanNode := planner.NewSeqScanNode(sales.oid, sales.StorageSchema().GetFieldTypes(), transaction.LockModeS)
	scanExec := NewSeqScanExecutor(scanNode, sales)

	// SELECT region, SUM(revenue), COUNT(sale_id) FROM sales GROUP BY region
	groupBy := []planner.Expr{
		planner.NewColumnValueExpression(3, sales.StorageSchema().GetFieldTypes(), "region"),
	}
	aggClauses := []planner.AggregateClause{
		{Type: planner.AggSum, Expr: planner.NewColumnValueExpression(1, sales.StorageSchema().GetFieldTypes(), "revenue")},
		{Type: planner.AggCount, Expr: planner.NewColumnValueExpression(0, sales.StorageSchema().GetFieldTypes(), "sale_id")},
	}

	aggNode := planner.NewAggregateNode(scanNode, groupBy, aggClauses)
	aggExec := NewAggregateExecutor(aggNode, scanExec)

	require.NoError(t, aggExec.Init(NewExecutorContext(nil)))

	// Expected:
	// "US": Sum=300, Count=2
	// "EU": Sum=150, Count=3
	expected := []storage.Tuple{
		storage.FromValues(common.NewStringValue("US"), common.NewIntValue(300), common.NewIntValue(2)),
		storage.FromValues(common.NewStringValue("EU"), common.NewIntValue(150), common.NewIntValue(3)),
	}

	checkAggregateResult(t, aggExec, expected)
}

func TestAggregate_AllTypes(t *testing.T) {
	bp := storage.NewBufferPool(20, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{})
	sales := setupSalesTable(t, bp)

	// Data: (Rev)
	// 10, 20, 30
	rows := [][]planner.Expr{
		{planner.NewConstantValueExpression(common.NewIntValue(1)), planner.NewConstantValueExpression(common.NewIntValue(10)), planner.NewConstantValueExpression(common.NewIntValue(0)), planner.NewConstantValueExpression(common.NewStringValue("A")), planner.NewConstantValueExpression(common.NewStringValue("C"))},
		{planner.NewConstantValueExpression(common.NewIntValue(2)), planner.NewConstantValueExpression(common.NewIntValue(20)), planner.NewConstantValueExpression(common.NewIntValue(0)), planner.NewConstantValueExpression(common.NewStringValue("A")), planner.NewConstantValueExpression(common.NewStringValue("C"))},
		{planner.NewConstantValueExpression(common.NewIntValue(3)), planner.NewConstantValueExpression(common.NewIntValue(30)), planner.NewConstantValueExpression(common.NewIntValue(0)), planner.NewConstantValueExpression(common.NewStringValue("A")), planner.NewConstantValueExpression(common.NewStringValue("C"))},
	}
	insertRows(t, sales, rows, nil)

	scanNode := planner.NewSeqScanNode(sales.oid, sales.StorageSchema().GetFieldTypes(), transaction.LockModeS)
	scanExec := NewSeqScanExecutor(scanNode, sales)

	// SELECT COUNT(id), SUM(rev), MIN(rev), MAX(rev) FROM sales
	aggClauses := []planner.AggregateClause{
		{Type: planner.AggCount, Expr: planner.NewColumnValueExpression(0, sales.StorageSchema().GetFieldTypes(), "id")},
		{Type: planner.AggSum, Expr: planner.NewColumnValueExpression(1, sales.StorageSchema().GetFieldTypes(), "revenue")},
		{Type: planner.AggMin, Expr: planner.NewColumnValueExpression(1, sales.StorageSchema().GetFieldTypes(), "revenue")},
		{Type: planner.AggMax, Expr: planner.NewColumnValueExpression(1, sales.StorageSchema().GetFieldTypes(), "revenue")},
	}

	aggNode := planner.NewAggregateNode(scanNode, nil, aggClauses)
	aggExec := NewAggregateExecutor(aggNode, scanExec)
	require.NoError(t, aggExec.Init(NewExecutorContext(nil)))

	expected := []storage.Tuple{
		storage.FromValues(common.NewIntValue(3), common.NewIntValue(60), common.NewIntValue(10), common.NewIntValue(30)),
	}

	checkAggregateResult(t, aggExec, expected)
}

func TestAggregate_EmptyInput(t *testing.T) {
	bp := storage.NewBufferPool(20, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{})
	sales := setupSalesTable(t, bp)
	// No data

	scanNode := planner.NewSeqScanNode(sales.oid, sales.StorageSchema().GetFieldTypes(), transaction.LockModeS)
	scanExec := NewSeqScanExecutor(scanNode, sales)

	aggClauses := []planner.AggregateClause{
		{Type: planner.AggSum, Expr: planner.NewColumnValueExpression(1, sales.StorageSchema().GetFieldTypes(), "revenue")},
	}

	aggNode := planner.NewAggregateNode(scanNode, nil, aggClauses)
	aggExec := NewAggregateExecutor(aggNode, scanExec)

	require.NoError(t, aggExec.Init(NewExecutorContext(nil)))

	// Since implementation loops over child.Next(), if no rows, nothing in hash map.
	// Returns 0 rows. (Standard SQL might return 1 row with NULL for global agg, but this implementation returns 0).
	count := 0
	for aggExec.Next() {
		count++
	}
	assert.Equal(t, 0, count)
	require.NoError(t, aggExec.Close())
}

func TestAggregate_StringMinMax(t *testing.T) {
	bp := storage.NewBufferPool(20, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{})
	sales := setupSalesTable(t, bp)

	// Schema: (sale_id, revenue, cost, region, category)
	// We will aggregate on 'category' (String)
	// Insert:
	// 1. Category "Banana"
	// 2. Category "Apple"
	// 3. Category "Zebra"
	// 4. Category "Mango"
	rows := [][]planner.Expr{
		{planner.NewConstantValueExpression(common.NewIntValue(1)), planner.NewConstantValueExpression(common.NewIntValue(100)), planner.NewConstantValueExpression(common.NewIntValue(0)), planner.NewConstantValueExpression(common.NewStringValue("US")), planner.NewConstantValueExpression(common.NewStringValue("Banana"))},
		{planner.NewConstantValueExpression(common.NewIntValue(2)), planner.NewConstantValueExpression(common.NewIntValue(100)), planner.NewConstantValueExpression(common.NewIntValue(0)), planner.NewConstantValueExpression(common.NewStringValue("US")), planner.NewConstantValueExpression(common.NewStringValue("Apple"))},
		{planner.NewConstantValueExpression(common.NewIntValue(3)), planner.NewConstantValueExpression(common.NewIntValue(100)), planner.NewConstantValueExpression(common.NewIntValue(0)), planner.NewConstantValueExpression(common.NewStringValue("US")), planner.NewConstantValueExpression(common.NewStringValue("Zebra"))},
		{planner.NewConstantValueExpression(common.NewIntValue(4)), planner.NewConstantValueExpression(common.NewIntValue(100)), planner.NewConstantValueExpression(common.NewIntValue(0)), planner.NewConstantValueExpression(common.NewStringValue("US")), planner.NewConstantValueExpression(common.NewStringValue("Mango"))},
	}
	insertRows(t, sales, rows, nil)

	scanNode := planner.NewSeqScanNode(sales.oid, sales.StorageSchema().GetFieldTypes(), transaction.LockModeS)
	scanExec := NewSeqScanExecutor(scanNode, sales)

	// SELECT MIN(category), MAX(category) FROM sales
	aggClauses := []planner.AggregateClause{
		{Type: planner.AggMin, Expr: planner.NewColumnValueExpression(4, sales.StorageSchema().GetFieldTypes(), "category")},
		{Type: planner.AggMax, Expr: planner.NewColumnValueExpression(4, sales.StorageSchema().GetFieldTypes(), "category")},
	}

	aggNode := planner.NewAggregateNode(scanNode, nil, aggClauses)
	aggExec := NewAggregateExecutor(aggNode, scanExec)

	require.NoError(t, aggExec.Init(NewExecutorContext(nil)))

	// Expected: Min="Apple", Max="Zebra"
	expected := []storage.Tuple{
		storage.FromValues(common.NewStringValue("Apple"), common.NewStringValue("Zebra")),
	}

	checkAggregateResult(t, aggExec, expected)
	require.NoError(t, aggExec.Close())
}

func TestAggregate_IgnoreNulls(t *testing.T) {
	bp := storage.NewBufferPool(20, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{})
	sales := setupSalesTable(t, bp)

	// Insert:
	// 1. Rev=100
	// 2. Rev=NULL
	// 3. Rev=200
	rows := [][]planner.Expr{
		{planner.NewConstantValueExpression(common.NewIntValue(1)), planner.NewConstantValueExpression(common.NewIntValue(100)), planner.NewConstantValueExpression(common.NewIntValue(0)), planner.NewConstantValueExpression(common.NewStringValue("A")), planner.NewConstantValueExpression(common.NewStringValue("C"))},
		{planner.NewConstantValueExpression(common.NewIntValue(2)), planner.NewConstantValueExpression(common.NewNullInt()), planner.NewConstantValueExpression(common.NewIntValue(0)), planner.NewConstantValueExpression(common.NewStringValue("A")), planner.NewConstantValueExpression(common.NewStringValue("C"))},
		{planner.NewConstantValueExpression(common.NewIntValue(3)), planner.NewConstantValueExpression(common.NewIntValue(200)), planner.NewConstantValueExpression(common.NewIntValue(0)), planner.NewConstantValueExpression(common.NewStringValue("A")), planner.NewConstantValueExpression(common.NewStringValue("C"))},
	}
	insertRows(t, sales, rows, nil)

	scanNode := planner.NewSeqScanNode(sales.oid, sales.StorageSchema().GetFieldTypes(), transaction.LockModeS)
	scanExec := NewSeqScanExecutor(scanNode, sales)

	// SELECT SUM(revenue), COUNT(revenue) FROM sales
	aggClauses := []planner.AggregateClause{
		{Type: planner.AggSum, Expr: planner.NewColumnValueExpression(1, sales.StorageSchema().GetFieldTypes(), "revenue")},
		{Type: planner.AggCount, Expr: planner.NewColumnValueExpression(1, sales.StorageSchema().GetFieldTypes(), "revenue")},
	}

	aggNode := planner.NewAggregateNode(scanNode, nil, aggClauses)
	aggExec := NewAggregateExecutor(aggNode, scanExec)
	require.NoError(t, aggExec.Init(NewExecutorContext(nil)))

	// Sum should be 300 (100+200), ignoring NULL
	// Count should be 2, ignoring NULL
	expected := []storage.Tuple{
		storage.FromValues(common.NewIntValue(300), common.NewIntValue(2)),
	}

	checkAggregateResult(t, aggExec, expected)
}

func TestAggregate_GroupBy_Multi(t *testing.T) {
	bp := storage.NewBufferPool(20, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{})
	sales := setupSalesTable(t, bp)

	// Schema: (sale_id, revenue, cost, region, category)
	// Group By: Region, Category
	// Data:
	// 1. US, Clothing, 100
	// 2. US, Clothing, 200   -> Group (US, Clothing): Sum=300
	// 3. US, Electronics, 50 -> Group (US, Electronics): Sum=50
	// 4. EU, Clothing, 20    -> Group (EU, Clothing): Sum=20
	rows := [][]planner.Expr{
		{planner.NewConstantValueExpression(common.NewIntValue(1)), planner.NewConstantValueExpression(common.NewIntValue(100)), planner.NewConstantValueExpression(common.NewIntValue(0)), planner.NewConstantValueExpression(common.NewStringValue("US")), planner.NewConstantValueExpression(common.NewStringValue("Clothing"))},
		{planner.NewConstantValueExpression(common.NewIntValue(2)), planner.NewConstantValueExpression(common.NewIntValue(200)), planner.NewConstantValueExpression(common.NewIntValue(0)), planner.NewConstantValueExpression(common.NewStringValue("US")), planner.NewConstantValueExpression(common.NewStringValue("Clothing"))},
		{planner.NewConstantValueExpression(common.NewIntValue(3)), planner.NewConstantValueExpression(common.NewIntValue(50)), planner.NewConstantValueExpression(common.NewIntValue(0)), planner.NewConstantValueExpression(common.NewStringValue("US")), planner.NewConstantValueExpression(common.NewStringValue("Electronics"))},
		{planner.NewConstantValueExpression(common.NewIntValue(4)), planner.NewConstantValueExpression(common.NewIntValue(20)), planner.NewConstantValueExpression(common.NewIntValue(0)), planner.NewConstantValueExpression(common.NewStringValue("EU")), planner.NewConstantValueExpression(common.NewStringValue("Clothing"))},
	}
	insertRows(t, sales, rows, nil)

	scanNode := planner.NewSeqScanNode(sales.oid, sales.StorageSchema().GetFieldTypes(), transaction.LockModeS)
	scanExec := NewSeqScanExecutor(scanNode, sales)

	// SELECT region, category, SUM(revenue) ... GROUP BY region, category
	groupBy := []planner.Expr{
		planner.NewColumnValueExpression(3, sales.StorageSchema().GetFieldTypes(), "region"),
		planner.NewColumnValueExpression(4, sales.StorageSchema().GetFieldTypes(), "category"),
	}
	aggClauses := []planner.AggregateClause{
		{Type: planner.AggSum, Expr: planner.NewColumnValueExpression(1, sales.StorageSchema().GetFieldTypes(), "revenue")},
	}

	aggNode := planner.NewAggregateNode(scanNode, groupBy, aggClauses)
	aggExec := NewAggregateExecutor(aggNode, scanExec)

	require.NoError(t, aggExec.Init(NewExecutorContext(nil)))

	expected := []storage.Tuple{
		storage.FromValues(common.NewStringValue("US"), common.NewStringValue("Clothing"), common.NewIntValue(300)),
		storage.FromValues(common.NewStringValue("US"), common.NewStringValue("Electronics"), common.NewIntValue(50)),
		storage.FromValues(common.NewStringValue("EU"), common.NewStringValue("Clothing"), common.NewIntValue(20)),
	}

	checkAggregateResult(t, aggExec, expected)
	require.NoError(t, aggExec.Close())
}

func TestAggregate_GroupBy_NullKey(t *testing.T) {
	bp := storage.NewBufferPool(20, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{})
	sales := setupSalesTable(t, bp)

	// Schema: (sale_id, revenue, ..., region)
	// We Group By Region.
	// Data:
	// 1. Region "US", Rev 100
	// 2. Region NULL, Rev 200
	// 3. Region NULL, Rev 300
	// Expected groups: "US"->100, NULL->500 (NULLs should group together)
	rows := [][]planner.Expr{
		{planner.NewConstantValueExpression(common.NewIntValue(1)), planner.NewConstantValueExpression(common.NewIntValue(100)), planner.NewConstantValueExpression(common.NewIntValue(0)), planner.NewConstantValueExpression(common.NewStringValue("US")), planner.NewConstantValueExpression(common.NewStringValue("C"))},
		{planner.NewConstantValueExpression(common.NewIntValue(2)), planner.NewConstantValueExpression(common.NewIntValue(200)), planner.NewConstantValueExpression(common.NewIntValue(0)), planner.NewConstantValueExpression(common.NewNullString()), planner.NewConstantValueExpression(common.NewStringValue("C"))},
		{planner.NewConstantValueExpression(common.NewIntValue(3)), planner.NewConstantValueExpression(common.NewIntValue(300)), planner.NewConstantValueExpression(common.NewIntValue(0)), planner.NewConstantValueExpression(common.NewNullString()), planner.NewConstantValueExpression(common.NewStringValue("C"))},
	}
	insertRows(t, sales, rows, nil)

	scanNode := planner.NewSeqScanNode(sales.oid, sales.StorageSchema().GetFieldTypes(), transaction.LockModeS)
	scanExec := NewSeqScanExecutor(scanNode, sales)

	groupBy := []planner.Expr{
		planner.NewColumnValueExpression(3, sales.StorageSchema().GetFieldTypes(), "region"),
	}
	aggClauses := []planner.AggregateClause{
		{Type: planner.AggSum, Expr: planner.NewColumnValueExpression(1, sales.StorageSchema().GetFieldTypes(), "revenue")},
	}

	aggNode := planner.NewAggregateNode(scanNode, groupBy, aggClauses)
	aggExec := NewAggregateExecutor(aggNode, scanExec)

	require.NoError(t, aggExec.Init(NewExecutorContext(nil)))

	expected := []storage.Tuple{
		storage.FromValues(common.NewStringValue("US"), common.NewIntValue(100)),
		storage.FromValues(common.NewNullString(), common.NewIntValue(500)),
	}

	checkAggregateResult(t, aggExec, expected)
	require.NoError(t, aggExec.Close())
}

func TestAggregate_CountStar(t *testing.T) {
	// COUNT(*) is typically implemented by the planner as COUNT(1) (counting a non-null constant).
	// This ensures we count rows even if specific columns are NULL.
	bp := storage.NewBufferPool(20, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{})
	sales := setupSalesTable(t, bp)

	// Data:
	// 1. Rev 100
	// 2. Rev NULL
	// 3. Rev 200
	// Total Rows: 3. Count(Rev): 2. Count(*): 3.
	rows := [][]planner.Expr{
		{planner.NewConstantValueExpression(common.NewIntValue(1)), planner.NewConstantValueExpression(common.NewIntValue(100)), planner.NewConstantValueExpression(common.NewIntValue(0)), planner.NewConstantValueExpression(common.NewStringValue("A")), planner.NewConstantValueExpression(common.NewStringValue("C"))},
		{planner.NewConstantValueExpression(common.NewIntValue(2)), planner.NewConstantValueExpression(common.NewNullInt()), planner.NewConstantValueExpression(common.NewIntValue(0)), planner.NewConstantValueExpression(common.NewStringValue("A")), planner.NewConstantValueExpression(common.NewStringValue("C"))},
		{planner.NewConstantValueExpression(common.NewIntValue(3)), planner.NewConstantValueExpression(common.NewIntValue(200)), planner.NewConstantValueExpression(common.NewIntValue(0)), planner.NewConstantValueExpression(common.NewStringValue("A")), planner.NewConstantValueExpression(common.NewStringValue("C"))},
	}
	insertRows(t, sales, rows, nil)

	scanNode := planner.NewSeqScanNode(sales.oid, sales.StorageSchema().GetFieldTypes(), transaction.LockModeS)
	scanExec := NewSeqScanExecutor(scanNode, sales)

	// SELECT COUNT(revenue), COUNT(1) FROM sales
	aggClauses := []planner.AggregateClause{
		// Count specific column (ignores NULLs)
		{Type: planner.AggCount, Expr: planner.NewColumnValueExpression(1, sales.StorageSchema().GetFieldTypes(), "revenue")},
		// Count constant (simulates COUNT(*), includes all rows)
		{Type: planner.AggCount, Expr: planner.NewConstantValueExpression(common.NewIntValue(1))},
	}

	aggNode := planner.NewAggregateNode(scanNode, nil, aggClauses)
	aggExec := NewAggregateExecutor(aggNode, scanExec)

	require.NoError(t, aggExec.Init(NewExecutorContext(nil)))

	expected := []storage.Tuple{
		storage.FromValues(common.NewIntValue(2), common.NewIntValue(3)),
	}

	checkAggregateResult(t, aggExec, expected)
	require.NoError(t, aggExec.Close())
}

func TestAggregate_AllNulls(t *testing.T) {
	bp := storage.NewBufferPool(20, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{})
	sales := setupSalesTable(t, bp)

	// Insert 3 rows where 'revenue' is NULL
	rows := [][]planner.Expr{
		{planner.NewConstantValueExpression(common.NewIntValue(1)), planner.NewConstantValueExpression(common.NewNullInt()), planner.NewConstantValueExpression(common.NewIntValue(0)), planner.NewConstantValueExpression(common.NewStringValue("A")), planner.NewConstantValueExpression(common.NewStringValue("C"))},
		{planner.NewConstantValueExpression(common.NewIntValue(2)), planner.NewConstantValueExpression(common.NewNullInt()), planner.NewConstantValueExpression(common.NewIntValue(0)), planner.NewConstantValueExpression(common.NewStringValue("A")), planner.NewConstantValueExpression(common.NewStringValue("C"))},
		{planner.NewConstantValueExpression(common.NewIntValue(3)), planner.NewConstantValueExpression(common.NewNullInt()), planner.NewConstantValueExpression(common.NewIntValue(0)), planner.NewConstantValueExpression(common.NewStringValue("A")), planner.NewConstantValueExpression(common.NewStringValue("C"))},
	}
	insertRows(t, sales, rows, nil)

	scanNode := planner.NewSeqScanNode(sales.oid, sales.StorageSchema().GetFieldTypes(), transaction.LockModeS)
	scanExec := NewSeqScanExecutor(scanNode, sales)

	// SELECT SUM(revenue), MIN(revenue), MAX(revenue) FROM sales
	// Since all revenues are NULL, the result for all should be NULL (not 0)
	aggClauses := []planner.AggregateClause{
		{Type: planner.AggSum, Expr: planner.NewColumnValueExpression(1, sales.StorageSchema().GetFieldTypes(), "revenue")},
		{Type: planner.AggMin, Expr: planner.NewColumnValueExpression(1, sales.StorageSchema().GetFieldTypes(), "revenue")},
		{Type: planner.AggMax, Expr: planner.NewColumnValueExpression(1, sales.StorageSchema().GetFieldTypes(), "revenue")},
	}

	aggNode := planner.NewAggregateNode(scanNode, nil, aggClauses)
	aggExec := NewAggregateExecutor(aggNode, scanExec)

	require.NoError(t, aggExec.Init(NewExecutorContext(nil)))

	expected := []storage.Tuple{
		storage.FromValues(common.NewNullInt(), common.NewNullInt(), common.NewNullInt()),
	}

	checkAggregateResult(t, aggExec, expected)
	require.NoError(t, aggExec.Close())
}
