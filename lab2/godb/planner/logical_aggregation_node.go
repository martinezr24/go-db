package planner

import (
	"fmt"
)

type LogicalAggregationNode struct {
	BaseLogicalPlanNode

	Child         LogicalPlanNode
	GroupByClause []Expr
	AggClauses    []AggregateClause

	outputSchema LogicalSchema
}

func NewLogicalAggregationNode(child LogicalPlanNode, groupBy []Expr, aggregates []AggregateClause) *LogicalAggregationNode {
	outputSchema := make([]*LogicalColumn, 0, len(groupBy)+len(aggregates))

	for _, groupExpr := range groupBy {
		colName := ""
		var origin *TableRef

		if col, ok := groupExpr.(*LogicalColumn); ok {
			colName = col.cname
			origin = col.origin
		} else {
			colName = groupExpr.String()
			origin = nil
		}

		outputSchema = append(outputSchema, &LogicalColumn{
			cname:  colName,
			ctype:  groupExpr.OutputType(),
			origin: origin,
		})
	}

	for _, agg := range aggregates {
		colName := fmt.Sprintf("%s(%s)", agg.Type.String(), agg.Expr.String())

		outputSchema = append(outputSchema, &LogicalColumn{
			cname:  colName,
			ctype:  agg.Expr.OutputType(),
			origin: nil,
		})
	}

	n := &LogicalAggregationNode{
		Child:         child,
		GroupByClause: groupBy,
		AggClauses:    aggregates,
		outputSchema:  outputSchema,
	}
	n.SetRequiredSchema(outputSchema)
	return n
}

func (n *LogicalAggregationNode) OutputSchema() LogicalSchema {
	return n.outputSchema
}

func (n *LogicalAggregationNode) Dependencies() LogicalSchema {
	deps := make(LogicalSchema, 0)

	for _, expr := range n.GroupByClause {
		deps = append(deps, expr.GetReferencedColumns()...)
	}

	for _, agg := range n.AggClauses {
		deps = append(deps, agg.Expr.GetReferencedColumns()...)
	}

	return deps
}

func (n *LogicalAggregationNode) Children() []LogicalPlanNode {
	return []LogicalPlanNode{n.Child}
}

func (n *LogicalAggregationNode) String() string {
	return fmt.Sprintf("LogicalAggregation: Aggregates %v, GroupBy(%v)", n.AggClauses, n.GroupByClause)
}
