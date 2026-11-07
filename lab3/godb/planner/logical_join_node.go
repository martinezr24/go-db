package planner

import (
	"fmt"
	"strings"
)

type JoinType int

const (
	Inner JoinType = iota
	Left
	Right
	FullOuter
	Cross
)

func (jt JoinType) String() string {
	switch jt {
	case Inner:
		return "INNER JOIN"
	case Left:
		return "LEFT JOIN"
	case Right:
		return "RIGHT JOIN"
	case FullOuter:
		return "FULL OUTER JOIN"
	case Cross:
		return "CROSS JOIN"
	}
	return "UNKNOWN JOIN TYPE"
}

type LogicalJoinNode struct {
	BaseLogicalPlanNode

	Left     LogicalPlanNode
	Right    LogicalPlanNode
	joinOn   []Expr
	joinType JoinType

	outputSchema LogicalSchema
}

func NewLogicalJoinNode(left, right LogicalPlanNode, joinOn []Expr, joinType JoinType) *LogicalJoinNode {
	node := &LogicalJoinNode{
		Left:     left,
		Right:    right,
		joinOn:   joinOn,
		joinType: joinType,
	}
	leftSchema := node.Left.OutputSchema()
	rightSchema := node.Right.OutputSchema()

	node.outputSchema = make(LogicalSchema, 0, len(leftSchema)+len(rightSchema))
	node.outputSchema = append(node.outputSchema, leftSchema...)
	node.outputSchema = append(node.outputSchema, rightSchema...)
	node.SetRequiredSchema(node.outputSchema)
	return node
}

func (n *LogicalJoinNode) Children() []LogicalPlanNode { return []LogicalPlanNode{n.Left, n.Right} }

func (n *LogicalJoinNode) OutputSchema() LogicalSchema {
	outputSchema := make(LogicalSchema, 0, len(n.Left.OutputSchema())+len(n.Right.OutputSchema()))
	outputSchema = append(outputSchema, n.Left.OutputSchema()...)
	outputSchema = append(outputSchema, n.Right.OutputSchema()...)
	return outputSchema
}

func (n *LogicalJoinNode) RawOutputSchema() LogicalSchema {
	return n.outputSchema
}

func (n *LogicalJoinNode) Dependencies() LogicalSchema {
	deps := make(LogicalSchema, 0)
	for _, expr := range n.joinOn {
		// e.g. ON u.id = o.user_id
		deps = append(deps, expr.GetReferencedColumns()...)
	}
	return deps
}
func (n *LogicalJoinNode) String() string {
	sb := strings.Builder{}
	sb.WriteString(fmt.Sprintf("LogicalJoin: %s", n.joinType.String()))
	if len(n.joinOn) > 0 {
		sb.WriteString(" ON ")
		var conds []string
		for _, c := range n.joinOn {
			conds = append(conds, c.String())
		}
		sb.WriteString(strings.Join(conds, " AND "))
	}
	return sb.String()
}
