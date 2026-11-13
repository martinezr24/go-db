package execution

import (
	"mit.edu/dsg/godb/planner"
	"mit.edu/dsg/godb/storage"
	"sort"
)

// SortExecutor sorts the input tuples based on the provided ordering expressions.
// It is a blocking operator but uses lazy evaluation (sorts on first Next).
type SortExecutor struct {
	plan    *planner.SortNode
	child   Executor
	tuples  []storage.Tuple
	cursor  int
	current storage.Tuple
	err     error
	built   bool
}

func NewSortExecutor(plan *planner.SortNode, child Executor) *SortExecutor {
	return &SortExecutor{plan: plan, child: child}
}

func (e *SortExecutor) PlanNode() planner.PlanNode {
	return e.plan
}

func (e *SortExecutor) Init(ctx *ExecutorContext) error {
	e.tuples = nil
	e.cursor = 0
	e.current = storage.Tuple{}
	e.err = nil
	e.built = false
	return e.child.Init(ctx)
}

func (e *SortExecutor) Next() bool {
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
		e.built = true
	}
	if e.cursor >= len(e.tuples) {
		return false
	}
	e.current = e.tuples[e.cursor]
	e.cursor++
	return true
}

func (e *SortExecutor) Current() storage.Tuple {
	return e.current
}

func (e *SortExecutor) Error() error {
	return e.err
}

func (e *SortExecutor) Close() error {
	return e.child.Close()
}

func tupleLess(a, b storage.Tuple, clauses []planner.OrderByClause) bool {
	for _, clause := range clauses {
		av, bv := clause.Expr.Eval(a), clause.Expr.Eval(b)
		cmp := av.Compare(bv)
		if cmp == 0 {
			continue
		}
		if clause.Direction == planner.SortOrderDescending {
			return cmp > 0
		}
		return cmp < 0
	}
	return false
}
