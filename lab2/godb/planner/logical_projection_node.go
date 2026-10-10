package planner

import (
	"fmt"
	"strings"
)

type LogicalProjectionNode struct {
	BaseLogicalPlanNode

	Child        LogicalPlanNode
	Expressions  []Expr
	outputSchema LogicalSchema // The schema is separate to account for e.g. SELECT t.a, t.a AS b FROM t.
}

func NewLogicalProjectionNode(child LogicalPlanNode, exprs []Expr, schema LogicalSchema) *LogicalProjectionNode {
	n := &LogicalProjectionNode{
		Child:        child,
		Expressions:  exprs,
		outputSchema: schema,
	}
	n.SetRequiredSchema(schema)
	return n
}

func (n *LogicalProjectionNode) OutputSchema() LogicalSchema {
	return n.outputSchema
}

func (n *LogicalProjectionNode) Dependencies() LogicalSchema {
	deps := make(LogicalSchema, 0)
	for _, expr := range n.Expressions {
		deps = append(deps, expr.GetReferencedColumns()...)
	}
	return deps
}

func (n *LogicalProjectionNode) Children() []LogicalPlanNode {
	return []LogicalPlanNode{n.Child}
}

func (n *LogicalProjectionNode) String() string {
	var exprs []string
	for i, e := range n.Expressions {
		// Print "Expr AS Alias"
		alias := n.outputSchema[i].cname
		exprs = append(exprs, fmt.Sprintf("%s AS %s", e.String(), alias))
	}
	return fmt.Sprintf("LogicalProjection: %s", strings.Join(exprs, ", "))
}
