package planner

import (
	"fmt"

	"mit.edu/dsg/godb/common"
	"mit.edu/dsg/godb/indexing"
)

// IndexLookupNode represents a point lookup (equality match) using an index.
type IndexLookupNode struct {
	IndexOid     common.ObjectID
	TableOid     common.ObjectID
	EqualityKey  indexing.Key
	ForUpdate    bool
	outputSchema []common.Type
}

func NewIndexLookupNode(indexOid common.ObjectID, tableOid common.ObjectID, outputSchema []common.Type, key indexing.Key, forUpdate bool) *IndexLookupNode {
	return &IndexLookupNode{
		IndexOid:     indexOid,
		TableOid:     tableOid,
		EqualityKey:  key,
		outputSchema: outputSchema,
		ForUpdate:    forUpdate,
	}
}

func (n *IndexLookupNode) OutputSchema() []common.Type {
	return n.outputSchema
}

func (n *IndexLookupNode) Children() []PlanNode {
	return nil
}

func (n *IndexLookupNode) String() string {
	return fmt.Sprintf("IndexProbe: IndexOID(%d) with Key %v", n.IndexOid, n.EqualityKey)
}

/*
BindKey allows the optimizer to bind a concrete index key schema to this IndexScanNode.
This allows the executor to compare the key schemas
*/
func (n *IndexLookupNode) BindKey(index indexing.Index) error {
	n.EqualityKey = index.Metadata().AsKey(n.EqualityKey.RawTuple)
	return nil
}
