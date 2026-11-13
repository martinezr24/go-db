package execution

import (
	"mit.edu/dsg/godb/common"
	"mit.edu/dsg/godb/planner"
	"mit.edu/dsg/godb/storage"
)

// ProjectionExecutor evaluates a list of expressions on the input tuples
// and produces a new tuple containing the results of those expressions.
type ProjectionExecutor struct {
	plan    *planner.ProjectionNode
	child   Executor
	current storage.Tuple
	values  []common.Value
	err     error
}

// NewProjectionExecutor creates a new ProjectionExecutor.
func NewProjectionExecutor(plan *planner.ProjectionNode, child Executor) *ProjectionExecutor {
	return &ProjectionExecutor{
		plan:  plan,
		child: child,
	}
}

func (e *ProjectionExecutor) PlanNode() planner.PlanNode {
	return e.plan
}

func (e *ProjectionExecutor) Init(ctx *ExecutorContext) error {
	e.err = nil
	e.current = storage.Tuple{}
	e.values = make([]common.Value, len(e.plan.Expressions))
	return e.child.Init(ctx)
}

func (e *ProjectionExecutor) Next() bool {
	if e.err != nil || !e.child.Next() {
		e.err = e.child.Error()
		return false
	}

	input := e.child.Current()
	for i, expr := range e.plan.Expressions {
		e.values[i] = expr.Eval(input)
	}

	e.current = storage.FromValues(e.values...)
	return true
}

func (e *ProjectionExecutor) Current() storage.Tuple {
	return e.current
}

func (e *ProjectionExecutor) Error() error {
	return e.err
}

func (e *ProjectionExecutor) Close() error {
	return e.child.Close()
}
