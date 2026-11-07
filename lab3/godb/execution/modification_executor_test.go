package execution

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"mit.edu/dsg/godb/common"
	"mit.edu/dsg/godb/indexing"
	"mit.edu/dsg/godb/planner"
	"mit.edu/dsg/godb/storage"
	"mit.edu/dsg/godb/transaction"
)

func TestModificationExecutor_Insert(t *testing.T) {
	th := setupSalesTable(t, storage.NewBufferPool(20, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{}))
	idxId := indexing.NewMemHashIndex(storage.NewRawTupleDesc([]common.Type{common.IntType}), []int{0})
	idxCat := indexing.NewMemHashIndex(storage.NewRawTupleDesc([]common.Type{common.StringType}), []int{4})
	indexes := []indexing.Index{idxId, idxCat}

	// (10, 2000, 1000, "US", "Electronics")
	row1 := []planner.Expr{
		planner.NewConstantValueExpression(common.NewIntValue(10)),
		planner.NewConstantValueExpression(common.NewIntValue(2000)),
		planner.NewConstantValueExpression(common.NewIntValue(1000)),
		planner.NewConstantValueExpression(common.NewStringValue("US")),
		planner.NewConstantValueExpression(common.NewStringValue("Electronics")),
	}
	// (11, 500, 200, "EU", "Books")
	row2 := []planner.Expr{
		planner.NewConstantValueExpression(common.NewIntValue(11)),
		planner.NewConstantValueExpression(common.NewIntValue(500)),
		planner.NewConstantValueExpression(common.NewIntValue(200)),
		planner.NewConstantValueExpression(common.NewStringValue("EU")),
		planner.NewConstantValueExpression(common.NewStringValue("Books")),
	}
	// (12, 800, 400, "US", "Clothing")
	row3 := []planner.Expr{
		planner.NewConstantValueExpression(common.NewIntValue(12)),
		planner.NewConstantValueExpression(common.NewIntValue(800)),
		planner.NewConstantValueExpression(common.NewIntValue(400)),
		planner.NewConstantValueExpression(common.NewStringValue("US")),
		planner.NewConstantValueExpression(common.NewStringValue("Clothing")),
	}
	// (13, 3000, 1500, "EU", "Electronics")
	row4 := []planner.Expr{
		planner.NewConstantValueExpression(common.NewIntValue(13)),
		planner.NewConstantValueExpression(common.NewIntValue(3000)),
		planner.NewConstantValueExpression(common.NewIntValue(1500)),
		planner.NewConstantValueExpression(common.NewStringValue("EU")),
		planner.NewConstantValueExpression(common.NewStringValue("Electronics")),
	}

	valuesNode := planner.NewValuesNode([][]planner.Expr{row1, row2, row3, row4}, th.StorageSchema().GetFieldTypes())
	insertExec := NewInsertExecutor(planner.NewInsertNode(th.oid, valuesNode), NewValuesExecutor(valuesNode), th, indexes)
	require.NoError(t, insertExec.Init(NewExecutorContext(nil)))

	// Should return true once with the total count
	assert.True(t, insertExec.Next())
	current := insertExec.Current()
	assert.Equal(t, int64(4), current.GetValue(0).IntValue())
	assert.False(t, insertExec.Next())

	iter, _ := th.Iterator(nil, transaction.LockModeS, make([]byte, th.StorageSchema().BytesPerTuple()))
	rids := make(map[int64]common.RecordID)
	rowsFound := 0
	for iter.Next() {
		tup := storage.FromRawTuple(iter.CurrentTuple(), th.StorageSchema(), iter.CurrentRID())
		id := tup.GetValue(0).IntValue()
		rids[id] = tup.RID()
		rowsFound++
	}
	assert.Equal(t, 4, rowsFound)

	verifyIndexMatch(t, idxId, []common.Value{common.NewIntValue(10)}, rids[10], true)
	verifyIndexMatch(t, idxId, []common.Value{common.NewIntValue(11)}, rids[11], true)
	verifyIndexMatch(t, idxId, []common.Value{common.NewIntValue(12)}, rids[12], true)
	verifyIndexMatch(t, idxId, []common.Value{common.NewIntValue(13)}, rids[13], true)

	verifyIndexMatch(t, idxCat, []common.Value{common.NewStringValue("Electronics")}, rids[10], true)
	verifyIndexMatch(t, idxCat, []common.Value{common.NewStringValue("Electronics")}, rids[13], true)
	verifyIndexMatch(t, idxCat, []common.Value{common.NewStringValue("Books")}, rids[11], true)
	verifyIndexMatch(t, idxCat, []common.Value{common.NewStringValue("Clothing")}, rids[12], true)
}

