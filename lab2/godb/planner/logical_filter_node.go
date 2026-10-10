package planner

import (
	"fmt"
)

type LogicalFilterNode struct {
	BaseLogicalPlanNode

	Child     LogicalPlanNode
	Predicate Expr
}

func NewLogicalFilterNode(child LogicalPlanNode, predicate Expr) *LogicalFilterNode {
	n := &LogicalFilterNode{
		Child:     child,
		Predicate: predicate,
	}
	n.SetRequiredSchema(child.OutputSchema())
	return n
}

func (n *LogicalFilterNode) OutputSchema() LogicalSchema {
	return n.Child.OutputSchema()
}

func (n *LogicalFilterNode) Dependencies() LogicalSchema {
	return n.Predicate.GetReferencedColumns()
}

func (n *LogicalFilterNode) Children() []LogicalPlanNode {
	return []LogicalPlanNode{n.Child}
}

func (n *LogicalFilterNode) String() string {
	return fmt.Sprintf("LogicalFilter: %s", n.Predicate.String())
}
