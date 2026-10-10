package transaction

import (
	"mit.edu/dsg/godb/storage"
)

type TransactionManager struct{}

func NewTransactionManager(logManager storage.LogManager, bufferPool *storage.BufferPool, lockManager *LockManager) *TransactionManager {
	return &TransactionManager{}
}
