package execution

import (
	"errors"

	"mit.edu/dsg/godb/indexing"
	"mit.edu/dsg/godb/planner"
	"mit.edu/dsg/godb/storage"
	"mit.edu/dsg/godb/transaction"
)

// IndexScanExecutor executes a range scan over an index.
// It iterates through the B+Tree (or other index type) starting from a specific key
// and traversing in a specific direction (Forward or Backward).
type IndexScanExecutor struct {
	plan      *planner.IndexScanNode
	index     indexing.Index
	tableHeap *TableHeap
	txn       *transaction.TransactionContext
	iterator  indexing.ScanIterator
	buffer    []byte
	current   storage.Tuple
	err       error
}

func NewIndexScanExecutor(plan *planner.IndexScanNode, index indexing.Index, tableHeap *TableHeap) *IndexScanExecutor {
	return &IndexScanExecutor{
		plan:      plan,
		index:     index,
		tableHeap: tableHeap,
	}
}

func (e *IndexScanExecutor) PlanNode() planner.PlanNode {
	return e.plan
}

func (e *IndexScanExecutor) Init(ctx *ExecutorContext) error {
	if e.iterator != nil {
		if err := e.iterator.Close(); err != nil {
			e.err = err
			return err
		}
		e.iterator = nil
	}

	e.err = nil
	e.current = storage.Tuple{}
	e.buffer = make([]byte, e.tableHeap.StorageSchema().BytesPerTuple())
	e.txn = ctx.GetTransaction()

	iterator, err := e.index.Scan(
		e.plan.StartKey,
		e.plan.Direction,
		e.txn,
	)
	if err != nil {
		e.err = err
		return err
	}

	e.iterator = iterator
	return nil
}

func (e *IndexScanExecutor) Next() bool {
	if e.err != nil {
		return false
	}

	e.current = storage.Tuple{}
	for e.iterator.Next() {
		rid := e.iterator.Value()
		indexKey := e.iterator.Key()
		if err := e.tableHeap.ReadTuple(
			e.txn,
			rid,
			e.buffer,
			e.plan.ForUpdate,
		); err != nil {
			if errors.Is(err, ErrTupleDeleted) {
				continue
			}
			e.err = err
			return false
		}

		actualKey := keyFromHeapTuple(e.index, e.tableHeap.StorageSchema(), e.buffer)
		if !actualKey.Equals(indexKey) {
			continue
		}

		e.current = storage.FromRawTuple(
			e.buffer,
			e.tableHeap.StorageSchema(),
			rid,
		)
		return true
	}

	e.err = e.iterator.Error()
	return false
}

func (e *IndexScanExecutor) Current() storage.Tuple {
	return e.current
}

func (e *IndexScanExecutor) Close() error {
	if e.iterator == nil {
		return nil
	}
	if err := e.iterator.Close(); err != nil {
		return err
	}
	e.iterator = nil
	return nil
}

func (e *IndexScanExecutor) Error() error {
	return e.err
}
