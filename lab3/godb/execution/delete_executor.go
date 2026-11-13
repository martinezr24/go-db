package execution

import (
	"mit.edu/dsg/godb/common"
	"mit.edu/dsg/godb/indexing"
	"mit.edu/dsg/godb/planner"
	"mit.edu/dsg/godb/storage"
	"mit.edu/dsg/godb/transaction"
)

// DeleteExecutor executes a DELETE query.
// It iterates over the child (which produces the tuples to be deleted with all rows read),
// removes them from the TableHeap, and cleans up all associated Index entries.
type DeleteExecutor struct {
	plan     *planner.DeleteNode
	child    Executor
	table    *TableHeap
	indexes  []indexing.Index
	txn      *transaction.TransactionContext
	count    int64
	current  storage.Tuple
	err      error
	finished bool
}

func NewDeleteExecutor(plan *planner.DeleteNode, child Executor, tableHeap *TableHeap, indexes []indexing.Index) *DeleteExecutor {
	return &DeleteExecutor{
		plan:    plan,
		child:   child,
		table:   tableHeap,
		indexes: indexes,
	}
}

func (e *DeleteExecutor) PlanNode() planner.PlanNode {
	return e.plan
}

func (e *DeleteExecutor) Init(ctx *ExecutorContext) error {
	e.count = 0
	e.current = storage.Tuple{}
	e.err = nil
	e.finished = false
	e.txn = ctx.GetTransaction()
	return e.child.Init(ctx)
}

func (e *DeleteExecutor) Next() bool {
	if e.finished || e.err != nil {
		return false
	}
	e.finished = true

	for e.child.Next() {
		row := e.child.Current()
		rid := row.RID()

		for _, index := range e.indexes {
			metadata := index.Metadata()
			keyBuffer := make([]byte, metadata.KeySchema.BytesPerTuple())

			for keyColumn, tableColumn := range metadata.ProjectionList {
				metadata.KeySchema.SetValue(
					keyBuffer,
					keyColumn,
					row.GetValue(tableColumn),
				)
			}

			key := metadata.AsKey(storage.RawTuple(keyBuffer))
			if err := index.DeleteEntry(key, rid, e.txn); err != nil {
				e.err = err
				return false
			}
		}

		if err := e.table.DeleteTuple(e.txn, rid); err != nil {
			e.err = err
			return false
		}

		e.count++
	}

	if e.err = e.child.Error(); e.err != nil {
		return false
	}

	e.current = storage.FromValues(common.NewIntValue(e.count))
	return true
}

func (e *DeleteExecutor) Current() storage.Tuple {
	return e.current
}

func (e *DeleteExecutor) Close() error {
	return e.child.Close()
}

func (e *DeleteExecutor) Error() error {
	return e.err
}
