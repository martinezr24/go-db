package execution

import (
	"fmt"

	"mit.edu/dsg/godb/common"
	"mit.edu/dsg/godb/planner"
	"mit.edu/dsg/godb/storage"
)

// AggregateExecutor implements hash-based aggregation.
type aggregateGroup struct {
	groupValues []common.Value
	counts      []int64
	values      []common.Value
	seen        []bool
}

type AggregateExecutor struct {
	plan  *planner.AggregateNode
	child Executor

	groups  map[string]*aggregateGroup
	order   []string
	results []storage.Tuple
	cursor  int
	built   bool
	current storage.Tuple
	err     error
}

func NewAggregateExecutor(plan *planner.AggregateNode, child Executor) *AggregateExecutor {
	return &AggregateExecutor{
		plan:  plan,
		child: child,
	}
}

func (e *AggregateExecutor) PlanNode() planner.PlanNode {
	return e.plan
}

func (e *AggregateExecutor) Init(ctx *ExecutorContext) error {
	e.groups = make(map[string]*aggregateGroup)
	e.order = nil
	e.results = nil
	e.cursor = 0
	e.built = false
	e.current = storage.Tuple{}
	e.err = nil

	for _, clause := range e.plan.AggClauses {
		if clause.Type == planner.AggSum && clause.Expr.OutputType() != common.IntType {
			e.err = fmt.Errorf("SUM requires an integer expression")
			return e.err
		}
	}

	return e.child.Init(ctx)
}

func (e *AggregateExecutor) Next() bool {
	if e.err != nil {
		return false
	}

	if !e.built {
		if !e.buildGroups() {
			return false
		}
		e.built = true
	}

	if e.cursor >= len(e.results) {
		return false
	}

	e.current = e.results[e.cursor]
	e.cursor++
	return true
}

func (e *AggregateExecutor) buildGroups() bool {
	groupTypes := make([]common.Type, len(e.plan.GroupByClause))
	for i, expr := range e.plan.GroupByClause {
		groupTypes[i] = expr.OutputType()
	}
	groupDesc := storage.NewRawTupleDesc(groupTypes)
	keyBuffer := make([]byte, groupDesc.BytesPerTuple())

	for e.child.Next() {
		input := e.child.Current()

		groupValues := make([]common.Value, len(e.plan.GroupByClause))
		for i, expr := range e.plan.GroupByClause {
			groupValues[i] = expr.Eval(input).Copy()
		}

		groupTuple := storage.FromValues(groupValues...)
		groupTuple.WriteToBuffer(keyBuffer, groupDesc)
		key := string(keyBuffer)

		group, exists := e.groups[key]
		if !exists {
			group = &aggregateGroup{
				groupValues: groupValues,
				counts:      make([]int64, len(e.plan.AggClauses)),
				values:      make([]common.Value, len(e.plan.AggClauses)),
				seen:        make([]bool, len(e.plan.AggClauses)),
			}
			e.groups[key] = group
			e.order = append(e.order, key)
		}

		for i, clause := range e.plan.AggClauses {
			value := clause.Expr.Eval(input).Copy()

			switch clause.Type {
			case planner.AggCount:
				if !value.IsNull() {
					group.counts[i]++
				}
			case planner.AggSum:
				if value.IsNull() {
					continue
				}
				if !group.seen[i] {
					group.values[i] = value
					group.seen[i] = true
				} else {
					sum := group.values[i].IntValue()
					addend := value.IntValue()
					const maxInt64 = 1<<63 - 1
					const minInt64 = -1 << 63
					if (addend > 0 && sum > maxInt64-addend) ||
						(addend < 0 && sum < minInt64-addend) {
						e.err = fmt.Errorf("SUM result overflows int64")
						return false
					}
					group.values[i] = common.NewIntValue(
						sum + addend,
					)
				}
			case planner.AggMin:
				if value.IsNull() {
					continue
				}
				if !group.seen[i] || value.Compare(group.values[i]) < 0 {
					group.values[i] = value
					group.seen[i] = true
				}
			case planner.AggMax:
				if value.IsNull() {
					continue
				}
				if !group.seen[i] || value.Compare(group.values[i]) > 0 {
					group.values[i] = value
					group.seen[i] = true
				}
			default:
				e.err = fmt.Errorf("unsupported aggregate type %v", clause.Type)
				return false
			}
		}
	}

	if err := e.child.Error(); err != nil {
		e.err = err
		return false
	}

	for _, key := range e.order {
		group := e.groups[key]
		row := make([]common.Value, 0, len(group.groupValues)+len(e.plan.AggClauses))
		row = append(row, group.groupValues...)

		for i, clause := range e.plan.AggClauses {
			switch clause.Type {
			case planner.AggCount:
				row = append(row, common.NewIntValue(group.counts[i]))
			case planner.AggSum, planner.AggMin, planner.AggMax:
				if group.seen[i] {
					row = append(row, group.values[i])
				} else if clause.Expr.OutputType() == common.StringType {
					row = append(row, common.NewNullString())
				} else {
					row = append(row, common.NewNullInt())
				}
			}
		}

		e.results = append(e.results, storage.FromValues(row...))
	}

	return true
}

func (e *AggregateExecutor) Current() storage.Tuple {
	return e.current
}

func (e *AggregateExecutor) Error() error {
	return e.err
}

func (e *AggregateExecutor) Close() error {
	return e.child.Close()
}
