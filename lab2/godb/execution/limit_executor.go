package execution

import (
	"mit.edu/dsg/godb/planner"
	"mit.edu/dsg/godb/storage"
)

// LimitExecutor limits the number of tuples returned by the child executor.
type LimitExecutor struct {
	plan    *planner.LimitNode
	child   Executor
	count   int
	current storage.Tuple
	err     error
}

func NewLimitExecutor(plan *planner.LimitNode, child Executor) *LimitExecutor {
	return &LimitExecutor{
		plan:  plan,
		child: child,
	}
}

func (e *LimitExecutor) PlanNode() planner.PlanNode {
	return e.plan
}

func (e *LimitExecutor) Init(ctx *ExecutorContext) error {
	e.count = 0
	e.current = storage.Tuple{}
	e.err = nil
	return e.child.Init(ctx)
}

func (e *LimitExecutor) Next() bool {
	if e.err != nil || e.count >= e.plan.Limit {
		return false
	}

	if !e.child.Next() {
		e.err = e.child.Error()
		return false
	}

	e.current = e.child.Current()
	e.count++
	return true
}

func (e *LimitExecutor) Current() storage.Tuple {
	return e.current
}

func (e *LimitExecutor) Error() error {
	return e.err
}

func (e *LimitExecutor) Close() error {
	return e.child.Close()
}
