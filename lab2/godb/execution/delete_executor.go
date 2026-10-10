package execution

import (
	"mit.edu/dsg/godb/indexing"
	"mit.edu/dsg/godb/planner"
	"mit.edu/dsg/godb/storage"
)

// DeleteExecutor executes a DELETE query.
// It iterates over the child (which produces the tuples to be deleted with all rows read),
// removes them from the TableHeap, and cleans up all associated Index entries.
type DeleteExecutor struct {
	// Fill me in!
}

func NewDeleteExecutor(plan *planner.DeleteNode, child Executor, tableHeap *TableHeap, indexes []indexing.Index) *DeleteExecutor {
	panic("unimplemented")
}

func (e *DeleteExecutor) PlanNode() planner.PlanNode {
	panic("unimplemented")
}

func (e *DeleteExecutor) Init(ctx *ExecutorContext) error {
	panic("unimplemented")
}

func (e *DeleteExecutor) Next() bool {
	panic("unimplemented")
}

func (e *DeleteExecutor) Current() storage.Tuple {
	panic("unimplemented")
}

func (e *DeleteExecutor) Close() error {
	panic("unimplemented")
}

func (e *DeleteExecutor) Error() error {
	panic("unimplemented")
}
