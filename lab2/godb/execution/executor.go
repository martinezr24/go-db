package execution

import (
	"mit.edu/dsg/godb/planner"
	"mit.edu/dsg/godb/storage"
)

// Executor is the interface that all physical execution nodes must implement.
type Executor interface {
	// PlanNode returns the logical plan node that this executor implements.
	// This node contains the schema and configuration (e.g., predicates, projection lists)
	// required for execution.
	PlanNode() planner.PlanNode

	// Init initializes the executor and its children.
	// This is called once at the start of execution, or multiple times if the
	// executor is being re-executed (e.g., as the inner child of a Nested Loop Join).
	Init(ctx *ExecutorContext) error

	// Next advances the executor to the next tuple.
	// It returns true if a tuple was found and is ready to be retrieved via Current().
	// It returns false if the executor is exhausted or an error occurred.
	// In the case of false, check Error() to distinguish between end-of-iteration and failure.
	Next() bool

	// Current returns the tuple at the current cursor position.
	// This must only be called after a successful call to Next() (i.e., Next() returned true).
	// The returned tuple is valid only until the next call to Next() or Close().
	Current() storage.Tuple

	// Error returns the last error encountered by the executor, if any.
	// This should be checked if Next() returns false.
	Error() error

	// Close cleans up any resources (e.g., buffer pool pins, open files) held by the executor.
	Close() error
}
