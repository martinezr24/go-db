package planner

import (
	"fmt"

	"mit.edu/dsg/godb/common"
)

// TopNNode represents a combined Sort + Limit operation (often using a heap).
type TopNNode struct {
	Child   PlanNode
	Limit   int
	OrderBy []OrderByClause
}

func NewTopNNode(child PlanNode, limit int, orderBy []OrderByClause) *TopNNode {
	return &TopNNode{
		Child:   child,
		Limit:   limit,
		OrderBy: orderBy,
	}
}

func (n *TopNNode) OutputSchema() []common.Type {
	return n.Child.OutputSchema()
}

func (n *TopNNode) Children() []PlanNode {
	return []PlanNode{n.Child}
}

func (n *TopNNode) String() string {
	return fmt.Sprintf("TopN: Limit %d", n.Limit)
}
