package execution

import (
	"mit.edu/dsg/godb/planner"
	"mit.edu/dsg/godb/storage"
)

// SeqScanExecutor implements a sequential scan over a table.
type SeqScanExecutor struct {
	// Fill me in!
}

// NewSeqScanExecutor creates a new SeqScanExecutor.
func NewSeqScanExecutor(plan *planner.SeqScanNode, tableHeap *TableHeap) *SeqScanExecutor {
	panic("unimplemented")
}

func (e *SeqScanExecutor) PlanNode() planner.PlanNode {
	panic("unimplemented")
}

func (e *SeqScanExecutor) Init(context *ExecutorContext) error {
	panic("unimplemented")
}

func (e *SeqScanExecutor) Next() bool {
	panic("unimplemented")
}

func (e *SeqScanExecutor) Current() storage.Tuple {
	panic("unimplemented")
}

func (e *SeqScanExecutor) Error() error {
	panic("unimplemented")
}

func (e *SeqScanExecutor) Close() error {
	panic("unimplemented")
}
