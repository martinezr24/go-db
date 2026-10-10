package planner

import (
	"fmt"
	"strings"

	"mit.edu/dsg/godb/common"
)

// ProjectionNode projects specific columns or expressions from its child.
type ProjectionNode struct {
	Child       PlanNode
	Expressions []Expr
	Passthrough []int // Optimization: indices of columns to pass through directly
}

func NewProjectionNode(child PlanNode, exprs []Expr) *ProjectionNode {
	return &ProjectionNode{
		Child:       child,
		Expressions: exprs,
	}
}

func (n *ProjectionNode) OutputSchema() []common.Type {
	out := make([]common.Type, len(n.Expressions))
	for i, e := range n.Expressions {
		out[i] = e.OutputType()
	}
	return out
}

func (n *ProjectionNode) Children() []PlanNode {
	return []PlanNode{n.Child}
}

func (n *ProjectionNode) String() string {
	var exprs []string
	for _, e := range n.Expressions {
		exprs = append(exprs, fmt.Sprintf("%s |", e.String()))
	}
	return fmt.Sprintf("Projection: %s", strings.Join(exprs, ", "))
}
