package execution

import (
	"container/heap"
	"sort"

	"mit.edu/dsg/godb/planner"
	"mit.edu/dsg/godb/storage"
)

// TopNExecutor optimizes "ORDER BY ... LIMIT N" queries.
//
// This should allow an optimized implementation that avoids sorting ALL tuples (O(M log M)).
type topNItem struct {
	tuple   storage.Tuple
	ordinal int
}

type topNHeap struct {
	items   []topNItem
	orderBy []planner.OrderByClause
}

func (h topNHeap) Len() int { return len(h.items) }

// Less puts the worst retained row at the root so it can be replaced quickly.
func (h topNHeap) Less(i, j int) bool {
	comparison := compareTopNRows(h.items[i].tuple, h.items[j].tuple, h.orderBy)
	if comparison == 0 {
		return h.items[i].ordinal > h.items[j].ordinal
	}
	return comparison > 0
}

func (h topNHeap) Swap(i, j int) {
	h.items[i], h.items[j] = h.items[j], h.items[i]
}

func (h *topNHeap) Push(value any) {
	h.items = append(h.items, value.(topNItem))
}

func (h *topNHeap) Pop() any {
	last := len(h.items) - 1
	item := h.items[last]
	h.items = h.items[:last]
	return item
}

type TopNExecutor struct {
	plan  *planner.TopNNode
	child Executor

	tuples  []storage.Tuple
	cursor  int
	built   bool
	current storage.Tuple
	err     error
}

func NewTopNExecutor(plan *planner.TopNNode, child Executor) *TopNExecutor {
	return &TopNExecutor{
		plan:  plan,
		child: child,
	}
}

func (e *TopNExecutor) PlanNode() planner.PlanNode {
	return e.plan
}

func (e *TopNExecutor) Init(ctx *ExecutorContext) error {
	e.tuples = nil
	e.cursor = 0
	e.built = false
	e.current = storage.Tuple{}
	e.err = nil

	return e.child.Init(ctx)
}

func (e *TopNExecutor) Next() bool {
	if e.err != nil {
		return false
	}

	if !e.built {
		e.built = true

		if e.plan.Limit <= 0 {
			return false
		}

		desc := storage.NewRawTupleDesc(e.plan.Child.OutputSchema())
		candidates := &topNHeap{orderBy: e.plan.OrderBy}
		heap.Init(candidates)

		ordinal := 0
		for e.child.Next() {
			row := e.child.Current().DeepCopy(desc)
			item := topNItem{tuple: row, ordinal: ordinal}
			ordinal++

			if candidates.Len() < e.plan.Limit {
				heap.Push(candidates, item)
				continue
			}

			if compareTopNRows(row, candidates.items[0].tuple, e.plan.OrderBy) < 0 {
				candidates.items[0] = item
				heap.Fix(candidates, 0)
			}
		}

		if err := e.child.Error(); err != nil {
			e.err = err
			return false
		}

		sort.SliceStable(candidates.items, func(i, j int) bool {
			comparison := compareTopNRows(
				candidates.items[i].tuple,
				candidates.items[j].tuple,
				e.plan.OrderBy,
			)
			if comparison == 0 {
				return candidates.items[i].ordinal < candidates.items[j].ordinal
			}
			return comparison < 0
		})

		for _, item := range candidates.items {
			e.tuples = append(e.tuples, item.tuple)
		}
	}

	if e.cursor >= len(e.tuples) {
		return false
	}

	e.current = e.tuples[e.cursor]
	e.cursor++
	return true
}

func compareTopNRows(left, right storage.Tuple, orderBy []planner.OrderByClause) int {
	for _, clause := range orderBy {
		leftValue := clause.Expr.Eval(left)
		rightValue := clause.Expr.Eval(right)
		comparison := leftValue.Compare(rightValue)

		if comparison != 0 {
			if clause.Direction == planner.SortOrderDescending {
				return -comparison
			}
			return comparison
		}
	}
	return 0
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
