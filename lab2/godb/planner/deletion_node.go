package planner

import (
	"fmt"

	"mit.edu/dsg/godb/common"
)

// DeleteNode represents a deletion from a table.
type DeleteNode struct {
	TableOid common.ObjectID
	Child    PlanNode
}

func NewDeleteNode(tableOid common.ObjectID, child PlanNode) *DeleteNode {
	return &DeleteNode{
		TableOid: tableOid,
		Child:    child,
	}
}

func (n *DeleteNode) OutputSchema() []common.Type {
	return []common.Type{common.IntType} // Returns count of deleted rows
}

func (n *DeleteNode) Children() []PlanNode {
	return []PlanNode{n.Child}
}

func (n *DeleteNode) String() string {
	return fmt.Sprintf("Delete: TableOID(%d)", n.TableOid)
}
