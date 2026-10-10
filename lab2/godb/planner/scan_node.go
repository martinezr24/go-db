package planner

import (
	"fmt"

	"mit.edu/dsg/godb/common"
	"mit.edu/dsg/godb/indexing"
	"mit.edu/dsg/godb/transaction"
)

// SeqScanNode represents a sequential scan over a table.
// It uses the TableOid to identify the target table.
type SeqScanNode struct {
	TableOid     common.ObjectID
	Mode         transaction.DBLockMode
	outputSchema []common.Type
}

func NewSeqScanNode(tableOid common.ObjectID, outputSchema []common.Type, mode transaction.DBLockMode) *SeqScanNode {
	return &SeqScanNode{
		TableOid:     tableOid,
		Mode:         mode,
		outputSchema: outputSchema,
	}
}

func (n *SeqScanNode) OutputSchema() []common.Type {
	return n.outputSchema
}

func (n *SeqScanNode) Children() []PlanNode {
	return nil
}

func (n *SeqScanNode) String() string {
	return fmt.Sprintf("SeqScan: TableOID(%d)", n.TableOid)
}

// IndexScanNode represents a scan using an index.
type IndexScanNode struct {
	IndexOid     common.ObjectID
	TableOid     common.ObjectID
	StartKey     indexing.Key
	Direction    indexing.ScanDirection
	ForUpdate    bool
	outputSchema []common.Type
}

func NewIndexScanNode(indexOid common.ObjectID, tableOid common.ObjectID, outputSchema []common.Type, direction indexing.ScanDirection, startKey indexing.Key, forUpdate bool) *IndexScanNode {
	return &IndexScanNode{
		IndexOid:     indexOid,
		TableOid:     tableOid,
		StartKey:     startKey,
		Direction:    direction,
		outputSchema: outputSchema,
		ForUpdate:    forUpdate,
	}
}

func (n *IndexScanNode) OutputSchema() []common.Type {
	return n.outputSchema
}

func (n *IndexScanNode) Children() []PlanNode {
	return nil
}

func (n *IndexScanNode) String() string {
	return fmt.Sprintf("IndexScan: IndexOID(%d) on TableOID(%d)", n.IndexOid, n.TableOid)
}

/*
BindKey allows the optimizer to bind a concrete index key schema to this IndexScanNode.
This allows the executor to compare the key schemas
*/
func (n *IndexScanNode) BindKey(index indexing.Index) error {
	n.StartKey = index.Metadata().AsKey(n.StartKey.RawTuple)
	return nil
}
