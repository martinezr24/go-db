package indexing

import (
	"fmt"

	"mit.edu/dsg/godb/catalog"
	"mit.edu/dsg/godb/common"
	"mit.edu/dsg/godb/storage"
)

// IndexManager manages the runtime lifecycle of index structures.
// Since GoDB schemas are static, we pre-load all indexes at startup.
type IndexManager struct {
	// runtimeIndexes maps IndexName -> Actual Index Implementation
	runtimeIndexes map[common.ObjectID]Index
}

// NewIndexManager initializes the IndexManager by creating empty runtime index
// structures for every index defined in the Catalog.
func NewIndexManager(c *catalog.Catalog) (*IndexManager, error) {
	im := &IndexManager{
		runtimeIndexes: make(map[common.ObjectID]Index),
	}

	for _, table := range c.Tables {
		for _, i := range table.Indexes {
			nameMap := make(map[string]int)
			for i, col := range table.Columns {
				nameMap[col.Name] = i
			}

			indices := make([]int, len(i.KeySchema))
			for i, name := range i.KeySchema {
				idx, ok := nameMap[name]
				if !ok {
					return nil, fmt.Errorf("column '%s' in index definition does not exist in table '%s'", name, table.Name)
				}
				indices[i] = idx
			}

			keyFields := make([]common.Type, len(indices))
			for i, colIdx := range indices {
				keyFields[i] = table.Columns[colIdx].Type
			}
			keySchema := storage.NewRawTupleDesc(keyFields)

			var idx Index
			switch i.Type {
			case "hash":
				idx = NewMemHashIndex(keySchema, indices)
			case "btree":
				idx = NewMemBTreeIndex(keySchema, indices)
			default:
				return nil, fmt.Errorf("unsupported index type '%s' for index '%s'", i.Type, i.Name)
			}

			im.runtimeIndexes[i.Oid] = idx
		}
	}

	return im, nil
}

// GetIndex retrieves an active index by its oid.
func (im *IndexManager) GetIndex(oid common.ObjectID) (Index, error) {
	if idx, exists := im.runtimeIndexes[oid]; exists {
		return idx, nil
	}
	return nil, fmt.Errorf("object '%d' not found", oid)
}