func TestModificationExecutor_Update(t *testing.T) {
	th := setupSalesTable(t, storage.NewBufferPool(20, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{}))
	idxId := indexing.NewMemHashIndex(storage.NewRawTupleDesc([]common.Type{common.IntType}), []int{0})
	idxReg := indexing.NewMemHashIndex(storage.NewRawTupleDesc([]common.Type{common.StringType}), []int{3})
	indexes := []indexing.Index{idxId, idxReg}

	insertSalesData(t, th, indexes, 5)
	ridMap := make(map[int64]common.RecordID)
	iter, _ := th.Iterator(nil, transaction.LockModeS, make([]byte, th.StorageSchema().BytesPerTuple()))
	for iter.Next() {
		tup := storage.FromRawTuple(iter.CurrentTuple(), th.StorageSchema(), iter.CurrentRID())
		id := tup.GetValue(0).IntValue()
		ridMap[id] = tup.RID()
	}
	iter.Close()

	// UPDATE sales SET region = 'CA', revenue = 9999 WHERE id = 1
	scanNode := planner.NewSeqScanNode(th.oid, th.StorageSchema().GetFieldTypes(), transaction.LockModeX)
	filter := planner.NewFilterNode(scanNode, planner.NewComparisonExpression(
		planner.NewColumnValueExpression(0, th.StorageSchema().GetFieldTypes(), "id"),
		planner.NewConstantValueExpression(common.NewIntValue(1)),
		planner.Equal,
	))
	updateExprs := []planner.Expr{
		planner.NewColumnValueExpression(0, th.StorageSchema().GetFieldTypes(), "id"),
		planner.NewConstantValueExpression(common.NewIntValue(9999)),
		planner.NewColumnValueExpression(2, th.StorageSchema().GetFieldTypes(), "cost"),
		planner.NewConstantValueExpression(common.NewStringValue("CA")),
		planner.NewColumnValueExpression(4, th.StorageSchema().GetFieldTypes(), "category"),
	}
	updateNode := planner.NewUpdateNode(th.oid, filter, updateExprs)
	updateExec := NewUpdateExecutor(
		updateNode,
		NewFilter(filter, NewSeqScanExecutor(scanNode, th)),
		th,
		indexes,
	)
	require.NoError(t, updateExec.Init(NewExecutorContext(nil)))
	assert.True(t, updateExec.Next())
	assert.Equal(t, int64(1), updateExec.Current().GetValue(0).IntValue())

	buffer := make([]byte, th.StorageSchema().BytesPerTuple())
	err := th.ReadTuple(nil, ridMap[1], buffer, false)
	assert.NoError(t, err)
	updatedTuple := storage.FromRawTuple(buffer, th.StorageSchema(), ridMap[1])

	assert.Equal(t, int64(9999), updatedTuple.GetValue(1).IntValue())
	assert.Equal(t, "CA", updatedTuple.GetValue(3).StringValue())
	assert.Equal(t, int64(1), updatedTuple.GetValue(0).IntValue(), "ID should remain 1")
	assert.Equal(t, int64(100), updatedTuple.GetValue(2).IntValue(), "Cost should remain 100")
	assert.Equal(t, "Books", updatedTuple.GetValue(4).StringValue(), "Category should remain 'Books'")

	// Old key "EU" -> Should NOT point to this RID
	verifyIndexMatch(t, idxReg, []common.Value{common.NewStringValue("EU")}, ridMap[1], false)
	// New key "CA" -> SHOULD point to this RID
	verifyIndexMatch(t, idxReg, []common.Value{common.NewStringValue("CA")}, ridMap[1], true)
	// Unchanged Primary Key Index -> SHOULD still point to this RID
	verifyIndexMatch(t, idxId, []common.Value{common.NewIntValue(1)}, ridMap[1], true)

	// UPDATE sales SET id = 50 WHERE id = 2
	scanNodeB := planner.NewSeqScanNode(th.oid, th.StorageSchema().GetFieldTypes(), transaction.LockModeX)
	filterB := planner.NewFilterNode(scanNodeB, planner.NewComparisonExpression(
		planner.NewColumnValueExpression(0, th.StorageSchema().GetFieldTypes(), "id"),
		planner.NewConstantValueExpression(common.NewIntValue(2)),
		planner.Equal,
	))
	updateExprsB := []planner.Expr{
		planner.NewConstantValueExpression(common.NewIntValue(50)), // id (update)
		planner.NewColumnValueExpression(1, th.StorageSchema().GetFieldTypes(), "revenue"),
		planner.NewColumnValueExpression(2, th.StorageSchema().GetFieldTypes(), "cost"),
		planner.NewColumnValueExpression(3, th.StorageSchema().GetFieldTypes(), "region"),
		planner.NewColumnValueExpression(4, th.StorageSchema().GetFieldTypes(), "category"),
	}
	updateNodeB := planner.NewUpdateNode(th.oid, filterB, updateExprsB)
	updateExecB := NewUpdateExecutor(
		updateNodeB,
		NewFilter(filterB, NewSeqScanExecutor(scanNodeB, th)),
		th,
		indexes,
	)

	require.NoError(t, updateExecB.Init(NewExecutorContext(nil)))
	assert.True(t, updateExecB.Next())
	assert.Equal(t, int64(1), updateExecB.Current().GetValue(0).IntValue())

	err = th.ReadTuple(nil, ridMap[2], buffer, false)
	assert.NoError(t, err)
	updatedTuple = storage.FromRawTuple(buffer, th.StorageSchema(), ridMap[2])

	assert.Equal(t, int64(50), updatedTuple.GetValue(0).IntValue())
	assert.Equal(t, int64(400), updatedTuple.GetValue(1).IntValue(), "Revenue should remain 400")
	assert.Equal(t, int64(200), updatedTuple.GetValue(2).IntValue(), "Cost should remain 200")
	assert.Equal(t, "US", updatedTuple.GetValue(3).StringValue(), "Region should remain 'US'")
	assert.Equal(t, "Clothing", updatedTuple.GetValue(4).StringValue(), "Category should remain 'Clothing'")

	// Old ID (2) -> Should be gone
	verifyIndexMatch(t, idxId, []common.Value{common.NewIntValue(2)}, ridMap[2], false)
	// New ID (50) -> Should be present
	verifyIndexMatch(t, idxId, []common.Value{common.NewIntValue(50)}, ridMap[2], true)
	// Region "US" did NOT change, so the secondary index must still point to this RID
	verifyIndexMatch(t, idxReg, []common.Value{common.NewStringValue("US")}, ridMap[2], true)

	// UPDATE sales SET revenue = cost, cost = revenue WHERE id = 3

	scanNodeC := planner.NewSeqScanNode(th.oid, th.StorageSchema().GetFieldTypes(), transaction.LockModeX)
	filterC := planner.NewFilterNode(scanNodeC, planner.NewComparisonExpression(
		planner.NewColumnValueExpression(0, th.StorageSchema().GetFieldTypes(), "id"),
		planner.NewConstantValueExpression(common.NewIntValue(3)),
		planner.Equal,
	))
	swapExprs := []planner.Expr{
		planner.NewColumnValueExpression(0, th.StorageSchema().GetFieldTypes(), "id"),
		planner.NewColumnValueExpression(2, th.StorageSchema().GetFieldTypes(), "cost"),
		planner.NewColumnValueExpression(1, th.StorageSchema().GetFieldTypes(), "revenue"),
		planner.NewColumnValueExpression(3, th.StorageSchema().GetFieldTypes(), "region"),
		planner.NewColumnValueExpression(4, th.StorageSchema().GetFieldTypes(), "category"),
	}

	updateNodeC := planner.NewUpdateNode(th.oid, filterC, swapExprs)
	updateExecC := NewUpdateExecutor(
		updateNodeC,
		NewFilter(filterC, NewSeqScanExecutor(scanNodeC, th)),
		th,
		indexes,
	)
	require.NoError(t, updateExecC.Init(NewExecutorContext(nil)))
	assert.True(t, updateExecC.Next())
	assert.Equal(t, int64(1), updateExecC.Current().GetValue(0).IntValue())
	err = th.ReadTuple(nil, ridMap[3], buffer, false)
	assert.NoError(t, err)
	swapTuple := storage.FromRawTuple(buffer, th.StorageSchema(), ridMap[3])
	assert.Equal(t, int64(300), swapTuple.GetValue(1).IntValue(), "Swap failed: Revenue should get old Cost")
	assert.Equal(t, int64(600), swapTuple.GetValue(2).IntValue(), "Swap failed: Cost should get old Revenue")
	assert.Equal(t, int64(3), swapTuple.GetValue(0).IntValue(), "ID should remain 3")

	// Verify that ID 0 (which was not touched) is still intact
	err = th.ReadTuple(nil, ridMap[0], buffer, false)
	assert.NoError(t, err)
	originalTuple := storage.FromRawTuple(buffer, th.StorageSchema(), ridMap[0])
	assert.NotNil(t, originalTuple)
	assert.Equal(t, "US", originalTuple.GetValue(3).StringValue(), "Unrelated row ID=0 should not change region")
	assert.Equal(t, int64(0), originalTuple.GetValue(1).IntValue(), "Unrelated row ID=0 should not change revenue")
	// Primary Key Index for ID 0
	verifyIndexMatch(t, idxId, []common.Value{common.NewIntValue(0)}, ridMap[0], true)
	// Secondary Index for Region "US" -> RID 0
	verifyIndexMatch(t, idxReg, []common.Value{common.NewStringValue("US")}, ridMap[0], true)
}

