package execution

import (
	"mit.edu/dsg/godb/planner"
	"mit.edu/dsg/godb/storage"
)

// SortMergeJoinExecutor implements the sort-merge join algorithm.
// The planner guarantees that both children are already sorted on their join keys.
// You only need to support Equi-Joins
type SortMergeJoinExecutor struct {
	// Fill me in!
}

func NewSortMergeJoinExecutor(plan *planner.SortMergeJoinNode, left, right Executor) *SortMergeJoinExecutor {
	panic("unimplemented")
}

func (e *SortMergeJoinExecutor) PlanNode() planner.PlanNode {
	panic("unimplemented")
}

func (e *SortMergeJoinExecutor) Init(ctx *ExecutorContext) error {
	panic("unimplemented")
}

func (e *SortMergeJoinExecutor) Next() bool {
	panic("unimplemented")
}

func (e *SortMergeJoinExecutor) Current() storage.Tuple {
	panic("unimplemented")
}

func (e *SortMergeJoinExecutor) Error() error {
	panic("unimplemented")
}

func (e *SortMergeJoinExecutor) Close() error {
	panic("unimplemented")
}
