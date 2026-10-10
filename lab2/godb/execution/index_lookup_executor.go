package execution

import (
	"mit.edu/dsg/godb/indexing"
	"mit.edu/dsg/godb/planner"
	"mit.edu/dsg/godb/storage"
)

// IndexLookupExecutor implements a Point Lookup using an index. Unlike a full Index Scan, which iterates over a
// range of keys, this executor efficiently retrieves only the tuples that match a specific equality key
// (e.g., "SELECT * FROM users WHERE id = 5").
type IndexLookupExecutor struct {
	// Fill me in!
}

func NewIndexLookupExecutor(plan *planner.IndexLookupNode, index indexing.Index, tableHeap *TableHeap) *IndexLookupExecutor {
	panic("unimplemented")
}

func (e *IndexLookupExecutor) PlanNode() planner.PlanNode {
	panic("unimplemented")
}

func (e *IndexLookupExecutor) Init(ctx *ExecutorContext) error {
	panic("unimplemented")
}

func (e *IndexLookupExecutor) Next() bool {
	panic("unimplemented")
}

func (e *IndexLookupExecutor) Current() storage.Tuple {
	panic("unimplemented")
}

func (e *IndexLookupExecutor) Close() error {
	panic("unimplemented")
}

func (e *IndexLookupExecutor) Error() error {
	panic("unimplemented")
}