func TestModificationExecutor_Delete(t *testing.T) {
	th := setupSalesTable(t, storage.NewBufferPool(20, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{}))
	idxId := indexing.NewMemHashIndex(storage.NewRawTupleDesc([]common.Type{common.IntType}), []int{0})
	idxReg := indexing.NewMemHashIndex(storage.NewRawTupleDesc([]common.Type{common.StringType}), []int{3})
	indexes := []indexing.Index{idxId, idxReg}
	insertSalesData(t, th, indexes, 5)

	// DELETE FROM sales WHERE id = 2
	scanNode := planner.NewSeqScanNode(th.oid, th.StorageSchema().GetFieldTypes(), transaction.LockModeX)
	filter := planner.NewFilterNode(scanNode, planner.NewComparisonExpression(
		planner.NewColumnValueExpression(0, th.StorageSchema().GetFieldTypes(), "id"),
		planner.NewConstantValueExpression(common.NewIntValue(2)),
		planner.Equal,
	))
	deleteNode := planner.NewDeleteNode(th.oid, filter)
	deleteExec := NewDeleteExecutor(
		deleteNode,
		NewFilter(filter, NewSeqScanExecutor(scanNode, th)),
		th,
		indexes,
	)
	require.NoError(t, deleteExec.Init(NewExecutorContext(nil)))
	assert.True(t, deleteExec.Next())
	assert.Equal(t, int64(1), deleteExec.Current().GetValue(0).IntValue())
	assert.False(t, deleteExec.Next())
	iter, _ := th.Iterator(nil, transaction.LockModeS, make([]byte, th.StorageSchema().BytesPerTuple()))
	rids := make(map[int64]common.RecordID)
	for iter.Next() {
		tup := storage.FromRawTuple(iter.CurrentTuple(), th.StorageSchema(), iter.CurrentRID())
		id := tup.GetValue(0).IntValue()
		rids[id] = tup.RID()
	}
	iter.Close()

	assert.NotContains(t, rids, int64(2))
	assert.Contains(t, rids, int64(0))
	assert.Contains(t, rids, int64(1))
	assert.Contains(t, rids, int64(3))
	assert.Contains(t, rids, int64(4))
	verifyIndexMatch(t, idxId, []common.Value{common.NewIntValue(0)}, rids[2], false)
	verifyIndexMatch(t, idxReg, []common.Value{common.NewStringValue("US")}, rids[2], false)

	// DELETE FROM sales WHERE region = 'EU'
	scanNode2 := planner.NewSeqScanNode(th.oid, th.StorageSchema().GetFieldTypes(), transaction.LockModeX)
	filter2 := planner.NewFilterNode(scanNode2, planner.NewComparisonExpression(
		planner.NewColumnValueExpression(3, th.StorageSchema().GetFieldTypes(), "region"),
		planner.NewConstantValueExpression(common.NewStringValue("EU")),
		planner.Equal,
	))

	deleteNode2 := planner.NewDeleteNode(th.oid, filter2)
	deleteExec2 := NewDeleteExecutor(
		deleteNode2,
		NewFilter(filter2, NewSeqScanExecutor(scanNode2, th)),
		th,
		indexes,
	)
	require.NoError(t, deleteExec2.Init(NewExecutorContext(nil)))
	assert.True(t, deleteExec2.Next())
	assert.Equal(t, int64(2), deleteExec2.Current().GetValue(0).IntValue())
	assert.False(t, deleteExec2.Next())
	ridsFinal := make(map[int64]common.RecordID)
	iterFinal, _ := th.Iterator(nil, transaction.LockModeS, make([]byte, th.StorageSchema().BytesPerTuple()))
	for iterFinal.Next() {
		tup := storage.FromRawTuple(iterFinal.CurrentTuple(), th.StorageSchema(), iterFinal.CurrentRID())
		id := tup.GetValue(0).IntValue()
		ridsFinal[id] = tup.RID()
	}
	iterFinal.Close()

	assert.Equal(t, 2, len(ridsFinal)) // Only 0 and 4 left
	assert.Contains(t, ridsFinal, int64(0))
	assert.Contains(t, ridsFinal, int64(4))
	assert.NotContains(t, ridsFinal, int64(1))
	assert.NotContains(t, ridsFinal, int64(3))

	verifyIndexMatch(t, idxId, []common.Value{common.NewIntValue(1)}, ridsFinal[1], false)
	verifyIndexMatch(t, idxReg, []common.Value{common.NewStringValue("EU")}, ridsFinal[1], false)
	verifyIndexMatch(t, idxId, []common.Value{common.NewIntValue(3)}, ridsFinal[3], false)
	verifyIndexMatch(t, idxReg, []common.Value{common.NewStringValue("EU")}, ridsFinal[3], false)

	verifyIndexMatch(t, idxId, []common.Value{common.NewIntValue(0)}, ridsFinal[0], true)
	verifyIndexMatch(t, idxId, []common.Value{common.NewIntValue(4)}, ridsFinal[4], true)
	verifyIndexMatch(t, idxReg, []common.Value{common.NewStringValue("US")}, ridsFinal[0], true)
	verifyIndexMatch(t, idxReg, []common.Value{common.NewStringValue("US")}, ridsFinal[4], true)
}

