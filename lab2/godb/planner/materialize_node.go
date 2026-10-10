package planner

import (
	"mit.edu/dsg/godb/common"
)

// MaterializeNode acts as a pipeline barrier, fully buffering the child to reuse tuples on a rescan
type MaterializeNode struct {
	Child PlanNode
}

func NewMaterializeNode(child PlanNode) *MaterializeNode {
	return &MaterializeNode{
		Child: child,
	}
}

func (n *MaterializeNode) OutputSchema() []common.Type {
	return n.Child.OutputSchema()
}

func (n *MaterializeNode) Children() []PlanNode {
	return []PlanNode{n.Child}
}

func (n *MaterializeNode) String() string {
	return "Materialize"
}
