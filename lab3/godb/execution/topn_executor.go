package execution

import (
	"mit.edu/dsg/godb/planner"
	"mit.edu/dsg/godb/storage"
	"sort"
)

// TopNExecutor optimizes "ORDER BY ... LIMIT N" queries.
//
// This should allow an optimized implementation that avoids sorting ALL tuples (O(M log M)).
type TopNExecutor struct {
	plan    *planner.TopNNode
	child   Executor
	tuples  []storage.Tuple
	cursor  int
	current storage.Tuple
	err     error
	built   bool
}

func NewTopNExecutor(plan *planner.TopNNode, child Executor) *TopNExecutor {
	return &TopNExecutor{plan: plan, child: child}
}

func (e *TopNExecutor) PlanNode() planner.PlanNode {
	return e.plan
}

func (e *TopNExecutor) Init(ctx *ExecutorContext) error {
	e.tuples = nil
	e.cursor = 0
	e.current = storage.Tuple{}
	e.err = nil
	e.built = false
	return e.child.Init(ctx)
}

func (e *TopNExecutor) Next() bool {
	if e.err != nil {
		return false
	}
	if !e.built {
		desc := storage.NewRawTupleDesc(e.plan.OutputSchema())
		for e.child.Next() {
			e.tuples = append(e.tuples, e.child.Current().DeepCopy(desc))
		}
		if e.child.Error() != nil {
			e.err = e.child.Error()
			return false
		}
		sort.SliceStable(e.tuples, func(i, j int) bool { return tupleLess(e.tuples[i], e.tuples[j], e.plan.OrderBy) })
		if e.plan.Limit < len(e.tuples) {
			e.tuples = e.tuples[:max(0, e.plan.Limit)]
		}
		e.built = true
	}
	if e.cursor >= len(e.tuples) {
		return false
	}
	e.current = e.tuples[e.cursor]
	e.cursor++
	return true
}

func (e *TopNExecutor) Current() storage.Tuple {
	return e.current
}

func (e *TopNExecutor) Error() error {
	return e.err
}

func (e *TopNExecutor) Close() error {
	return e.child.Close()
}
