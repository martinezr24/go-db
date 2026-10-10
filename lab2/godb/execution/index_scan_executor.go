package execution

import (
	"mit.edu/dsg/godb/indexing"
	"mit.edu/dsg/godb/planner"
	"mit.edu/dsg/godb/storage"
)

// IndexScanExecutor executes a range scan over an index.
// It iterates through the B+Tree (or other index type) starting from a specific key
// and traversing in a specific direction (Forward or Backward).
type IndexScanExecutor struct {
	plan      *planner.IndexScanNode
	index     indexing.Index
	tableHeap *TableHeap
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
	}

	e.err = nil
	e.current = storage.Tuple{}
	e.buffer = make([]byte, e.tableHeap.StorageSchema().BytesPerTuple())

	iterator, err := e.index.Scan(
		e.plan.StartKey,
		e.plan.Direction,
		ctx.GetTransaction(),
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

	if !e.iterator.Next() {
		e.err = e.iterator.Error()
		return false
	}

	rid := e.iterator.Value()
	if err := e.tableHeap.ReadTuple(
		nil, // Replace with the transaction from Init; see note below.
		rid,
		e.buffer,
		e.plan.ForUpdate,
	); err != nil {
		e.err = err
		return false
	}

	e.current = storage.FromRawTuple(
		e.buffer,
		e.tableHeap.StorageSchema(),
		rid,
	)
	return true
}

func (e *IndexScanExecutor) Current() storage.Tuple {
	return e.current
}

func (e *IndexScanExecutor) Close() error {
	if e.iterator == nil {
		return nil
	}
	return e.iterator.Close()
}

func (e *IndexScanExecutor) Error() error {
	return e.err
}
