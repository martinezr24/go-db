package execution

import (
	"mit.edu/dsg/godb/planner"
	"mit.edu/dsg/godb/storage"
)

// TopNExecutor optimizes "ORDER BY ... LIMIT N" queries.
//
// This should allow an optimized implementation that avoids sorting ALL tuples (O(M log M)).
type TopNExecutor struct {
	// Fill me in!
}

func NewTopNExecutor(plan *planner.TopNNode, child Executor) *TopNExecutor {
	panic("unimplemented")
}

func (e *TopNExecutor) PlanNode() planner.PlanNode {
	panic("unimplemented")
}

func (e *TopNExecutor) Init(ctx *ExecutorContext) error {
	panic("unimplemented")
}

func (e *TopNExecutor) Next() bool {
	panic("unimplemented")
}

func (e *TopNExecutor) Current() storage.Tuple {
	panic("unimplemented")
}

func (e *TopNExecutor) Error() error {
	panic("unimplemented")
}

func (e *TopNExecutor) Close() error {
	panic("unimplemented")
}
