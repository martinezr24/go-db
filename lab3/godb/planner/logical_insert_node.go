package planner

import (
	"fmt"

	"mit.edu/dsg/godb/common"
)

type LogicalInsertNode struct {
	BaseLogicalPlanNode

	TableOid common.ObjectID
	Child    LogicalPlanNode
}

func NewLogicalInsertNode(tableOid common.ObjectID, child LogicalPlanNode) *LogicalInsertNode {
	return &LogicalInsertNode{
		TableOid: tableOid,
		Child:    child,
	}
}

func (n *LogicalInsertNode) OutputSchema() LogicalSchema {
	intSchema := &LogicalColumn{
		cname:  "count",
		ctype:  common.IntType,
		origin: nil,
	}
	return LogicalSchema{intSchema} // Returns count of inserted rows
}

func (n *LogicalInsertNode) Children() []LogicalPlanNode {
	return []LogicalPlanNode{n.Child}
}

func (n *LogicalInsertNode) Dependencies() LogicalSchema {
	return make(LogicalSchema, 0)
}

func (n *LogicalInsertNode) String() string {
	return fmt.Sprintf("LogicalInsert: TableOID(%d)", n.TableOid)
}
