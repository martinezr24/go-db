package catalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"mit.edu/dsg/godb/common"
)

// Catalog manages the database schema and provides fast lookups.
// For simplicity, the catalog is serialized as a single JSON blob. In a production DBMS, the
// catalog is usually stored as a set of standard database tables (e.g., 'pg_class' or
// 'pg_attribute' in Postgres) that enjoy the same ACID guarantees as user tables.
//
// To read a user table, the DBMS kernel first performs a hidden query on the
// catalog tables using its own internal Buffer Pool and Storage Engine. This
// creates a recursive dependency usually solved by "hard-coding" the
// physical locations of the core catalog files on disk.
//
// IMMUTABILITY & SCHEMA EVOLUTION:
// For simplicity, GoDB treats the catalog as immutable during runtime. If you
// need a new schema, you restart with a new catalog file. To start with a new catalog, restart the system with
// a new catalog file path or delete the persistent catalog file.
//
// In a real-world system, schema changes (ALTER TABLE) are massive, transactional
// operations. Because multiple queries might be running simultaneously, a real
// catalog must ensure that an active query doesn't see a column disappear while it is still reading
// data.
type Catalog struct {
	catalogState

	// In-memory structures for fast lookups
	tableMap  map[string]*Table   // TableName -> Table
	columnMap map[string][]*Table // ColumnName -> List of Tables containing this column

	persistenceProvider PersistenceProvider
}

// Column represents the basic unit of a table schema.
type Column struct {
	Name string      `json:"name"`
	Type common.Type `json:"type"`
}

// Index describes a physical access path used to speed up queries.
// It maps a set of columns (KeySchema) to an ObjectID for the index file.
type Index struct {
	Oid       common.ObjectID `json:"oid"`
	TableOid  common.ObjectID `json:"table_oid"`
	Name      string          `json:"name"`
	Type      string          `json:"type"`       // "hash" or "btree"
	KeySchema []string        `json:"key_schema"` // List of column names
}

// Table is the primary metadata structure. It groups columns and their
// associated indexes under a unique ObjectID.
type Table struct {
	Oid     common.ObjectID `json:"oid"`
	Name    string          `json:"name"`
	Columns []Column        `json:"columns"`
	Indexes []Index         `json:"indexes"`
}

// PersistenceProvider abstracts how the catalog is saved to and loaded from disk.
type PersistenceProvider interface {
	LoadCatalogState() (json string, err error)
	SaveCatalogState(json string) error
}

func (t *Table) String() string {
	b, _ := json.MarshalIndent(t, "", "  ")
	return string(b)
}

type catalogState struct {
	NextId uint32   `json:"next_id"`
	Tables []*Table `json:"tables"`
}

func (c *Catalog) String() string {
	b, _ := json.MarshalIndent(c, "", "  ")
	return string(b)
}

