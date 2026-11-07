package execution

import (
	"mit.edu/dsg/godb/planner"
	"mit.edu/dsg/godb/storage"
)

// ProjectionExecutor evaluates a list of expressions on the input tuples
// and produces a new tuple containing the results of those expressions.
type ProjectionExecutor struct {
	// Fill me in!
}

// NewProjectionExecutor creates a new ProjectionExecutor.
func NewProjectionExecutor(plan *planner.ProjectionNode, child Executor) *ProjectionExecutor {
	panic("unimplemented")
}

func (e *ProjectionExecutor) PlanNode() planner.PlanNode {
	panic("unimplemented")
}

func (e *ProjectionExecutor) Init(ctx *ExecutorContext) error {
	panic("unimplemented")
}

func (e *ProjectionExecutor) Next() bool {
	panic("unimplemented")
}

func (e *ProjectionExecutor) Current() storage.Tuple {
	panic("unimplemented")
}

func (e *ProjectionExecutor) Error() error {
	panic("unimplemented")
}

func (e *ProjectionExecutor) Close() error {
	panic("unimplemented")
}
