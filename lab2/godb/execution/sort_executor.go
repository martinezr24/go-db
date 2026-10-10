package execution

import (
	"mit.edu/dsg/godb/planner"
	"mit.edu/dsg/godb/storage"
)

// SortExecutor sorts the input tuples based on the provided ordering expressions.
// It is a blocking operator but uses lazy evaluation (sorts on first Next).
type SortExecutor struct {
	// Fill me in!
}

func NewSortExecutor(plan *planner.SortNode, child Executor) *SortExecutor {
	panic("unimplemented")
}

func (e *SortExecutor) PlanNode() planner.PlanNode {
	panic("unimplemented")
}

func (e *SortExecutor) Init(ctx *ExecutorContext) error {
	panic("unimplemented")
}

func (e *SortExecutor) Next() bool {
	panic("unimplemented")
}

func (e *SortExecutor) Current() storage.Tuple {
	panic("unimplemented")
}

func (e *SortExecutor) Error() error {
	panic("unimplemented")
}

func (e *SortExecutor) Close() error {
	panic("unimplemented")
}
