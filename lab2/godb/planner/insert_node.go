package planner

import (
	"fmt"

	"mit.edu/dsg/godb/common"
)

// InsertNode represents an insertion into a table.
type InsertNode struct {
	TableOid common.ObjectID
	Child    PlanNode
}

func NewInsertNode(tableOid common.ObjectID, child PlanNode) *InsertNode {
	return &InsertNode{
		TableOid: tableOid,
		Child:    child,
	}
}

func (n *InsertNode) OutputSchema() []common.Type {
	return []common.Type{common.IntType} // Returns count of inserted rows
}

func (n *InsertNode) Children() []PlanNode {
	return []PlanNode{n.Child}
}

func (n *InsertNode) String() string {
	return fmt.Sprintf("Insert: TableOID(%d)", n.TableOid)
}
