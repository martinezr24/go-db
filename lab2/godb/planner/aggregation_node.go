package planner

import (
	"fmt"

	"mit.edu/dsg/godb/common"
)

type AggregatorType int

const (
	AggCount AggregatorType = iota
	AggSum
	AggMin
	AggMax
)

// String() returns the string representation of the AggregatorType
// Used for the binding process and debugging.
func (a AggregatorType) String() string {
	switch a {
	case AggCount:
		return "COUNT"
	case AggSum:
		return "SUM"
	case AggMin:
		return "MIN"
	case AggMax:
		return "MAX"
	default:
		return fmt.Sprintf("AggregatorType(%d)", int(a))
	}
}

type AggregateClause struct {
	Type AggregatorType
	Expr Expr
}

// AggregateNode represents a group-by and aggregation operation.
type AggregateNode struct {
	Child         PlanNode
	GroupByClause []Expr
	AggClauses    []AggregateClause
	outputSchema  []common.Type
}

func NewAggregateNode(child PlanNode, groupBy []Expr, aggregates []AggregateClause) *AggregateNode {
	outputSchema := make([]common.Type, len(groupBy)+len(aggregates))
	for i, expr := range groupBy {
		outputSchema[i] = expr.OutputType()
	}
	for i, agg := range aggregates {
		outputSchema[len(groupBy)+i] = agg.Expr.OutputType()
	}

	return &AggregateNode{
		Child:         child,
		GroupByClause: groupBy,
		AggClauses:    aggregates,
		outputSchema:  outputSchema,
	}
}

func (n *AggregateNode) OutputSchema() []common.Type {
	return n.outputSchema
}

func (n *AggregateNode) Children() []PlanNode {
	return []PlanNode{n.Child}
}

func (n *AggregateNode) String() string {
	return fmt.Sprintf("Aggregate: GroupBy(%v)", n.GroupByClause)
}
