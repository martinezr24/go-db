package planner

import (
	"fmt"

	"mit.edu/dsg/godb/common"
)

// HashJoinNode represents a hash join between two children.
type HashJoinNode struct {
	Left         PlanNode
	Right        PlanNode
	LeftKeys     []Expr
	RightKeys    []Expr
	outputSchema []common.Type
}

func NewHashJoinNode(left, right PlanNode, leftKeys, rightKeys []Expr) *HashJoinNode {
	return &HashJoinNode{
		Left:         left,
		Right:        right,
		LeftKeys:     leftKeys,
		RightKeys:    rightKeys,
		outputSchema: append(left.OutputSchema(), right.OutputSchema()...),
	}
}

func (n *HashJoinNode) OutputSchema() []common.Type {
	return n.outputSchema
}

func (n *HashJoinNode) Children() []PlanNode {
	return []PlanNode{n.Left, n.Right}
}

func (n *HashJoinNode) String() string {
	return fmt.Sprintf("HashJoin: %v = %v", n.LeftKeys, n.RightKeys)
}

// NestedLoopJoinNode represents a block nested loop join.
type NestedLoopJoinNode struct {
	Left         PlanNode
	Right        PlanNode
	Predicate    Expr
	outputSchema []common.Type
}

func NewBlockNestedLoopJoinNode(left, right PlanNode, predicate Expr) *NestedLoopJoinNode {
	return &NestedLoopJoinNode{
		Left:         left,
		Right:        right,
		Predicate:    predicate,
		outputSchema: append(left.OutputSchema(), right.OutputSchema()...),
	}
}

func (n *NestedLoopJoinNode) OutputSchema() []common.Type {
	return n.outputSchema
}

func (n *NestedLoopJoinNode) Children() []PlanNode {
	return []PlanNode{n.Left, n.Right}
}

func (n *NestedLoopJoinNode) String() string {
	return fmt.Sprintf("BNLJ: %s", n.Predicate.String())
}

// IndexNestedLoopJoinNode represents a join where the right side is accessed via an index lookup.
// The "LeftKeys" here are the expressions from the Left tuple used to probe the Right Index.
type IndexNestedLoopJoinNode struct {
	Left          PlanNode
	LeftKeys      []Expr // Expressions from Left to probe the index
	RightTableOid common.ObjectID
	RightIndexOid common.ObjectID
	ForUpdate     bool
	outputSchema  []common.Type
}

func NewIndexNestedLoopJoinNode(left PlanNode, innerTableOid, innerIndexOid common.ObjectID, rightKeys []Expr, innerSchema []common.Type, forUpdate bool) *IndexNestedLoopJoinNode {
	return &IndexNestedLoopJoinNode{
		Left:          left,
		RightTableOid: innerTableOid,
		RightIndexOid: innerIndexOid,
		LeftKeys:      rightKeys,
		outputSchema:  append(left.OutputSchema(), innerSchema...),
		ForUpdate:     forUpdate,
	}
}

func (n *IndexNestedLoopJoinNode) OutputSchema() []common.Type {
	return n.outputSchema
}

func (n *IndexNestedLoopJoinNode) Children() []PlanNode {
	return []PlanNode{n.Left}
}

func (n *IndexNestedLoopJoinNode) String() string {
	return fmt.Sprintf("IndexJoin: Table(%d) via Index(%d), outputSchema=%v", n.RightTableOid, n.RightIndexOid, n.outputSchema)
}

// SortMergeJoinNode represents a join where both children are sorted on the join keys.
type SortMergeJoinNode struct {
	Left         PlanNode
	Right        PlanNode
	LeftKeys     []Expr
	RightKeys    []Expr
	outputSchema []common.Type
}

func NewSortMergeJoinNode(left, right PlanNode, leftKeys, rightKeys []Expr) *SortMergeJoinNode {
	return &SortMergeJoinNode{
		Left:         left,
		Right:        right,
		LeftKeys:     leftKeys,
		RightKeys:    rightKeys,
		outputSchema: append(left.OutputSchema(), right.OutputSchema()...),
	}
}

func (n *SortMergeJoinNode) OutputSchema() []common.Type {
	return n.outputSchema
}

func (n *SortMergeJoinNode) Children() []PlanNode {
	return []PlanNode{n.Left, n.Right}
}

func (n *SortMergeJoinNode) String() string {
	return fmt.Sprintf("SortMergeJoin: %v = %v", n.LeftKeys, n.RightKeys)
}
