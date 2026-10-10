package execution

import (
	"fmt"

	"mit.edu/dsg/godb/catalog"
	"mit.edu/dsg/godb/common"
	"mit.edu/dsg/godb/storage"
	"mit.edu/dsg/godb/transaction"
)

// TableManager manages the lifecycle of TableHeap objects.
// Since GoDB does not support runtime schema changes (no CREATE TABLE during transactions),
type TableManager struct {
	tables map[common.ObjectID]*TableHeap
}

// NewTableManager initializes the TableManager and eagerly creates TableHeap instances
// for all tables defined in the Catalog.
func NewTableManager(catalog *catalog.Catalog, bufferPool *storage.BufferPool, logManager storage.LogManager, lockManager *transaction.LockManager) (*TableManager, error) {
	tm := &TableManager{
		tables: make(map[common.ObjectID]*TableHeap),
	}

	// Eagerly load all tables to avoid synchronization overhead at runtime
	for _, tableDef := range catalog.Tables {
		heap, err := NewTableHeap(tableDef, bufferPool, logManager, lockManager)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize table '%s': %w", tableDef.Name, err)
		}
		tm.tables[tableDef.Oid] = heap
	}

	return tm, nil
}

// GetTable retrieves the TableHeap for a given table oid.
func (tm *TableManager) GetTable(oid common.ObjectID) (*TableHeap, error) {
	if heap, exists := tm.tables[oid]; exists {
		return heap, nil
	}
	return nil, fmt.Errorf("object '%d' not found", oid)
}