func (c *Catalog) toJSON() (string, error) {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (c *Catalog) fromJSON(jsonData string) error {
	if err := json.Unmarshal([]byte(jsonData), c); err != nil {
		return err
	}
	for _, t := range c.Tables {
		c.tableMap[t.Name] = t
		for _, f := range t.Columns {
			c.columnMap[f.Name] = append(c.columnMap[f.Name], t)
		}
	}
	return nil
}

// NewCatalog initializes a catalog. It attempts to load existing state
// from the provider; if no state exists, it starts with an empty database.
func NewCatalog(provider PersistenceProvider) (*Catalog, error) {
	result := &Catalog{
		catalogState: catalogState{
			NextId: 0,
			Tables: make([]*Table, 0),
		},
		tableMap:            make(map[string]*Table),
		columnMap:           make(map[string][]*Table),
		persistenceProvider: provider,
	}

	jsonData, err := provider.LoadCatalogState()
	if errors.Is(err, os.ErrNotExist) {
		// Start from scratch
		return result, nil
	}
	if err != nil {
		return nil, err
	}

	if err = result.fromJSON(jsonData); err != nil {
		// Parsing errors are fatal system errors, usually indicating corruption
		return nil, fmt.Errorf("failed to parse catalog state: %v", err)
	}

	return result, nil
}

// AddTable registers a new table in the catalog.
// It assigns a globally unique ObjectID to the table and persists the updated state. If the table with that name
// already exists, it returns DuplicateObjectError.
func (c *Catalog) AddTable(tableName string, columns []Column) (*Table, error) {
	if _, exists := c.tableMap[tableName]; exists {
		return nil, common.GoDBError{
			Code:      common.DuplicateObjectError,
			ErrString: fmt.Sprintf("table '%s' already exists", tableName),
		}
	}

	// oid 0 is reserved for INVALID
	c.NextId++

	t := &Table{
		Oid:     common.ObjectID(c.NextId),
		Name:    tableName,
		Columns: columns,
		Indexes: make([]Index, 0),
	}

	c.Tables = append(c.Tables, t)
	c.tableMap[tableName] = t
	for _, f := range columns {
		c.columnMap[f.Name] = append(c.columnMap[f.Name], t)
	}

	jsonData, err := c.toJSON()
	if err != nil {
		return nil, err
	}
	return t, c.persistenceProvider.SaveCatalogState(jsonData)
}

// GetTableMetadata fetches the schema for a specific table name.
func (c *Catalog) GetTableMetadata(tableName string) (*Table, error) {
	table, exists := c.tableMap[tableName]
	if !exists {
		return nil, common.GoDBError{
			Code:      common.NoSuchObjectError,
			ErrString: fmt.Sprintf("table '%s' does not exist", tableName),
		}
	}
	return table, nil
}

func (c *Catalog) GetTableByOid(tableOid common.ObjectID) (*Table, error) {
	for _, table := range c.Tables {
		if table.Oid == tableOid {
			return table, nil
		}
	}
	return nil, common.GoDBError{
		Code:      common.NoSuchObjectError,
		ErrString: fmt.Sprintf("table with OID '%d' does not exist", tableOid),
	}
}

func (c *Catalog) GetIndexByOid(indexOid common.ObjectID) (*Index, error) {
	for _, table := range c.Tables {
		for _, index := range table.Indexes {
			if index.Oid == indexOid {
				return &index, nil
			}
		}
	}
	return nil, common.GoDBError{
		Code:      common.NoSuchObjectError,
		ErrString: fmt.Sprintf("index with OID '%d' does not exist", indexOid),
	}
}

// FindTablesWithColumnName returns all tables that contain a column with
// the given name. Used by the Query Optimizer to resolve identifiers.
func (c *Catalog) FindTablesWithColumnName(columnName string) []*Table {
	return c.columnMap[columnName]
}

// AddIndex attaches a new index definition to a table. If an index with that name
// // already exists, it returns DuplicateObjectError.
func (c *Catalog) AddIndex(indexName string, tableName string, indexType string, columnNames []string) (*Index, error) {
	table, err := c.GetTableMetadata(tableName)
	if err != nil {
		return nil, err
	}

	// Check for duplicate indexing name on this table
	for _, idx := range table.Indexes {
		if idx.Name == indexName {
			return nil, common.GoDBError{
				Code:      common.DuplicateObjectError,
				ErrString: fmt.Sprintf("indexing '%s' already exists on table '%s'", indexName, tableName),
			}
		}
	}

	// Validate columns exist
	tableCols := make(map[string]bool)
	for _, col := range table.Columns {
		tableCols[col.Name] = true
	}
	for _, colName := range columnNames {
		if !tableCols[colName] {
			return nil, common.GoDBError{
				Code:      common.NoSuchObjectError,
				ErrString: fmt.Sprintf("column '%s' does not exist in table '%s'", colName, tableName),
			}
		}
	}

	c.NextId++
	idx := Index{
		Oid:       common.ObjectID(c.NextId),
		TableOid:  table.Oid,
		Name:      indexName,
		Type:      indexType,
		KeySchema: columnNames,
	}

	table.Indexes = append(table.Indexes, idx)

	jsonData, err := c.toJSON()
	if err != nil {
		return nil, err
	}
	return &idx, c.persistenceProvider.SaveCatalogState(jsonData)
}

type NullPersistenceProvider struct{}

func (NullPersistenceProvider) LoadCatalogState() (string, error) {
	return "", os.ErrNotExist
}

func (NullPersistenceProvider) SaveCatalogState(json string) error {
	return nil
}

type DiskCatalogManager struct {
	path string
}

func NewDiskCatalogManager(path string) *DiskCatalogManager {
	return &DiskCatalogManager{
		path: path,
	}
}

// LoadCatalogState implements the catalog.PersistenceProvider interface.
func (dcm *DiskCatalogManager) LoadCatalogState() (string, error) {
	content, err := os.ReadFile(dcm.path)
	if err != nil {
		return "", err // Let the caller (Catalog) handle os.ErrNotExist
	}
	return string(content), nil
}

// SaveCatalogState implements the catalog.PersistenceProvider interface.
func (dcm *DiskCatalogManager) SaveCatalogState(jsonData string) error {
	// perform an atomic WriteToFullTuple using a temporary file.
	tmpPath := filepath.Join(dcm.path, ".tmp")
	if err := os.WriteFile(tmpPath, []byte(jsonData), 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, dcm.path)
}
