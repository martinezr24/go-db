package planner

import "mit.edu/dsg/godb/common"

// ValuesNode represents a set of constant rows to be emitted.
// Example: INSERT INTO T VALUES (1, 'a'), (2, 'b');
type ValuesNode struct {
	// Outer list = rows, Inner list = columns/expressions for that row
	Values [][]Expr
	Schema []common.Type
}

func NewValuesNode(values [][]Expr, schema []common.Type) *ValuesNode {
	return &ValuesNode{
		Values: values,
		Schema: schema,
	}
}

func (n *ValuesNode) OutputSchema() []common.Type {
	return n.Schema
}

func (n *ValuesNode) Children() []PlanNode {
	return nil // Leaf node
}

func (n *ValuesNode) String() string {
	return "ValuesNode"
}