func TestModificationExecutor_MultiColumnIndex(t *testing.T) {
	th := setupSalesTable(t, storage.NewBufferPool(20, storage.NewDiskStorageManager(t.TempDir()), storage.NoopLogManager{}))

	// Create a Multi-Column Index on (region, category)
	idxMulti := indexing.NewMemHashIndex(
		storage.NewRawTupleDesc([]common.Type{common.StringType, common.StringType}),
		[]int{3, 4},
	)
	indexes := []indexing.Index{idxMulti}

	// INSERT: (1, 100, 50, "US", "Books")
	row1 := []planner.Expr{
		planner.NewConstantValueExpression(common.NewIntValue(1)),
		planner.NewConstantValueExpression(common.NewIntValue(100)),
		planner.NewConstantValueExpression(common.NewIntValue(50)),
		planner.NewConstantValueExpression(common.NewStringValue("US")),
		planner.NewConstantValueExpression(common.NewStringValue("Books")),
	}
	insertRows(t, th, [][]planner.Expr{row1}, indexes)

	iter, _ := th.Iterator(nil, transaction.LockModeS, make([]byte, th.StorageSchema().BytesPerTuple()))
	var rid common.RecordID
	if iter.Next() {
		rid = iter.CurrentRID()
	}
	iter.Close()
	require.False(t, rid.IsNil())
	// Verify Index: ("US", "Books") -> Match
	key1 := []common.Value{common.NewStringValue("US"), common.NewStringValue("Books")}
	verifyIndexMatch(t, idxMulti, key1, rid, true)

	// UPDATE: Change 'category' to 'Electronics', leave 'region' as 'US'
	scanNode := planner.NewSeqScanNode(th.oid, th.StorageSchema().GetFieldTypes(), transaction.LockModeX)
	filter := planner.NewFilterNode(scanNode, planner.NewComparisonExpression(
		planner.NewColumnValueExpression(0, th.StorageSchema().GetFieldTypes(), "id"),
		planner.NewConstantValueExpression(common.NewIntValue(1)),
		planner.Equal,
	))

	updateExprs := []planner.Expr{
		planner.NewColumnValueExpression(0, th.StorageSchema().GetFieldTypes(), "id"),
		planner.NewColumnValueExpression(1, th.StorageSchema().GetFieldTypes(), "revenue"),
		planner.NewColumnValueExpression(2, th.StorageSchema().GetFieldTypes(), "cost"),
		planner.NewColumnValueExpression(3, th.StorageSchema().GetFieldTypes(), "region"), // Keep 'region' source
		planner.NewConstantValueExpression(common.NewStringValue("Electronics")),          // Change 'category'
	}
	updateNode := planner.NewUpdateNode(th.oid, filter, updateExprs)
	updateExec := NewUpdateExecutor(
		updateNode,
		NewFilter(filter, NewSeqScanExecutor(scanNode, th)),
		th,
		indexes,
	)
	require.NoError(t, updateExec.Init(NewExecutorContext(nil)))
	assert.True(t, updateExec.Next())

	verifyIndexMatch(t, idxMulti, key1, rid, false)
	key2 := []common.Value{common.NewStringValue("US"), common.NewStringValue("Electronics")}
	verifyIndexMatch(t, idxMulti, key2, rid, true)

	// UPDATE: Change 'revenue' to 900. Touch NO columns in index.
	scanNode3 := planner.NewSeqScanNode(th.oid, th.StorageSchema().GetFieldTypes(), transaction.LockModeX)
	filter3 := planner.NewFilterNode(scanNode3, planner.NewComparisonExpression(
		planner.NewColumnValueExpression(0, th.StorageSchema().GetFieldTypes(), "id"),
		planner.NewConstantValueExpression(common.NewIntValue(1)),
		planner.Equal,
	))
	updateExprs3 := []planner.Expr{
		planner.NewColumnValueExpression(0, th.StorageSchema().GetFieldTypes(), "id"),
		planner.NewConstantValueExpression(common.NewIntValue(900)), // Change Revenue
		planner.NewColumnValueExpression(2, th.StorageSchema().GetFieldTypes(), "cost"),
		planner.NewColumnValueExpression(3, th.StorageSchema().GetFieldTypes(), "region"),
		planner.NewColumnValueExpression(4, th.StorageSchema().GetFieldTypes(), "category"),
	}
	updateNode3 := planner.NewUpdateNode(th.oid, filter3, updateExprs3)
	updateExec3 := NewUpdateExecutor(
		updateNode3,
		NewFilter(filter3, NewSeqScanExecutor(scanNode3, th)),
		th,
		indexes,
	)
	require.NoError(t, updateExec3.Init(NewExecutorContext(nil)))
	assert.True(t, updateExec3.Next())
	// Verify Key ("US", "Electronics") STILL matches the RID
	verifyIndexMatch(t, idxMulti, key2, rid, true)
}
