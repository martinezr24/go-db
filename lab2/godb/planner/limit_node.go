package planner

import (
	"fmt"

	"mit.edu/dsg/godb/common"
)

// LimitNode limits the number of output tuples.
type LimitNode struct {
	Child PlanNode
	Limit int
}

func NewLimitNode(child PlanNode, limit int) *LimitNode {
	return &LimitNode{
		Child: child,
		Limit: limit,
	}
}

func (n *LimitNode) OutputSchema() []common.Type {
	return n.Child.OutputSchema()
}

func (n *LimitNode) Children() []PlanNode {
	return []PlanNode{n.Child}
}

func (n *LimitNode) String() string {
	return fmt.Sprintf("Limit: %d", n.Limit)
}
