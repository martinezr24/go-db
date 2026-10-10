package execution

import (
	"mit.edu/dsg/godb/planner"
	"mit.edu/dsg/godb/storage"
)

// SeqScanExecutor implements a sequential scan over a table.
type SeqScanExecutor struct {
	plan      *planner.SeqScanNode
	tableHeap *TableHeap
	buffer    []byte
	current   storage.Tuple
	err       error
	iterator  TableHeapIterator
}

// NewSeqScanExecutor creates a new SeqScanExecutor.
func NewSeqScanExecutor(plan *planner.SeqScanNode, tableHeap *TableHeap) *SeqScanExecutor {
	return &SeqScanExecutor{
		plan:      plan,
		tableHeap: tableHeap,
	}
}

func (e *SeqScanExecutor) PlanNode() planner.PlanNode {
	return e.plan
}

func (e *SeqScanExecutor) Init(context *ExecutorContext) error {
	e.err = nil
	e.current = storage.Tuple{}
	e.buffer = make([]byte, e.tableHeap.StorageSchema().BytesPerTuple())

	iterator, err := e.tableHeap.Iterator(
		context.GetTransaction(),
		e.plan.Mode,
		e.buffer,
	)
	if err != nil {
		e.err = err
		return err
	}

	e.iterator = iterator
	return nil
}

func (e *SeqScanExecutor) Next() bool {
	if e.err != nil {
		return false
	}

	if !e.iterator.Next() {
		e.err = e.iterator.Error()
		return false
	}

	e.current = storage.FromRawTuple(
		e.iterator.CurrentTuple(),
		e.tableHeap.StorageSchema(),
		e.iterator.CurrentRID(),
	)
	return true
}

func (e *SeqScanExecutor) Current() storage.Tuple {
	return e.current
}

func (e *SeqScanExecutor) Error() error {
	return e.err
}

func (e *SeqScanExecutor) Close() error {
	if e.iterator.IsNil() {
		return nil
	}
	return e.iterator.Close()
}
