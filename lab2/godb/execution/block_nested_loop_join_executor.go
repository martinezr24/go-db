package execution

import (
	"errors"

	"mit.edu/dsg/godb/common"
	"mit.edu/dsg/godb/planner"
	"mit.edu/dsg/godb/storage"
)

// The size of block, in bytes, that the join operator is allowed to buffer
const blockSize = 1 << 15

// BlockNestedLoopJoinExecutor implements the block nested loop join algorithm.
// It loads a block of tuples from the left child into memory and then scans the right child
// to find matches. This reduces the number of times the right child is sequentially scanned.
type BlockNestedLoopJoinExecutor struct {
	plan  *planner.NestedLoopJoinNode
	left  Executor
	right Executor
	ctx   *ExecutorContext

	leftRowSize int
	leftBlock   []storage.Tuple
	blockBytes  int
	leftDone    bool
	pendingLeft storage.Tuple
	hasPending  bool

	rightTuple    storage.Tuple
	rightHasTuple bool
	leftIndex     int
	rightStarted  bool

	current storage.Tuple
	err     error
}

// NewBlockNestedLoopJoinExecutor creates a new BlockNestedLoopJoinExecutor.
func NewBlockNestedLoopJoinExecutor(plan *planner.NestedLoopJoinNode, left Executor, right Executor) *BlockNestedLoopJoinExecutor {
	leftSchema := storage.NewRawTupleDesc(plan.Left.OutputSchema())
	leftRowSize := leftSchema.BytesPerTuple()
	if leftRowSize == 0 {
		leftRowSize = 1
	}
	return &BlockNestedLoopJoinExecutor{
		plan:        plan,
		left:        left,
		right:       right,
		leftRowSize: leftRowSize,
	}
}

func (e *BlockNestedLoopJoinExecutor) PlanNode() planner.PlanNode {
	return e.plan
}

func (e *BlockNestedLoopJoinExecutor) Init(ctx *ExecutorContext) error {
	e.ctx = ctx
	e.leftBlock = nil
	e.blockBytes = 0
	e.leftDone = false
	e.pendingLeft = storage.Tuple{}
	e.hasPending = false
	e.rightTuple = storage.Tuple{}
	e.rightHasTuple = false
	e.leftIndex = 0
	e.rightStarted = false
	e.current = storage.Tuple{}
	e.err = nil

	if err := e.left.Init(ctx); err != nil {
		e.err = err
		return err
	}
	if err := e.right.Init(ctx); err != nil {
		e.err = err
		return err
	}
	e.rightStarted = true
	return nil
}

func (e *BlockNestedLoopJoinExecutor) Next() bool {
	if e.err != nil {
		return false
	}

	for {
		if e.rightHasTuple {
			for e.leftIndex < len(e.leftBlock) {
				leftTuple := e.leftBlock[e.leftIndex]
				e.leftIndex++

				values := make([]common.Value, 0, leftTuple.NumColumns()+e.rightTuple.NumColumns())
				for i := 0; i < leftTuple.NumColumns(); i++ {
					values = append(values, leftTuple.GetValue(i))
				}
				for i := 0; i < e.rightTuple.NumColumns(); i++ {
					values = append(values, e.rightTuple.GetValue(i))
				}
				joined := storage.FromValues(values...)

				if planner.ExprIsTrue(e.plan.Predicate.Eval(joined)) {
					e.current = joined
					return true
				}
			}
			e.rightHasTuple = false
		}

		if len(e.leftBlock) == 0 || (e.leftIndex == 0 && !e.rightStarted) {
			if !e.loadLeftBlock() {
				return false
			}
			if e.err != nil {
				return false
			}
			if !e.rightStarted {
				if err := e.right.Init(e.ctx); err != nil {
					e.err = err
					return false
				}
				e.rightStarted = true
			}
			e.leftIndex = 0
		}

		if e.right.Next() {
			e.rightTuple = copyTupleValues(e.right.Current())
			e.rightHasTuple = true
			e.leftIndex = 0
			continue
		}
		if err := e.right.Error(); err != nil {
			e.err = err
			return false
		}

		e.leftBlock = nil
		e.blockBytes = 0
		e.rightStarted = false
	}
}

func (e *BlockNestedLoopJoinExecutor) Current() storage.Tuple {
	return e.current
}

func (e *BlockNestedLoopJoinExecutor) Error() error {
	return e.err
}

func (e *BlockNestedLoopJoinExecutor) Close() error {
	return errors.Join(e.left.Close(), e.right.Close())
}

func (e *BlockNestedLoopJoinExecutor) loadLeftBlock() bool {
	e.leftBlock = nil
	e.blockBytes = 0

	for !e.leftDone && e.blockBytes < blockSize {
		var tuple storage.Tuple
		if e.hasPending {
			tuple = e.pendingLeft
			e.pendingLeft = storage.Tuple{}
			e.hasPending = false
		} else {
			if !e.left.Next() {
				e.leftDone = true
				if err := e.left.Error(); err != nil {
					e.err = err
				}
				break
			}
			tuple = copyTupleValues(e.left.Current())
		}

		if len(e.leftBlock) > 0 && e.blockBytes+e.leftRowSize > blockSize {
			e.pendingLeft = tuple
			e.hasPending = true
			break
		}

		e.leftBlock = append(e.leftBlock, tuple)
		e.blockBytes += e.leftRowSize
	}

	return len(e.leftBlock) > 0
}

func copyTupleValues(tuple storage.Tuple) storage.Tuple {
	values := make([]common.Value, tuple.NumColumns())
	for i := range values {
		values[i] = tuple.GetValue(i).Copy()
	}
	return storage.FromValues(values...)
}
