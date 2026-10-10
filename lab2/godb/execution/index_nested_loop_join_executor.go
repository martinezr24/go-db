package execution

import (
	"mit.edu/dsg/godb/indexing"
	"mit.edu/dsg/godb/planner"
	"mit.edu/dsg/godb/storage"
)

// IndexNestedLoopJoinExecutor implements an index nested loop join.
// It iterates over the left child, and for each tuple, probes the index of the right table.
// The expressions given for the left tuple should have the same schema as the right index's key
type IndexNestedLoopJoinExecutor struct {
	// Fill me in!
}

// NewIndexJoinExecutor creates a new IndexNestedLoopJoinExecutor.
// It assumes the left table is accessed via the provided rightIndex and rightTableHeap.
func NewIndexJoinExecutor(plan *planner.IndexNestedLoopJoinNode, left Executor, rightIndex indexing.Index, rightTableHeap *TableHeap) *IndexNestedLoopJoinExecutor {
	panic("unimplemented")
}

func (e *IndexNestedLoopJoinExecutor) PlanNode() planner.PlanNode {
	panic("unimplemented")
}

func (e *IndexNestedLoopJoinExecutor) Init(ctx *ExecutorContext) error {
	panic("unimplemented")
}

func (e *IndexNestedLoopJoinExecutor) Next() bool {
	panic("unimplemented")
}

func (e *IndexNestedLoopJoinExecutor) Current() storage.Tuple {
	panic("unimplemented")
}

func (e *IndexNestedLoopJoinExecutor) Error() error {
	panic("unimplemented")
}

func (e *IndexNestedLoopJoinExecutor) Close() error {
	panic("unimplemented")
}
