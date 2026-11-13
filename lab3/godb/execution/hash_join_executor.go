package execution

import (
	"mit.edu/dsg/godb/common"
	"mit.edu/dsg/godb/planner"
	"mit.edu/dsg/godb/storage"
)

// HashJoinExecutor implements the hash join algorithm.
// It builds a hash table from the left child and probes it with the right child.
// It only supports Equi-Joins.
type HashJoinExecutor struct {
	plan        *planner.HashJoinNode
	left, right Executor
	results     []storage.Tuple
	cursor      int
	current     storage.Tuple
	err         error
	built       bool
}

// NewHashJoinExecutor creates a new HashJoinExecutor.
func NewHashJoinExecutor(plan *planner.HashJoinNode, left Executor, right Executor) *HashJoinExecutor {
	return &HashJoinExecutor{plan: plan, left: left, right: right}
}

func (e *HashJoinExecutor) PlanNode() planner.PlanNode {
	return e.plan
}

func (e *HashJoinExecutor) Init(ctx *ExecutorContext) error {
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

func (e *HashJoinExecutor) Next() bool {
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

func (e *HashJoinExecutor) Current() storage.Tuple {
	return e.current
}

func (e *HashJoinExecutor) Error() error {
	return e.err
}

func (e *HashJoinExecutor) Close() error {
	if err := e.left.Close(); err != nil {
		return err
	}
	return e.right.Close()
}

func joinKey(t storage.Tuple, exprs []planner.Expr) ([]byte, bool) {
	vals := make([]common.Value, len(exprs))
	types := make([]common.Type, len(exprs))
	for i, expr := range exprs {
		vals[i] = expr.Eval(t).Copy()
		if vals[i].IsNull() {
			return nil, false
		}
		types[i] = expr.OutputType()
	}
	desc := storage.NewRawTupleDesc(types)
	buf := make([]byte, desc.BytesPerTuple())
	storage.FromValues(vals...).WriteToBuffer(buf, desc)
	return buf, true
}

func (e *HashJoinExecutor) build() {
	leftDesc := storage.NewRawTupleDesc(e.plan.Left.OutputSchema())
	table := make(map[string][]storage.Tuple)
	for e.left.Next() {
		t := e.left.Current()
		key, ok := joinKey(t, e.plan.LeftKeys)
		if ok {
			table[string(key)] = append(table[string(key)], t.DeepCopy(leftDesc))
		}
	}
	if e.left.Error() != nil {
		e.err = e.left.Error()
		return
	}
	outDesc := storage.NewRawTupleDesc(e.plan.OutputSchema())
	for e.right.Next() {
		r := e.right.Current()
		key, ok := joinKey(r, e.plan.RightKeys)
		if !ok {
			continue
		}
		for _, l := range table[string(key)] {
			buf := make([]byte, outDesc.BytesPerTuple())
			e.results = append(e.results, storage.MergeTuples(buf, outDesc, l, r))
		}
	}
	if e.right.Error() != nil {
		e.err = e.right.Error()
	}
}
