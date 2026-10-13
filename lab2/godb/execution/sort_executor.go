package execution

import (
	"sort"

	"mit.edu/dsg/godb/planner"
	"mit.edu/dsg/godb/storage"
)

// SortExecutor sorts the input tuples based on the provided ordering expressions.
// It is a blocking operator but uses lazy evaluation (sorts on first Next).
type SortExecutor struct {
	plan  *planner.SortNode
	child Executor

	tuples  []storage.Tuple
	cursor  int
	sorted  bool
	current storage.Tuple
	err     error
}

func NewSortExecutor(plan *planner.SortNode, child Executor) *SortExecutor {
	return &SortExecutor{
		plan:  plan,
		child: child,
	}
}

func (e *SortExecutor) PlanNode() planner.PlanNode {
	return e.plan
}

func (e *SortExecutor) Init(ctx *ExecutorContext) error {
	e.tuples = nil
	e.cursor = 0
	e.sorted = false
	e.current = storage.Tuple{}
	e.err = nil

	return e.child.Init(ctx)
}

func (e *SortExecutor) Next() bool {
	if e.err != nil {
		return false
	}

	if !e.sorted {
		desc := storage.NewRawTupleDesc(e.plan.Child.OutputSchema())

		for e.child.Next() {
			e.tuples = append(e.tuples, e.child.Current().DeepCopy(desc))
		}

		if err := e.child.Error(); err != nil {
			e.err = err
			return false
		}

		sort.SliceStable(e.tuples, func(i, j int) bool {
			for _, order := range e.plan.OrderBy {
				left := order.Expr.Eval(e.tuples[i])
				right := order.Expr.Eval(e.tuples[j])
				comparison := left.Compare(right)

				if comparison != 0 {
					if order.Direction == planner.SortOrderDescending {
						return comparison > 0
					}
					return comparison < 0
				}
			}
			return false
		})

		e.sorted = true
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
