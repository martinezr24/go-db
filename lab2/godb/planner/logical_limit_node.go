package planner

import (
	"fmt"
)

type LogicalLimitNode struct {
	BaseLogicalPlanNode

	Child  LogicalPlanNode
	Limit  int
	Offset int
}

func NewLogicalLimitNode(child LogicalPlanNode, limit int, offset int) *LogicalLimitNode {
	n := &LogicalLimitNode{
		Child:  child,
		Limit:  limit,
		Offset: offset,
	}
	n.SetRequiredSchema(child.OutputSchema())
	return n
}

func (n *LogicalLimitNode) OutputSchema() LogicalSchema {
	return n.Child.OutputSchema()
}

func (n *LogicalLimitNode) Dependencies() LogicalSchema {
	return make(LogicalSchema, 0)
}

func (n *LogicalLimitNode) Children() []LogicalPlanNode {
	return []LogicalPlanNode{n.Child}
}

func (n *LogicalLimitNode) String() string {
	return fmt.Sprintf("LogicalLimit: %d OFFSET %d", n.Limit, n.Offset)
}
