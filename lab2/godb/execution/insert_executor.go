package execution

import (
	"mit.edu/dsg/godb/common"
	"mit.edu/dsg/godb/indexing"
	"mit.edu/dsg/godb/planner"
	"mit.edu/dsg/godb/storage"
	"mit.edu/dsg/godb/transaction"
)

// InsertExecutor executes an INSERT query.
// It consumes tuples from its child (which could be a ValuesExecutor or a SELECT query),
// inserts them into the TableHeap, and updates all associated indexes.
//
// For this course, you may assume that the child does not read from the table you are inserting into
type InsertExecutor struct {
	plan     *planner.InsertNode
	child    Executor
	table    *TableHeap
	indexes  []indexing.Index
	txn      *transaction.TransactionContext
	count    int64
	current  storage.Tuple
	err      error
	finished bool
}

func NewInsertExecutor(plan *planner.InsertNode, child Executor, tableHeap *TableHeap, indexes []indexing.Index) *InsertExecutor {
	return &InsertExecutor{
		plan:    plan,
		child:   child,
		table:   tableHeap,
		indexes: indexes,
	}
}

func (e *InsertExecutor) PlanNode() planner.PlanNode {
	return e.plan
}

func (e *InsertExecutor) Init(ctx *ExecutorContext) error {
	e.count = 0
	e.current = storage.Tuple{}
	e.err = nil
	e.finished = false
	e.txn = ctx.GetTransaction()
	return e.child.Init(ctx)
}

func (e *InsertExecutor) Next() bool {
	if e.finished || e.err != nil {
		return false
	}
	e.finished = true

	desc := e.table.StorageSchema()
	rowBuffer := make([]byte, desc.BytesPerTuple())

	for e.child.Next() {
		input := e.child.Current()

		input.WriteToBuffer(rowBuffer, desc)
		rid, err := e.table.InsertTuple(e.txn, storage.RawTuple(rowBuffer))
		if err != nil {
			e.err = err
			return false
		}

		for _, index := range e.indexes {
			metadata := index.Metadata()
			keyBuffer := make([]byte, metadata.KeySchema.BytesPerTuple())

			for keyColumn, tableColumn := range metadata.ProjectionList {
				metadata.KeySchema.SetValue(
					keyBuffer,
					keyColumn,
					input.GetValue(tableColumn),
				)
			}

			if err := index.InsertEntry(
				metadata.AsKey(storage.RawTuple(keyBuffer)),
				rid,
				e.txn,
			); err != nil {
				e.err = err
				return false
			}
		}

		e.count++
	}

	if e.err = e.child.Error(); e.err != nil {
		return false
	}

	e.current = storage.FromValues(common.NewIntValue(e.count))
	return true
}

func (e *InsertExecutor) Current() storage.Tuple {
	return e.current
}

func (e *InsertExecutor) Close() error {
	return e.child.Close()
}

func (e *InsertExecutor) Error() error {
	return e.err
}
