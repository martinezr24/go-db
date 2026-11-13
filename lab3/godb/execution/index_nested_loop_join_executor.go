package execution

import (
	"errors"
	"mit.edu/dsg/godb/common"
	"mit.edu/dsg/godb/indexing"
	"mit.edu/dsg/godb/planner"
	"mit.edu/dsg/godb/storage"
)

// IndexNestedLoopJoinExecutor implements an index nested loop join.
// It iterates over the left child, and for each tuple, probes the index of the right table.
// The expressions given for the left tuple should have the same schema as the right index's key
type IndexNestedLoopJoinExecutor struct {
	plan       *planner.IndexNestedLoopJoinNode
	left       Executor
	rightIndex indexing.Index
	rightTable *TableHeap
	ctx        *ExecutorContext
	leftTuple  storage.Tuple
	rids       []common.RecordID
	ridCursor  int
	buffer     []byte
	current    storage.Tuple
	err        error
	outDesc    *storage.RawTupleDesc
}

// NewIndexJoinExecutor creates a new IndexNestedLoopJoinExecutor.
// It assumes the left table is accessed via the provided rightIndex and rightTableHeap.
func NewIndexJoinExecutor(plan *planner.IndexNestedLoopJoinNode, left Executor, rightIndex indexing.Index, rightTableHeap *TableHeap) *IndexNestedLoopJoinExecutor {
	return &IndexNestedLoopJoinExecutor{plan: plan, left: left, rightIndex: rightIndex, rightTable: rightTableHeap}
}

func (e *IndexNestedLoopJoinExecutor) PlanNode() planner.PlanNode {
	return e.plan
}

func (e *IndexNestedLoopJoinExecutor) Init(ctx *ExecutorContext) error {
	e.ctx = ctx
	e.rids = nil
	e.ridCursor = 0
	e.buffer = make([]byte, e.rightTable.StorageSchema().BytesPerTuple())
	e.current = storage.Tuple{}
	e.err = nil
	e.outDesc = storage.NewRawTupleDesc(e.plan.OutputSchema())
	return e.left.Init(ctx)
}

func (e *IndexNestedLoopJoinExecutor) Next() bool {
	if e.err != nil {
		return false
	}
	for {
		for e.ridCursor < len(e.rids) {
			rid := e.rids[e.ridCursor]
			e.ridCursor++
			if err := e.rightTable.ReadTuple(e.ctx.GetTransaction(), rid, e.buffer, e.plan.ForUpdate); err != nil {
				if errors.Is(err, ErrTupleDeleted) {
					continue
				}
				e.err = err
				return false
			}
			actual := keyFromHeapTuple(e.rightIndex, e.rightTable.StorageSchema(), e.buffer)
			probe, ok := e.probeKey(e.leftTuple)
			if !ok || !actual.Equals(probe) {
				continue
			}
			right := storage.FromRawTuple(e.buffer, e.rightTable.StorageSchema(), rid)
			buf := make([]byte, e.outDesc.BytesPerTuple())
			e.current = storage.MergeTuples(buf, e.outDesc, e.leftTuple, right)
			return true
		}
		if !e.left.Next() {
			e.err = e.left.Error()
			return false
		}
		e.leftTuple = e.left.Current()
		key, ok := e.probeKey(e.leftTuple)
		if !ok {
			continue
		}
		rids, err := e.rightIndex.ScanKey(key, e.rids[:0], e.ctx.GetTransaction())
		if err != nil {
			e.err = err
			return false
		}
		e.rids = rids
		e.ridCursor = 0
	}
}

func (e *IndexNestedLoopJoinExecutor) Current() storage.Tuple {
	return e.current
}

func (e *IndexNestedLoopJoinExecutor) Error() error {
	return e.err
}

func (e *IndexNestedLoopJoinExecutor) Close() error {
	return e.left.Close()
}

func (e *IndexNestedLoopJoinExecutor) probeKey(tuple storage.Tuple) (indexing.Key, bool) {
	md := e.rightIndex.Metadata()
	raw := make(storage.RawTuple, md.KeySchema.BytesPerTuple())
	for i, expr := range e.plan.LeftKeys {
		v := expr.Eval(tuple)
		if v.IsNull() {
			return indexing.NilKey, false
		}
		md.KeySchema.SetValue(raw, i, v)
	}
	return indexing.NewKey(raw, md.KeySchema), true
}
