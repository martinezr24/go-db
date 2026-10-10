package execution

import (
	"mit.edu/dsg/godb/planner"
	"mit.edu/dsg/godb/storage"
)

// HashJoinExecutor implements the hash join algorithm.
// It builds a hash table from the left child and probes it with the right child.
// It only supports Equi-Joins.
type HashJoinExecutor struct {
	// Fill me in!
}

// NewHashJoinExecutor creates a new HashJoinExecutor.
func NewHashJoinExecutor(plan *planner.HashJoinNode, left Executor, right Executor) *HashJoinExecutor {
	panic("unimplemented")
}

func (e *HashJoinExecutor) PlanNode() planner.PlanNode {
	panic("unimplemented")
}

func (e *HashJoinExecutor) Init(ctx *ExecutorContext) error {
	panic("unimplemented")
}

func (e *HashJoinExecutor) Next() bool {
	panic("unimplemented")
}

func (e *HashJoinExecutor) Current() storage.Tuple {
	panic("unimplemented")
}

func (e *HashJoinExecutor) Error() error {
	panic("unimplemented")
}

func (e *HashJoinExecutor) Close() error {
	panic("unimplemented")
}
