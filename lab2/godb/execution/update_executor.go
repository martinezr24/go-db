package execution

import (
	"mit.edu/dsg/godb/common"
	"mit.edu/dsg/godb/indexing"
	"mit.edu/dsg/godb/planner"
	"mit.edu/dsg/godb/storage"
	"mit.edu/dsg/godb/transaction"
)

// UpdateExecutor implements the execution logic for updating tuples in a table.
// It iterates over the tuples provided by its child executor, which represent the full value of the current row
// and its RID. It uses the expressions defined in the plan to calculate the new values for every column in the new row.
// The executor updates the table heap in-place and ensures that all relevant indexes are updated
// if the key columns have changed. It produces a single tuple containing the count of updated rows.
type UpdateExecutor struct {
	plan     *planner.UpdateNode
	child    Executor
	table    *TableHeap
	indexes  []indexing.Index
	txn      *transaction.TransactionContext
	count    int64
	current  storage.Tuple
	err      error
	finished bool
}

func NewUpdateExecutor(plan *planner.UpdateNode, child Executor, tableHeap *TableHeap, indexes []indexing.Index) *UpdateExecutor {
	return &UpdateExecutor{
		plan:    plan,
		child:   child,
		table:   tableHeap,
		indexes: indexes,
	}
}

func (e *UpdateExecutor) PlanNode() planner.PlanNode {
	return e.plan
}

func (e *UpdateExecutor) Init(ctx *ExecutorContext) error {
	e.txn = ctx.GetTransaction()
	e.count = 0
	e.current = storage.Tuple{}
	e.err = nil
	e.finished = false
	return e.child.Init(ctx)
}

func (e *UpdateExecutor) Next() bool {
	if e.finished || e.err != nil {
		return false
	}
	e.finished = true

	desc := e.table.StorageSchema()
	rowBuffer := make([]byte, desc.BytesPerTuple())

	for e.child.Next() {
		oldRow := e.child.Current()
		rid := oldRow.RID()

		newValues := make([]common.Value, len(e.plan.Expressions))
		for i, expr := range e.plan.Expressions {
			newValues[i] = expr.Eval(oldRow)
		}
		newRow := storage.FromValues(newValues...)

		for _, index := range e.indexes {
			metadata := index.Metadata()
			oldKeyBuffer := make([]byte, metadata.KeySchema.BytesPerTuple())
			newKeyBuffer := make([]byte, metadata.KeySchema.BytesPerTuple())
			keyChanged := false

			for keyColumn, tableColumn := range metadata.ProjectionList {
				oldValue := oldRow.GetValue(tableColumn)
				newValue := newValues[tableColumn]
				metadata.KeySchema.SetValue(oldKeyBuffer, keyColumn, oldValue)
				metadata.KeySchema.SetValue(newKeyBuffer, keyColumn, newValue)

				if oldValue.Compare(newValue) != 0 {
					keyChanged = true
				}
			}

			if keyChanged {
				if err := index.DeleteEntry(
					metadata.AsKey(storage.RawTuple(oldKeyBuffer)),
					rid,
					e.txn,
				); err != nil {
					e.err = err
					return false
				}
				if err := index.InsertEntry(
					metadata.AsKey(storage.RawTuple(newKeyBuffer)),
					rid,
					e.txn,
				); err != nil {
					e.err = err
					return false
				}
			}
		}

		newRow.WriteToBuffer(rowBuffer, desc)
		if err := e.table.UpdateTuple(e.txn, rid, storage.RawTuple(rowBuffer)); err != nil {
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

func (e *UpdateExecutor) OutputSchema() []common.Type {
	return e.plan.OutputSchema()
}

func (e *UpdateExecutor) Current() storage.Tuple {
	return e.current
}

func (e *UpdateExecutor) Close() error {
	return e.child.Close()
}

func (e *UpdateExecutor) Error() error {
	return e.err
}
