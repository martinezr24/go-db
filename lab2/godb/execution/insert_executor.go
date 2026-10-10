package execution

import (
	"mit.edu/dsg/godb/indexing"
	"mit.edu/dsg/godb/planner"
	"mit.edu/dsg/godb/storage"
)

// InsertExecutor executes an INSERT query.
// It consumes tuples from its child (which could be a ValuesExecutor or a SELECT query),
// inserts them into the TableHeap, and updates all associated indexes.
//
// For this course, you may assume that the child does not read from the table you are inserting into
type InsertExecutor struct {
	// Fill me in!
}

func NewInsertExecutor(plan *planner.InsertNode, child Executor, tableHeap *TableHeap, indexes []indexing.Index) *InsertExecutor {
	panic("unimplemented")
}

func (e *InsertExecutor) PlanNode() planner.PlanNode {
	panic("unimplemented")
}

func (e *InsertExecutor) Init(ctx *ExecutorContext) error {
	panic("unimplemented")
}

func (e *InsertExecutor) Next() bool {
	panic("unimplemented")
}

func (e *InsertExecutor) Current() storage.Tuple {
	panic("unimplemented")
}

func (e *InsertExecutor) Close() error {
	panic("unimplemented")
}

func (e *InsertExecutor) Error() error {
	panic("unimplemented")
}
