package planner

import (
	"mit.edu/dsg/godb/common"
)

type SortDirection int

const (
	SortOrderAscending SortDirection = iota
	SortOrderDescending
)

func (d SortDirection) String() string {
	if d == SortOrderAscending {
		return "ASC"
	}
	return "DESC"
}

type OrderByClause struct {
	Expr      Expr
	Direction SortDirection
}

// SortNode sorts the input tuples.
type SortNode struct {
	Child   PlanNode
	OrderBy []OrderByClause
}

func NewSortNode(child PlanNode, orderBy []OrderByClause) *SortNode {
	return &SortNode{
		Child:   child,
		OrderBy: orderBy,
	}
}

func (n *SortNode) OutputSchema() []common.Type {
	return n.Child.OutputSchema()
}

func (n *SortNode) Children() []PlanNode {
	return []PlanNode{n.Child}
}

func (n *SortNode) String() string {
	return "Sort"
}
