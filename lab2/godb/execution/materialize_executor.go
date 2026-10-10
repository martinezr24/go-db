package execution

import (
	"mit.edu/dsg/godb/planner"
	"mit.edu/dsg/godb/storage"
)

// MaterializeExecutor acts as a pipeline barrier.
// It consumes all tuples from its child during the first execution and stores them.
// Subsequent calls to Init/Next iterate over the stored tuples.
type MaterializeExecutor struct {
	// Fill me in!
}

func NewMaterializeExecutor(plan *planner.MaterializeNode, child Executor) *MaterializeExecutor {
	panic("unimplemented")
}

func (e *MaterializeExecutor) PlanNode() planner.PlanNode {
	panic("unimplemented")
}

func (e *MaterializeExecutor) Init(ctx *ExecutorContext) error {
	panic("unimplemented")
}

func (e *MaterializeExecutor) Next() bool {
	panic("unimplemented")
}

func (e *MaterializeExecutor) Current() storage.Tuple {
	panic("unimplemented")
}

func (e *MaterializeExecutor) Error() error {
	panic("unimplemented")
}

func (e *MaterializeExecutor) Close() error {
	panic("unimplemented")
}
