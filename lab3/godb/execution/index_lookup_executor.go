package execution

import (
	"errors"

	"mit.edu/dsg/godb/common"
	"mit.edu/dsg/godb/indexing"
	"mit.edu/dsg/godb/planner"
	"mit.edu/dsg/godb/storage"
	"mit.edu/dsg/godb/transaction"
)

// IndexLookupExecutor implements a Point Lookup using an index. Unlike a full Index Scan, which iterates over a
// range of keys, this executor efficiently retrieves only the tuples that match a specific equality key
// (e.g., "SELECT * FROM users WHERE id = 5").
type IndexLookupExecutor struct {
	plan      *planner.IndexLookupNode
	index     indexing.Index
	tableHeap *TableHeap
	txn       *transaction.TransactionContext

	rids    []common.RecordID
	cursor  int
	buffer  []byte
	current storage.Tuple
	err     error
}

func NewIndexLookupExecutor(plan *planner.IndexLookupNode, index indexing.Index, tableHeap *TableHeap) *IndexLookupExecutor {
	return &IndexLookupExecutor{
		plan:      plan,
		index:     index,
		tableHeap: tableHeap,
	}
}

func (e *IndexLookupExecutor) PlanNode() planner.PlanNode {
	return e.plan
}

func (e *IndexLookupExecutor) Init(ctx *ExecutorContext) error {
	e.txn = ctx.GetTransaction()
	e.rids = nil
	e.cursor = 0
	e.current = storage.Tuple{}
	e.err = nil
	e.buffer = make([]byte, e.tableHeap.StorageSchema().BytesPerTuple())

	rids, err := e.index.ScanKey(e.plan.EqualityKey, nil, e.txn)
	if err != nil {
		e.err = err
		return err
	}
	e.rids = rids
	return nil
}

func keyFromHeapTuple(index indexing.Index, tableSchema *storage.RawTupleDesc, tuple storage.RawTuple) indexing.Key {
	metadata := index.Metadata()
	keyTuple := make(storage.RawTuple, metadata.KeySchema.BytesPerTuple())
	for keyColumn, tableColumn := range metadata.ProjectionList {
		metadata.KeySchema.SetValue(
			keyTuple,
			keyColumn,
			tableSchema.GetValue(tuple, tableColumn),
		)
	}
	return indexing.NewKey(keyTuple, metadata.KeySchema)
}

func (e *IndexLookupExecutor) Next() bool {
	if e.err != nil {
		return false
	}

	e.current = storage.Tuple{}
	for e.cursor < len(e.rids) {
		rid := e.rids[e.cursor]
		e.cursor++

		if err := e.tableHeap.ReadTuple(e.txn, rid, e.buffer, e.plan.ForUpdate); err != nil {
			if errors.Is(err, ErrTupleDeleted) {
				continue
			}
			e.err = err
			return false
		}

		actualKey := keyFromHeapTuple(e.index, e.tableHeap.StorageSchema(), e.buffer)
		if !actualKey.Equals(e.plan.EqualityKey) {
			continue
		}

		e.current = storage.FromRawTuple(
			e.buffer,
			e.tableHeap.StorageSchema(),
			rid,
		)
		return true
	}

	return false
}

func (e *IndexLookupExecutor) Current() storage.Tuple {
	return e.current
}

func (e *IndexLookupExecutor) Close() error {
	return nil
}

func (e *IndexLookupExecutor) Error() error {
	return e.err
}
