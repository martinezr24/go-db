package execution

import (
	"mit.edu/dsg/godb/planner"
	"mit.edu/dsg/godb/storage"
)

// LimitExecutor limits the number of tuples returned by the child executor.
type LimitExecutor struct {
	// Fill me in!
}

func NewLimitExecutor(plan *planner.LimitNode, child Executor) *LimitExecutor {
	panic("unimplemented")
}

func (e *LimitExecutor) PlanNode() planner.PlanNode {
	panic("unimplemented")
}

func (e *LimitExecutor) Init(ctx *ExecutorContext) error {
	panic("unimplemented")
}

func (e *LimitExecutor) Next() bool {
	panic("unimplemented")
}

func (e *LimitExecutor) Current() storage.Tuple {
	panic("unimplemented")
}

func (e *LimitExecutor) Error() error {
	panic("unimplemented")
}

func (e *LimitExecutor) Close() error {
	panic("unimplemented")
}
