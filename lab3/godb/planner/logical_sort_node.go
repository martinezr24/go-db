package planner

import (
	"fmt"
	"strings"
)

type LogicalSortNode struct {
	BaseLogicalPlanNode

	Child   LogicalPlanNode
	OrderBy []OrderByClause
}

func NewLogicalSortNode(child LogicalPlanNode, orderBy []OrderByClause) *LogicalSortNode {
	n := &LogicalSortNode{
		Child:   child,
		OrderBy: orderBy,
	}
	n.SetRequiredSchema(child.OutputSchema())
	return n
}

func (n *LogicalSortNode) OutputSchema() LogicalSchema {
	return n.Child.OutputSchema()
}

func (n *LogicalSortNode) Dependencies() LogicalSchema {
	deps := make(LogicalSchema, 0)
	for _, ob := range n.OrderBy {
		deps = append(deps, ob.Expr.GetReferencedColumns()...)
	}
	return deps
}

func (n *LogicalSortNode) Children() []LogicalPlanNode {
	return []LogicalPlanNode{n.Child}
}

func (n *LogicalSortNode) String() string {
	var sortKeys []string
	for _, o := range n.OrderBy {
		sortKeys = append(sortKeys, fmt.Sprintf("%s %s", o.Expr.String(), o.Direction.String()))
	}
	return fmt.Sprintf("LogicalSort: %s", strings.Join(sortKeys, ", "))
}
