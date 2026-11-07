package execution

import (
	"mit.edu/dsg/godb/common"
	"mit.edu/dsg/godb/planner"
	"mit.edu/dsg/godb/storage"
)

// ValuesExecutor generates a stream of tuples from a list of constant expressions.
// It is typically used in INSERT statements (e.g., "INSERT INTO t VALUES (1, 'a'), (2, 'b')").
type ValuesExecutor struct {
	plan *planner.ValuesNode
	ctx  *ExecutorContext

	// Runtime state
	cursor        int
	currentValues []common.Value
}

func NewValuesExecutor(plan *planner.ValuesNode) *ValuesExecutor {
	return &ValuesExecutor{
		plan: plan,
	}
}

func (e *ValuesExecutor) PlanNode() planner.PlanNode {
	return e.plan
}

func (e *ValuesExecutor) Init(ctx *ExecutorContext) error {
	e.ctx = ctx
	e.cursor = 0
	e.currentValues = make([]common.Value, len(e.plan.Schema))
	return nil
}

func (e *ValuesExecutor) Next() bool {

	if e.cursor >= len(e.plan.Values) {
		return false
	}

	for i, expr := range e.plan.Values[e.cursor] {
		// Evaluate the expression (tuples are nil because these are constants)
		e.currentValues[i] = expr.Eval(storage.EmptyTuple)
	}

	e.cursor++
	return true
}

func (e *ValuesExecutor) Current() storage.Tuple {
	return storage.FromValues(e.currentValues...)
}

func (e *ValuesExecutor) Close() error {
	return nil
}

func (e *ValuesExecutor) Error() error {
	return nil
}
