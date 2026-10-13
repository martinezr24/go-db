package execution

import (
	"mit.edu/dsg/godb/planner"
	"mit.edu/dsg/godb/storage"
)

// MaterializeExecutor acts as a pipeline barrier.
// It consumes all tuples from its child during the first execution and stores them.
// Subsequent calls to Init/Next iterate over the stored tuples.
type MaterializeExecutor struct {
	plan  *planner.MaterializeNode
	child Executor

	tuples       []storage.Tuple
	cursor       int
	materialized bool
	current      storage.Tuple
	err          error
}

func NewMaterializeExecutor(plan *planner.MaterializeNode, child Executor) *MaterializeExecutor {
	return &MaterializeExecutor{
		plan:  plan,
		child: child,
	}
}

func (e *MaterializeExecutor) PlanNode() planner.PlanNode {
	return e.plan
}

func (e *MaterializeExecutor) Init(ctx *ExecutorContext) error {
	e.cursor = 0
	e.current = storage.Tuple{}

	if e.materialized {
		return e.err
	}

	e.err = nil
	if err := e.child.Init(ctx); err != nil {
		e.err = err
		return err
	}

	desc := storage.NewRawTupleDesc(e.plan.Child.OutputSchema())
	for e.child.Next() {
		e.tuples = append(e.tuples, e.child.Current().DeepCopy(desc))
	}

	if err := e.child.Error(); err != nil {
		e.err = err
		return err
	}

	e.materialized = true
	return nil
}

func (e *MaterializeExecutor) Next() bool {
	if e.err != nil || e.cursor >= len(e.tuples) {
		return false
	}

	e.current = e.tuples[e.cursor]
	e.cursor++
	return true
}

func (e *MaterializeExecutor) Current() storage.Tuple {
	return e.current
}

func (e *MaterializeExecutor) Error() error {
	return e.err
}

func (e *MaterializeExecutor) Close() error {
	return e.child.Close()
}
