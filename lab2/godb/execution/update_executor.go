package execution

import (
	"mit.edu/dsg/godb/common"
	"mit.edu/dsg/godb/indexing"
	"mit.edu/dsg/godb/planner"
	"mit.edu/dsg/godb/storage"
)

// UpdateExecutor implements the execution logic for updating tuples in a table.
// It iterates over the tuples provided by its child executor, which represent the full value of the current row
// and its RID. It uses the expressions defined in the plan to calculate the new values for every column in the new row.
// The executor updates the table heap in-place and ensures that all relevant indexes are updated
// if the key columns have changed. It produces a single tuple containing the count of updated rows.
type UpdateExecutor struct {
	// Fill me in!
}

func NewUpdateExecutor(plan *planner.UpdateNode, child Executor, tableHeap *TableHeap, indexes []indexing.Index) *UpdateExecutor {
	panic("unimplemented")
}

func (e *UpdateExecutor) PlanNode() planner.PlanNode {
	panic("unimplemented")

}

func (e *UpdateExecutor) Init(ctx *ExecutorContext) error {
	panic("unimplemented")
}

func (e *UpdateExecutor) Next() bool {
	panic("unimplemented")
}

func (e *UpdateExecutor) OutputSchema() []common.Type {
	panic("unimplemented")
}

func (e *UpdateExecutor) Current() storage.Tuple {
	panic("unimplemented")
}

func (e *UpdateExecutor) Close() error {
	panic("unimplemented")
}

func (e *UpdateExecutor) Error() error {
	panic("unimplemented")
}
