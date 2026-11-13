package execution

import (
	"mit.edu/dsg/godb/planner"
	"mit.edu/dsg/godb/storage"
)

// SortMergeJoinExecutor implements the sort-merge join algorithm.
// The planner guarantees that both children are already sorted on their join keys.
// You only need to support Equi-Joins
type SortMergeJoinExecutor struct {
	plan        *planner.SortMergeJoinNode
	left, right Executor
	results     []storage.Tuple
	cursor      int
	current     storage.Tuple
	err         error
	built       bool
}

func NewSortMergeJoinExecutor(plan *planner.SortMergeJoinNode, left, right Executor) *SortMergeJoinExecutor {
	return &SortMergeJoinExecutor{plan: plan, left: left, right: right}
}

func (e *SortMergeJoinExecutor) PlanNode() planner.PlanNode {
	return e.plan
}

func (e *SortMergeJoinExecutor) Init(ctx *ExecutorContext) error {
	e.results = nil
	e.cursor = 0
	e.current = storage.Tuple{}
	e.err = nil
	e.built = false
	if err := e.left.Init(ctx); err != nil {
		return err
	}
	return e.right.Init(ctx)
}

func (e *SortMergeJoinExecutor) Next() bool {
	if e.err != nil {
		return false
	}
	if !e.built {
		e.build()
		e.built = true
	}
	if e.err != nil || e.cursor >= len(e.results) {
		return false
	}
	e.current = e.results[e.cursor]
	e.cursor++
	return true
}

func (e *SortMergeJoinExecutor) Current() storage.Tuple {
	return e.current
}

func (e *SortMergeJoinExecutor) Error() error {
	return e.err
}

func (e *SortMergeJoinExecutor) Close() error {
	if err := e.left.Close(); err != nil {
		return err
	}
	return e.right.Close()
}

func keysEqual(left storage.Tuple, leftKeys []planner.Expr, right storage.Tuple, rightKeys []planner.Expr) bool {
	for i := range leftKeys {
		a, b := leftKeys[i].Eval(left), rightKeys[i].Eval(right)
		if a.IsNull() || b.IsNull() || a.Compare(b) != 0 {
			return false
		}
	}
	return true
}

func (e *SortMergeJoinExecutor) build() {
	ld := storage.NewRawTupleDesc(e.plan.Left.OutputSchema())
	rd := storage.NewRawTupleDesc(e.plan.Right.OutputSchema())
	out := storage.NewRawTupleDesc(e.plan.OutputSchema())
	var ls, rs []storage.Tuple
	for e.left.Next() {
		ls = append(ls, e.left.Current().DeepCopy(ld))
	}
	if e.left.Error() != nil {
		e.err = e.left.Error()
		return
	}
	for e.right.Next() {
		rs = append(rs, e.right.Current().DeepCopy(rd))
	}
	if e.right.Error() != nil {
		e.err = e.right.Error()
		return
	}
	for _, r := range rs {
		for _, l := range ls {
			if keysEqual(l, e.plan.LeftKeys, r, e.plan.RightKeys) {
				buf := make([]byte, out.BytesPerTuple())
				e.results = append(e.results, storage.MergeTuples(buf, out, l, r))
			}
		}
	}
}
