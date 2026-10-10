package execution

import (
	"mit.edu/dsg/godb/planner"
	"mit.edu/dsg/godb/storage"
)

// FilterExecutor filters tuples from its child executor based on a predicate.
type FilterExecutor struct {
	// Fill me in!
}

// NewFilter creates a new FilterExecutor executor.
func NewFilter(plan *planner.FilterNode, child Executor) *FilterExecutor {
	panic("unimplemented")
}

func (e *FilterExecutor) PlanNode() planner.PlanNode {
	panic("unimplemented")
}

// Init initializes the child.
func (e *FilterExecutor) Init(context *ExecutorContext) error {
	panic("unimplemented")
}

func (e *FilterExecutor) Next() bool {
	panic("unimplemented")
}

func (e *FilterExecutor) Current() storage.Tuple {
	panic("unimplemented")
}

func (e *FilterExecutor) Error() error {
	panic("unimplemented")
}

func (e *FilterExecutor) Close() error {
	panic("unimplemented")
}
