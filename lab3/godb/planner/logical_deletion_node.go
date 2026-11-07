package planner

import (
	"fmt"

	"mit.edu/dsg/godb/common"
)

type LogicalDeleteNode struct {
	BaseLogicalPlanNode

	TableOid common.ObjectID
	Child    LogicalPlanNode
}

func NewLogicalDeleteNode(tableOid common.ObjectID, child LogicalPlanNode) *LogicalDeleteNode {
	return &LogicalDeleteNode{
		TableOid: tableOid,
		Child:    child,
	}
}

func (n *LogicalDeleteNode) OutputSchema() LogicalSchema {
	intSchema := &LogicalColumn{
		cname:  "deleted_count",
		ctype:  common.IntType,
		origin: nil,
	}
	return LogicalSchema{intSchema} // Returns count of deleted rows
}

func (n *LogicalDeleteNode) Children() []LogicalPlanNode {
	return []LogicalPlanNode{n.Child}
}

func (n *LogicalDeleteNode) Dependencies() LogicalSchema {
	return make(LogicalSchema, 0)
}

func (n *LogicalDeleteNode) String() string {
	return fmt.Sprintf("LogicalDelete: TableOID(%d)", n.TableOid)
}
