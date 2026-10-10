package execution

import (
	"mit.edu/dsg/godb/transaction"
)

// ExecutorContext holds all the state and resources required for query execution.
// It is passed to every Executor during construction.
type ExecutorContext struct {
	txn *transaction.TransactionContext
	// A production system would have many more fields here
}

// NewExecutorContext creates a new execution context.
// This is typically called by the ConnectionManager or the main query execution loop.
func NewExecutorContext(txn *transaction.TransactionContext) *ExecutorContext {
	return &ExecutorContext{
		txn: txn,
	}
}

// GetTransaction returns the active transaction for this query.
// Executors must use this transaction when accessing the BufferPool or locking items if present.
func (ctx *ExecutorContext) GetTransaction() *transaction.TransactionContext {
	return ctx.txn
}
