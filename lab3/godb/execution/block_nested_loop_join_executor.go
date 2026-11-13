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
	plan              *planner.NestedLoopJoinNode
	left, right       Executor
	ctx               *ExecutorContext
	block             []storage.Tuple
	leftDone          bool
	rightTuple        storage.Tuple
	haveRight         bool
	blockIndex        int
	current           storage.Tuple
	err               error
	outDesc, leftDesc *storage.RawTupleDesc
}

// NewBlockNestedLoopJoinExecutor creates a new BlockNestedLoopJoinExecutor.
func NewBlockNestedLoopJoinExecutor(plan *planner.NestedLoopJoinNode, left Executor, right Executor) *BlockNestedLoopJoinExecutor {
	return &BlockNestedLoopJoinExecutor{plan: plan, left: left, right: right}
}

func (e *BlockNestedLoopJoinExecutor) PlanNode() planner.PlanNode {
	return e.plan
}

func (e *BlockNestedLoopJoinExecutor) Init(ctx *ExecutorContext) error {
	e.ctx = ctx
	e.block = nil
	e.leftDone = false
	e.haveRight = false
	e.blockIndex = 0
	e.current = storage.Tuple{}
	e.err = nil
	e.leftDesc = storage.NewRawTupleDesc(e.plan.Left.OutputSchema())
	e.outDesc = storage.NewRawTupleDesc(e.plan.OutputSchema())
	if err := e.left.Init(ctx); err != nil {
		return err
	}
	return e.loadBlock()
}

func (e *BlockNestedLoopJoinExecutor) Next() bool {
	if e.err != nil {
		return false
	}
	for len(e.block) > 0 {
		if !e.haveRight {
			if e.right.Next() {
				e.rightTuple = e.right.Current()
				e.haveRight = true
				e.blockIndex = 0
			} else {
				if e.right.Error() != nil {
					e.err = e.right.Error()
					return false
				}
				if e.leftDone {
					return false
				}
				if err := e.loadBlock(); err != nil {
					e.err = err
					return false
				}
				if len(e.block) == 0 {
					return false
				}
				continue
			}
		}
		for e.blockIndex < len(e.block) {
			l := e.block[e.blockIndex]
			e.blockIndex++
			buf := make([]byte, e.outDesc.BytesPerTuple())
			joined := storage.MergeTuples(buf, e.outDesc, l, e.rightTuple)
			if planner.ExprIsTrue(e.plan.Predicate.Eval(joined)) {
				e.current = joined
				return true
			}
		}
		e.haveRight = false
	}
	return false
}

func (e *BlockNestedLoopJoinExecutor) Current() storage.Tuple {
	return e.current
}

func (e *BlockNestedLoopJoinExecutor) Error() error {
	return e.err
}

func (e *BlockNestedLoopJoinExecutor) Close() error {
	if err := e.left.Close(); err != nil {
		return err
	}
	return e.right.Close()
}

func (e *BlockNestedLoopJoinExecutor) loadBlock() error {
	e.block = e.block[:0]
	maxTuples := blockSize / e.leftDesc.BytesPerTuple()
	if maxTuples < 1 {
		maxTuples = 1
	}
	for len(e.block) < maxTuples && e.left.Next() {
		e.block = append(e.block, e.left.Current().DeepCopy(e.leftDesc))
	}
	if e.left.Error() != nil {
		return e.left.Error()
	}
	if len(e.block) < maxTuples {
		e.leftDone = true
	}
	e.haveRight = false
	if len(e.block) > 0 {
		return e.right.Init(e.ctx)
	}
	return nil
}
