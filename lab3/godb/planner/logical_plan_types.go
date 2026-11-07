package planner

import (
	"fmt"
	"strings"

	"mit.edu/dsg/godb/catalog"
	"mit.edu/dsg/godb/common"
	"mit.edu/dsg/godb/storage"
)

/*
Only a LogicalScanNode or a LogicalSubqueryNode maintains a TableRef.
alias is either
 1. a table name
 2. a manually set alias
 3. "", where the schema contains logical columns referring to lower level
    TableRefs and this TableRef is a pass-through reference. This is only possible
    through
    (1) query rewriting since xbw1989/parser's dialect requires derived tables
    to be aliased.
    (2) aggregation node
*/
type TableRef struct {
	schema LogicalSchema
	table  *catalog.Table // only non-nil for LogicalScanNode
	alias  string

	refID uint64
}

func (t *TableRef) Equals(other *TableRef) bool {
	if t == other {
		return true
	}
	if t == nil || other == nil {
		return false
	}
	if t.refID == other.refID {
		return true
	}
	if (t.refID == 0 || other.refID == 0) && t.alias == other.alias {
		// One of t/other is not attached to the logical operator tree.
		return true
	}
	return false
}

type FromScope struct {
	sourceTables []*TableRef // each table reference in FROM
}

// LogicalColumn implements Expr except for Eval() which is a physical function.
// It represents a reference to a column produced by a child node.
type LogicalColumn struct {
	cname  string      // column name or alias
	ctype  common.Type // physical type info
	origin *TableRef   // Table/subquery where this column comes from, immutable
}

func (c *LogicalColumn) Equals(other *LogicalColumn) bool {
	if c == other {
		return true
	}
	if c == nil || other == nil {
		return false
	}

	// TODO: Handle type casting
	if !strings.EqualFold(c.cname, other.cname) || c.ctype != other.ctype {
		return false
	}

	return c.origin.Equals(other.origin)
}

type LogicalSchema []*LogicalColumn

// Contains checks if the target column exists in this schema.
// It uses pointer equality to verify identity.
func (s LogicalSchema) Contains(target *LogicalColumn) bool {
	for _, col := range s {
		if col.Equals(target) {
			return true
		}
	}
	return false
}

// Equals checks if two schemas contain the same set of columns, regardless of order.
func (s LogicalSchema) Equals(other LogicalSchema) bool {
	if len(s) != len(other) {
		return false
	}
	for i := range s {
		if !other.Contains(s[i]) {
			return false
		}
	}
	for j := range other {
		if !s.Contains(other[j]) {
			return false
		}
	}
	return true
}

func (s LogicalSchema) Covers(subset LogicalSchema) bool {
	for _, col := range subset {
		if !s.Contains(col) {
			return false
		}
	}
	return true
}

// CoversExpr checks if 'expr' can be evaluated using ONLY this schema.
// Used by predicate pushdown to verify if a filter is valid at this node.
func (s LogicalSchema) CoversExpr(expr Expr) bool {
	cols := expr.GetReferencedColumns()
	for _, col := range cols {
		if !s.Contains(col) {
			return false
		}
	}
	return true
}

func (s LogicalSchema) GetExprs() []Expr {
	exprs := make([]Expr, len(s))
	for i, col := range s {
		exprs[i] = col
	}
	return exprs
}

// --- Expr Interface Implementation ---

// OutputType tells the optimizer what kind of data this column holds.
func (c *LogicalColumn) OutputType() common.Type {
	return c.ctype
}

// Eval panics because LogicalColumns are placeholders.
// They must be converted to BoundValueExpr (physical) before execution.
func (c *LogicalColumn) Eval(t storage.Tuple) common.Value {
	panic("Error: Attempted to Eval() a LogicalColumn. " +
		"This node should have been replaced by BoundValueExpr in the Physical Plan.")
}

// String provides a clean representation (e.g., "users.id")
func (c *LogicalColumn) String() string {
	if c.origin != nil && c.origin.alias != "" {
		return fmt.Sprintf("%s.%s", c.origin.alias, c.cname)
	}
	return c.cname
}

// LogicalColumn is the leaf of the expression tree.
// It simply returns a list containing itself.
func (c *LogicalColumn) GetReferencedColumns() []*LogicalColumn {
	return []*LogicalColumn{c}
}
