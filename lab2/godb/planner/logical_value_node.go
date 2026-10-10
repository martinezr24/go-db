package planner

type LogicalValuesNode struct {
	BaseLogicalPlanNode

	Values [][]Expr // row major
	Schema LogicalSchema
}

func NewLogicalValuesNode(values [][]Expr, schema LogicalSchema) *LogicalValuesNode {
	return &LogicalValuesNode{
		Values: values,
		Schema: schema,
	}
}

func (n *LogicalValuesNode) OutputSchema() LogicalSchema {
	return n.Schema
}

func (n *LogicalValuesNode) Children() []LogicalPlanNode {
	return nil
}

func (n *LogicalValuesNode) Dependencies() LogicalSchema { return make(LogicalSchema, 0) }

func (n *LogicalValuesNode) String() string { return "LogicalValues" }
