package planner

import (
	"fmt"
)

type LogicalSubqueryNode struct {
	BaseLogicalPlanNode

	TableRef *TableRef
	Child    LogicalPlanNode

	// The optimizer may mutate outputSchema.
	outputSchema LogicalSchema

	// For debugging and pretty-printing
	subquery string
}

func NewLogicalSubqueryNode(aliasedTableRef *TableRef, child LogicalPlanNode, subquery string) *LogicalSubqueryNode {
	if aliasedTableRef == nil {
		panic("LogicalSubqueryNode requires a non-nil aliasedTableRef")
	}

	schema := make(LogicalSchema, len(aliasedTableRef.schema))
	copy(schema, aliasedTableRef.schema)

	n := &LogicalSubqueryNode{
		TableRef:     aliasedTableRef,
		Child:        child,
		outputSchema: schema,
		subquery:     subquery,
	}
	n.SetRequiredSchema(n.outputSchema)
	return n
}

func (n *LogicalSubqueryNode) OutputSchema() LogicalSchema {
	return n.outputSchema
}

func (n *LogicalSubqueryNode) Dependencies() LogicalSchema {
	return make(LogicalSchema, 0)
}

func (n *LogicalSubqueryNode) Children() []LogicalPlanNode {
	return []LogicalPlanNode{n.Child}
}

func (n *LogicalSubqueryNode) String() string {
	return fmt.Sprintf("LogicalSubqueryNode(%s): %s", n.TableRef.alias, n.subquery)
}
