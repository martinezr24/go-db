package planner

import "mit.edu/dsg/godb/common"

type LogicalUpdateNode struct {
	BaseLogicalPlanNode

	TableOid common.ObjectID
	Child    LogicalPlanNode

	// Updates maps the target logical column to the expression for the new value.
	Updates map[*LogicalColumn]Expr
}

func NewLogicalUpdateNode(tableOid common.ObjectID, child LogicalPlanNode, updates map[*LogicalColumn]Expr) *LogicalUpdateNode {
	return &LogicalUpdateNode{
		TableOid: tableOid,
		Child:    child,
		Updates:  updates,
	}
}

func (n *LogicalUpdateNode) OutputSchema() LogicalSchema {
	return LogicalSchema{&LogicalColumn{cname: "count", ctype: common.IntType}}
}

func (n *LogicalUpdateNode) Children() []LogicalPlanNode {
	return []LogicalPlanNode{n.Child}
}

func (n *LogicalUpdateNode) Dependencies() LogicalSchema {
	deps := make(LogicalSchema, 0)
	for _, expr := range n.Updates {
		deps = append(deps, expr.GetReferencedColumns()...)
	}

	return deps
}

func (n *LogicalUpdateNode) String() string { return "LogicalUpdate" }
