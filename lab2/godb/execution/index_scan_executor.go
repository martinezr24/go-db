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
	// Fill me in!
}

func NewIndexScanExecutor(plan *planner.IndexScanNode, index indexing.Index, tableHeap *TableHeap) *IndexScanExecutor {
	panic("unimplemented")
}

func (e *IndexScanExecutor) PlanNode() planner.PlanNode {
	panic("unimplemented")
}

func (e *IndexScanExecutor) Init(ctx *ExecutorContext) error {
	panic("unimplemented")
}

func (e *IndexScanExecutor) Next() bool {
	panic("unimplemented")
}

func (e *IndexScanExecutor) Current() storage.Tuple {
	panic("unimplemented")
}

func (e *IndexScanExecutor) Close() error {
	panic("unimplemented")
}

func (e *IndexScanExecutor) Error() error {
	panic("unimplemented")
}
