package execution

import (
	"mit.edu/dsg/godb/planner"
	"mit.edu/dsg/godb/storage"
)

// The size of block, in bytes, that the join operator is allowed to buffer
const blockSize = 1 << 15

// BlockNestedLoopJoinExecutor implements the block nested loop join algorithm.
// It loads a block of tuples from the left child into memory and then scans the right child
// to find matches. This reduces the number of times the right child is sequentially scanned.
type BlockNestedLoopJoinExecutor struct {
	// Fill me in!
}

// NewBlockNestedLoopJoinExecutor creates a new BlockNestedLoopJoinExecutor.
func NewBlockNestedLoopJoinExecutor(plan *planner.NestedLoopJoinNode, left Executor, right Executor) *BlockNestedLoopJoinExecutor {
	panic("unimplemented")
}

func (e *BlockNestedLoopJoinExecutor) PlanNode() planner.PlanNode {
	panic("unimplemented")
}

func (e *BlockNestedLoopJoinExecutor) Init(ctx *ExecutorContext) error {
	panic("unimplemented")
}

func (e *BlockNestedLoopJoinExecutor) Next() bool {
	panic("unimplemented")
}

func (e *BlockNestedLoopJoinExecutor) Current() storage.Tuple {
	panic("unimplemented")
}

func (e *BlockNestedLoopJoinExecutor) Error() error {
	panic("unimplemented")
}

func (e *BlockNestedLoopJoinExecutor) Close() error {
	panic("unimplemented")
}
