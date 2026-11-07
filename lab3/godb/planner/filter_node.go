package planner

import (
	"fmt"

	"mit.edu/dsg/godb/common"
)

// FilterNode filters tuples from its child based on a predicate.
type FilterNode struct {
	Child     PlanNode
	Predicate Expr
}

func NewFilterNode(child PlanNode, predicate Expr) *FilterNode {
	return &FilterNode{
		Child:     child,
		Predicate: predicate,
	}
}

func (n *FilterNode) OutputSchema() []common.Type {
	return n.Child.OutputSchema()
}

func (n *FilterNode) Children() []PlanNode {
	return []PlanNode{n.Child}
}

func (n *FilterNode) String() string {
	return fmt.Sprintf("Filter: %s", n.Predicate.String())
}
