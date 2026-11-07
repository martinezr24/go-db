package planner

import (
	"fmt"
	"strings"
	"testing"

	"github.com/xwb1989/sqlparser"
	"mit.edu/dsg/godb/catalog"
	"mit.edu/dsg/godb/common"
)

// getBasicTestCatalog returns a catalog with 'users' and 'orders' tables
func getBasicTestCatalog() *catalog.Catalog {
	result, _ := catalog.NewCatalog(catalog.NullPersistenceProvider{})
	_, _ = result.AddTable("users", []catalog.Column{
		{Name: "id", Type: common.IntType},
		{Name: "name", Type: common.StringType},
		{Name: "age", Type: common.IntType},
		{Name: "country", Type: common.StringType},
		{Name: "year", Type: common.IntType},
	})

	_, _ = result.AddTable("orders", []catalog.Column{
		{Name: "id", Type: common.IntType},
		{Name: "oid", Type: common.IntType},
		{Name: "user_id", Type: common.IntType},
		{Name: "amount", Type: common.IntType},
		{Name: "country", Type: common.StringType},
		{Name: "year", Type: common.IntType},
	})

	_, _ = result.AddTable("users_new", []catalog.Column{
		{Name: "id", Type: common.IntType},
		{Name: "name", Type: common.StringType},
		{Name: "age", Type: common.IntType},
		{Name: "country", Type: common.StringType},
		{Name: "year", Type: common.IntType},
	})
	return result
}

func printPlanTree(t *testing.T, node LogicalPlanNode, depth int) {
	if node == nil {
		return
	}
	indent := strings.Repeat("  ", depth)
	t.Logf("%s-> %s\n", indent, node.String())
	for _, child := range node.Children() {
		printPlanTree(t, child, depth+1)
	}
}

// ******************** Testing bindExpr ***********************

func extractSelect(t *testing.T, sqlStr string) *sqlparser.Select {
	stmt, err := sqlparser.Parse(sqlStr)
	if err != nil {
		t.Fatalf("Failed to parse SQL '%s': %v", sqlStr, err)
	}
	sel, ok := stmt.(*sqlparser.Select)
	if !ok {
		t.Fatalf("Expected SELECT statement")
	}
	return sel
}

// parseExpr is a helper that wraps a raw SQL expression string in a
// dummy SELECT statement to parse it into an AST node.
func parseExpr(t *testing.T, exprSql string) sqlparser.Expr {
	// 1. Wrap the expression in a SELECT so the parser accepts it
	sql := fmt.Sprintf("SELECT %s FROM dummy", exprSql)

	sel := extractSelect(t, sql)

	if len(sel.SelectExprs) == 0 {
		t.Fatalf("No expression found in parsed SQL")
	}

	aliased, ok := sel.SelectExprs[0].(*sqlparser.AliasedExpr)
	if !ok {
		t.Fatalf("Expected AliasedExpr, got %T", sel.SelectExprs[0])
	}

	return aliased.Expr
}

func getScopeFromSQL(t *testing.T, builder *LogicalPlanBuilder, sqlStr string) *FromScope {
	sel := extractSelect(t, sqlStr)

	if len(sel.From) == 0 {
		t.Fatal("FROM clause is empty")
	}

	scope, _, err := builder.planFrom(sel.From)
	if err != nil {
		t.Fatalf("Failed to plan FROM clause: %v", err)
	}
	return scope
}

func TestBuildLogicalExpr(t *testing.T) {
	cat := getBasicTestCatalog()
	builder := NewLogicalPlanBuilder(cat)

	scope := getScopeFromSQL(t, builder, "SELECT * FROM users AS u, orders, (SELECT MAX(users.id) from users) AS agg")

	tests := []struct {
		name        string
		exprSQL     string
		expectError bool
		validate    func(expr Expr, t *testing.T)
	}{
		{
			name:    "Constant Integer",
			exprSQL: "SELECT * FROM u WHERE 42",
			validate: func(e Expr, t *testing.T) {
				if e.String() != "42" {
					t.Errorf("Expected '42', got '%s'", e.String())
				}
				if e.OutputType() != common.IntType {
					t.Errorf("Expected IntType, got '%v'", e.OutputType())
				}
			},
		},
		{
			name:    "Constant Expression",
			exprSQL: "SELECT * FROM u WHERE 1-2",
			validate: func(e Expr, t *testing.T) {
				exp, ok := e.(*ArithmeticExpression)
				if !ok || exp.op != Sub {
					t.Fatalf("Expected ArithmeticExpression with op Sub, got %s", exp.String())
				}
				cols := e.GetReferencedColumns()
				if len(cols) != 0 {
					t.Errorf("Expected empty column reference.")
				}
			},
		},
		{
			name:    "Qualified Column (Match Alias)",
			exprSQL: "SELECT * FROM u WHERE u.age",
			validate: func(e Expr, t *testing.T) {
				col, ok := e.(*LogicalColumn)
				if !ok {
					t.Fatalf("Expected LogicalColumn")
				}
				if col.cname != "age" {
					t.Errorf("Expected column 'age', got '%s'", col.cname)
				}
				if col.origin.alias != "u" {
					t.Errorf("Expected origin alias 'u', got '%s'", col.origin.alias)
				}
				if col.OutputType() != common.IntType {
					t.Errorf("Expected column type IntType, got '%v", col.OutputType())
				}
			},
		},
		{
			name:    "Unqualified Column (Auto-Resolve)",
			exprSQL: "SELECT * FROM u WHERE name",
			validate: func(e Expr, t *testing.T) {
				col, ok := e.(*LogicalColumn)
				if !ok {
					t.Fatalf("Expected LogicalColumn")
				}
				if col.cname != "name" {
					t.Errorf("Expected column 'name', got '%s'", col.cname)
				}
				if col.origin.alias != "u" {
					t.Errorf("Expected origin alias 'u', got '%s'", col.origin.alias)
				}
			},
		},
		{
			name:    "Complex Expression (Math + Column)",
			exprSQL: "SELECT * FROM u WHERE u.age + 1",
			validate: func(e Expr, t *testing.T) {
				exp, ok := e.(*ArithmeticExpression)
				if !ok || exp.op != Add {
					t.Fatalf("Expected ArithmeticExpression with op Add, got %s", exp.String())
				}

				cols := e.GetReferencedColumns()
				if len(cols) != 1 || cols[0].cname != "age" {
					t.Errorf("Expected reference to 'age'")
				}
			},
		},
		{
			name:    "Comparison (Predicate)",
			exprSQL: "SELECT * FROM u WHERE age > 21",
			validate: func(e Expr, t *testing.T) {
				exp, ok := e.(*ComparisonExpression)
				if !ok || exp.compType != GreaterThan {
					t.Fatalf("Expected ComparisonExpression with compType >, got %s", exp.String())
				}
			},
		},
		{
			name:    "Aggregation Func Expr",
			exprSQL: "SELECT * FROM u WHERE MAX(users.id)",
			validate: func(e Expr, t *testing.T) {
				col, ok := e.(*LogicalColumn)
				if !ok || col.cname != "MAX(users.id)" || col.origin.alias != "agg" {
					t.Fatalf("Expected ComparisonExpression with compType >, got %s", col.String())
				}
			},
		},
		{
			name:        "Nonexistent aggregation",
			exprSQL:     "SELECT * FROM u WHERE COUNT(*)",
			expectError: true,
		},
		{
			name:        "Error: Unknown Column",
			exprSQL:     "SELECT * FROM u WHERE banana",
			expectError: true,
		},
		{
			name:        "Error: Invalid Alias",
			exprSQL:     "SELECT * FROM u WHERE users.id",
			expectError: true,
		},
		{
			name:        "Error: Nonexistent Table",
			exprSQL:     "SELECT * FROM u where faketable.oid",
			expectError: true,
		},
		{
			name:        "Error: Ambiguous Column",
			exprSQL:     "SELECT * FROM u WHERE id",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stmt, err := sqlparser.Parse(tt.exprSQL)
			if err != nil {
				t.Fatalf("Failed to parse SQL '%s': %v", tt.exprSQL, err)
			}
			sel, ok := stmt.(*sqlparser.Select)
			if !ok {
				t.Fatalf("Expected SELECT statement, got %T", stmt)
			}

			res, err := builder.bindExpr(sel.Where.Expr, scope)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error, got nil")
				}
			} else {
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}
				if tt.validate != nil {
					tt.validate(res, t)
				}
			}
		})
	}
}

func TestPlanOrder(t *testing.T) {
	cat := getBasicTestCatalog()
	builder := NewLogicalPlanBuilder(cat)

	setupSQL := "SELECT * FROM users AS u"
	scope := getScopeFromSQL(t, builder, setupSQL)

	dummyChild := NewLogicalScanNode(scope.sourceTables[0], false)

	tests := []struct {
		name        string
		sql         string
		expectError bool
		validate    func(node LogicalPlanNode, t *testing.T)
	}{
		{
			name: "No Order By (Optimization)",
			sql:  "SELECT * FROM users",
			validate: func(node LogicalPlanNode, t *testing.T) {
				if _, ok := node.(*LogicalSortNode); ok {
					t.Error("Expected no SortNode for query without ORDER BY")
				}
				if node != dummyChild {
					t.Error("Expected planOrder to return child unmodified")
				}
			},
		},
		{
			name: "Single Column (Default ASC)",
			sql:  "SELECT * FROM users ORDER BY age",
			validate: func(node LogicalPlanNode, t *testing.T) {
				sortNode, ok := node.(*LogicalSortNode)
				if !ok {
					t.Fatalf("Expected LogicalSortNode")
				}

				if len(sortNode.OrderBy) != 1 {
					t.Fatalf("Expected 1 sort key")
				}

				clause := sortNode.OrderBy[0]
				if clause.Expr.(*LogicalColumn).cname != "age" {
					t.Errorf("Expected sort on 'age'")
				}
				if clause.Direction != SortOrderAscending {
					t.Errorf("Expected ASC by default")
				}
			},
		},
		{
			name: "Single Column Explicit DESC",
			sql:  "SELECT * FROM users ORDER BY age DESC",
			validate: func(node LogicalPlanNode, t *testing.T) {
				sortNode, ok := node.(*LogicalSortNode)
				if !ok {
					t.Fatalf("Expected LogicalSortNode")
				}

				if sortNode.OrderBy[0].Direction != SortOrderDescending {
					t.Errorf("Expected DESC sort order")
				}
			},
		},
		{
			name: "Multi-Column Sort",
			sql:  "SELECT * FROM users ORDER BY year DESC, name ASC",
			validate: func(node LogicalPlanNode, t *testing.T) {
				sortNode, ok := node.(*LogicalSortNode)
				if !ok {
					t.Fatalf("Expected LogicalSortNode")
				}

				if len(sortNode.OrderBy) != 2 {
					t.Fatalf("Expected 2 sort keys")
				}

				c1 := sortNode.OrderBy[0]
				if c1.Expr.(*LogicalColumn).cname != "year" || c1.Direction != SortOrderDescending {
					t.Error("First sort key should be year DESC")
				}

				c2 := sortNode.OrderBy[1]
				if c2.Expr.(*LogicalColumn).cname != "name" || c2.Direction != SortOrderAscending {
					t.Error("Second sort key should be name ASC")
				}
			},
		},
		{
			name: "Expression Sort",
			sql:  "SELECT * FROM users ORDER BY age + 1",
			validate: func(node LogicalPlanNode, t *testing.T) {
				sortNode, ok := node.(*LogicalSortNode)
				if !ok {
					t.Fatalf("Expected LogicalSortNode")
				}
				if len(sortNode.OrderBy) != 1 {
					t.Error("Expected 1 sort key")
				}
			},
		},
		{
			name:        "Unknown Column Error",
			sql:         "SELECT * FROM users ORDER BY banana",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sel := extractSelect(t, tt.sql)

			resultNode, err := builder.planOrder(sel.OrderBy, dummyChild, scope)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
			} else {
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}
				if tt.validate != nil {
					tt.validate(resultNode, t)
				}
			}
		})
	}
}

// Helper to parse SQL and extract the first FROM clause element
func extractTableExpr(t *testing.T, sqlStr string) *sqlparser.AliasedTableExpr {
	sel := extractSelect(t, sqlStr)

	if len(sel.From) == 0 {
		t.Fatal("FROM clause is empty")
	}

	// We assume the test SQL uses standard joins/tables, so the first element
	// is an AliasedTableExpr.
	expr, ok := sel.From[0].(*sqlparser.AliasedTableExpr)
	if !ok {
		t.Fatalf("Expected AliasedTableExpr in FROM, got %T", sel.From[0])
	}
	return expr
}

func TestPlanAliasedTable(t *testing.T) {
	cat := getBasicTestCatalog()

	tests := []struct {
		name        string
		sql         string
		expectError bool
		validate    func(scope *FromScope, node LogicalPlanNode, t *testing.T)
	}{
		{
			name: "Base Table Explicit Alias",
			sql:  "SELECT * FROM users AS u",
			validate: func(scope *FromScope, node LogicalPlanNode, t *testing.T) {
				if len(scope.sourceTables) != 1 {
					t.Fatalf("Expected 1 TableRef, got %d", len(scope.sourceTables))
				}
				ref := scope.sourceTables[0]

				if ref.alias != "u" {
					t.Errorf("Expected Alias 'u', got '%s'", ref.alias)
				}
				if ref.table == nil || ref.table.Name != "users" {
					t.Errorf("Expected Table metadata for 'users'")
				}

				if len(ref.schema) == 0 {
					t.Fatal("Schema should not be empty")
				}
				if ref.schema[0].origin != ref {
					t.Errorf("Column origin mismatch. Expected ref 'u', got %v", ref.schema[0].origin)
				}
				scanNode, ok := node.(*LogicalScanNode)
				if !ok {
					t.Fatalf("Expected LogicalScanNode, got %T", node)
				}
				if scanNode.TableRef != ref {
					t.Error("TableRefs should match")
				}
				if scanNode.GetTableAlias() != "u" {
					t.Errorf("Expected LogicalScanNode to reflect alias 'u', got '%s'", scanNode.GetTableAlias())
				}
				if scanNode.GetTableOid() != 1 {
					t.Errorf("Expected LogicalScanNode to reflect oid 1, got '%d'", scanNode.GetTableOid())
				}
			},
		},
		{
			name: "Base Table Default Alias",
			sql:  "SELECT * FROM users",
			validate: func(scope *FromScope, node LogicalPlanNode, t *testing.T) {
				ref := scope.sourceTables[0]
				if ref.alias != "users" {
					t.Errorf("Expected default alias 'users', got '%s'", ref.alias)
				}
				scanNode, ok := node.(*LogicalScanNode)
				if !ok {
					t.Fatalf("Expected LogicalScanNode, got %T", node)
				}
				if scanNode.TableRef != ref {
					t.Error("TableRefs should match")
				}
				if scanNode.GetTableAlias() != "users" {
					t.Errorf("Expected LogicalScanNode to reflect alias 'users', got '%s'", scanNode.GetTableAlias())
				}
				if scanNode.GetTableOid() != 1 {
					t.Errorf("Expected LogicalScanNode to reflect oid 1, got '%d'", scanNode.GetTableOid())
				}
			},
		},
		{
			name: "Aliased Subquery (Barrier)",
			sql:  "SELECT * FROM (SELECT id FROM users) AS sub",
			validate: func(scope *FromScope, node LogicalPlanNode, t *testing.T) {
				ref := scope.sourceTables[0]

				if ref.alias != "sub" {
					t.Errorf("Expected alias 'sub', got '%s'", ref.alias)
				}

				if ref.table != nil {
					t.Error("Subquery TableRef should have nil Table metadata")
				}

				col := ref.schema[0]
				if col.origin != ref {
					t.Errorf("Aliased subquery should re-bind origin. Expected 'sub', got %v", col.origin)
				}

				subNode, ok := node.(*LogicalSubqueryNode)
				if !ok {
					t.Fatalf("Expected LogicalSubqueryNode, got %T", node)
				}
				if subNode.TableRef != ref {
					t.Error("Node should point to the created TableRef")
				}
			},
		},
		{
			name:        "Non-Existent Table",
			sql:         "SELECT * FROM ghost_table",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewLogicalPlanBuilder(cat)

			aliasedTableExpr := extractTableExpr(t, tt.sql)

			scope, node, err := builder.planAliasedTable(aliasedTableExpr)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
			} else {
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}
				if tt.validate != nil {
					tt.validate(scope, node, t)
				}
			}
		})
	}
}

// Helper to extract specifically a JoinTableExpr from a SELECT statement
func extractJoinExpr(t *testing.T, sqlStr string) *sqlparser.JoinTableExpr {
	sel := extractSelect(t, sqlStr)

	if len(sel.From) == 0 {
		t.Fatal("FROM clause is empty")
	}

	// The parser represents "A JOIN B" as a single JoinTableExpr in the From list
	expr, ok := sel.From[0].(*sqlparser.JoinTableExpr)
	if !ok {
		t.Fatalf("Expected JoinTableExpr in FROM, got %T", sel.From[0])
	}
	return expr
}

func TestPlanExplicitJoin(t *testing.T) {
	cat := getBasicTestCatalog()

	tests := []struct {
		name        string
		sql         string
		expectError bool
		validate    func(scope *FromScope, node LogicalPlanNode, t *testing.T)
	}{
		{
			name: "Simple Inner Join (ON Clause)",
			sql:  "SELECT * FROM users JOIN orders ON users.id = orders.user_id",
			validate: func(scope *FromScope, node LogicalPlanNode, t *testing.T) {
				joinNode, ok := node.(*LogicalJoinNode)
				if !ok {
					t.Fatalf("Expected LogicalJoinNode, got %T", node)
				}

				if joinNode.joinType != Inner {
					t.Errorf("Expected Inner Join, got %v", joinNode.joinType)
				}

				if len(scope.sourceTables) != 2 {
					t.Errorf("Expected merged scope to have 2 tables, got %d", len(scope.sourceTables))
				}

				leftScan, ok := joinNode.Left.(*LogicalScanNode)
				rightScan, ok := joinNode.Right.(*LogicalScanNode)
				if !ok || (leftScan.GetTableAlias() != "users" && leftScan.GetTableAlias() != "orders") {
					t.Error("Left child should be scan of 'users' or 'orders'")
				}
				if !ok || (rightScan.GetTableAlias() != "orders" && rightScan.GetTableAlias() != "users") {
					t.Error("Right child should be scan of 'orders' or 'users'")
				}

				if len(joinNode.joinOn) != 1 {
					t.Errorf("Expected 1 join condition, got %d", len(joinNode.joinOn))
				}
			},
		},
		{
			name: "Left Join",
			sql:  "SELECT * FROM users LEFT JOIN orders ON users.id = orders.user_id",
			validate: func(scope *FromScope, node LogicalPlanNode, t *testing.T) {
				joinNode, ok := node.(*LogicalJoinNode)
				if !ok {
					t.Fatalf("Expected LogicalJoinNode, got %T", node)
				}
				if joinNode.joinType != Left {
					t.Errorf("Expected Left Join, got %v", joinNode.joinType)
				}
			},
		},
		{
			name: "Nested Join",
			sql:  "SELECT * FROM users JOIN orders ON users.id = orders.user_id JOIN users AS u2 ON orders.user_id = u2.id",
			validate: func(scope *FromScope, node LogicalPlanNode, t *testing.T) {
				rootJoin, ok := node.(*LogicalJoinNode)
				if !ok {
					t.Fatalf("Expected root to be LogicalJoinNode")
				}

				// Scope should have 3 tables: users, orders, u2
				if len(scope.sourceTables) != 3 {
					t.Errorf("Expected merged scope to have 3 tables, got %d", len(scope.sourceTables))
				}

				// The Left child of the root should be the first JOIN (users + orders) or a scan
				_, leftJoinOk := rootJoin.Left.(*LogicalJoinNode)
				_, leftScanOk := rootJoin.Left.(*LogicalScanNode)
				if !leftJoinOk && !leftScanOk {
					t.Fatalf("Expected left child to be a Join (nested join) or a Scan")
				}

				_, rightJoinOk := rootJoin.Right.(*LogicalJoinNode)
				_, rightScanOk := rootJoin.Right.(*LogicalScanNode)
				if !rightJoinOk && !rightScanOk {
					t.Fatalf("Expected right child to be a Join (nested join) or a Scan")
				}
			},
		},
		{
			name:        "Invalid Column in ON Clause",
			sql:         "SELECT * FROM users JOIN orders ON users.id = orders.banana_count",
			expectError: true,
		},
		{
			name:        "Ambiguous Column Reference in ON Clause",
			sql:         "SELECT * FROM users AS u1 JOIN users AS u2 ON id = id",
			expectError: true,
		},
		{
			name: "USING Clause (Single Column)",
			sql:  "SELECT * FROM users JOIN orders USING (country)",
			validate: func(scope *FromScope, node LogicalPlanNode, t *testing.T) {
				joinNode, ok := node.(*LogicalJoinNode)
				if !ok {
					t.Fatalf("Expected LogicalJoinNode")
				}

				if len(joinNode.joinOn) != 1 {
					t.Fatalf("Expected 1 join condition, got %d", len(joinNode.joinOn))
				}

				// verify predicate
				cmp, ok := joinNode.joinOn[0].(*ComparisonExpression)
				if !ok || cmp.compType != Equal {
					t.Fatal("USING clause should generate an Equality Comparison")
				}

				leftCol, ok := cmp.left.(*LogicalColumn)
				if !ok || leftCol.cname != "country" || leftCol.origin.alias != "users" {
					t.Errorf("Left operand should be 'users.country', got %s", cmp.left)
				}

				rightCol, ok := cmp.right.(*LogicalColumn)
				if !ok || rightCol.cname != "country" || rightCol.origin.alias != "orders" {
					t.Errorf("Right operand should be 'orders.country', got %s", cmp.right)
				}
			},
		},
		{
			name: "USING Clause (Multi-Column)",
			sql:  "SELECT * FROM users JOIN orders USING (country, year)",
			validate: func(scope *FromScope, node LogicalPlanNode, t *testing.T) {
				joinNode, ok := node.(*LogicalJoinNode)
				if !ok {
					t.Fatalf("Expected LogicalJoinNode")
				}

				// Should generate 2 conditions: country=country AND year=year
				if len(joinNode.joinOn) != 2 {
					t.Fatalf("Expected 2 join conditions, got %d", len(joinNode.joinOn))
				}

				// Check first condition (Parser preserves order)
				cmp1 := joinNode.joinOn[0].(*ComparisonExpression)
				if cmp1.left.(*LogicalColumn).cname != "country" {
					t.Error("Expected first condition to be on 'country'")
				}

				// Check second condition
				cmp2 := joinNode.joinOn[1].(*ComparisonExpression)
				if cmp2.left.(*LogicalColumn).cname != "year" {
					t.Error("Expected second condition to be on 'year'")
				}
			},
		},
		{
			name:        "USING Clause Error (Column Missing in Right Table)",
			sql:         "SELECT * FROM users JOIN orders USING (age)",
			expectError: true,
		},
		{
			name:        "USING Clause Error (Column Missing in Left Table)",
			sql:         "SELECT * FROM users JOIN orders USING (amount)",
			expectError: true,
		},
		{
			name:        "USING Clause Error (Non-Existent Column)",
			sql:         "SELECT * FROM users JOIN orders USING (banana)",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewLogicalPlanBuilder(cat)
			joinExpr := extractJoinExpr(t, tt.sql)

			scope, node, err := builder.planExplicitJoin(joinExpr)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
			} else {
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}
				if tt.validate != nil {
					tt.validate(scope, node, t)
				}
			}
		})
	}
}

func TestLogicalPlanBuilder(t *testing.T) {
	cat := getBasicTestCatalog()

	tests := []struct {
		name        string
		sql         string
		expectError bool
		validate    func(root LogicalPlanNode, t *testing.T)
	}{
		{
			name: "Simple Scan",
			sql:  "SELECT * FROM users",
			validate: func(root LogicalPlanNode, t *testing.T) {
				// Expect: Projection -> Scan
				proj, ok := root.(*LogicalProjectionNode)
				if !ok {
					t.Fatalf("Expected root to be Projection, got %T", root)
				}
				scan, ok := proj.Children()[0].(*LogicalScanNode)
				if !ok {
					t.Fatalf("Expected child to be Scan, got %T", proj.Children()[0])
				}
				if scan.GetTableAlias() != "users" {
					t.Errorf("Expected scan alias 'users', got '%s'", scan.GetTableAlias())
				}
			},
		},
		{
			name: "Filter and Projection",
			sql:  "SELECT name FROM users WHERE age > 21",
			validate: func(root LogicalPlanNode, t *testing.T) {
				// Expect: Projection -> Filter -> Scan
				proj := root.(*LogicalProjectionNode)
				filter, ok := proj.Children()[0].(*LogicalFilterNode)
				if !ok {
					t.Fatalf("Expected Filter node under Projection, got %T", proj.Children()[0])
				}
				_, ok = filter.Children()[0].(*LogicalScanNode)
				if !ok {
					t.Fatalf("Expected Scan node under Filter")
				}
			},
		},
		{
			name: "Join with Aliases",
			sql:  "SELECT u.name, o.amount FROM users u JOIN orders o ON u.id = o.user_id",
			validate: func(root LogicalPlanNode, t *testing.T) {
				// Expect: Projection -> Join -> (Scan u, Scan o)
				proj := root.(*LogicalProjectionNode)
				join, ok := proj.Children()[0].(*LogicalJoinNode)
				if !ok {
					t.Fatalf("Expected Join node, got %T", proj.Children()[0])
				}

				if join.joinType != Inner {
					t.Errorf("Expected Inner Join")
				}

				leftScan := join.Left.(*LogicalScanNode)
				rightScan := join.Right.(*LogicalScanNode)

				if leftScan.GetTableAlias() != "u" || rightScan.GetTableAlias() != "o" {
					t.Errorf("Expected aliases 'u' and 'o', got '%s' and '%s'", leftScan.GetTableAlias(), rightScan.GetTableAlias())
				}
			},
		},
		{
			name: "Aggregation (Group By)",
			sql:  "SELECT age, COUNT(*) FROM users GROUP BY age",
			validate: func(root LogicalPlanNode, t *testing.T) {
				// Expect: Projection -> Aggregation -> Scan
				proj := root.(*LogicalProjectionNode)
				agg, ok := proj.Children()[0].(*LogicalAggregationNode)
				if !ok {
					t.Fatalf("Expected Aggregation node, got %T", proj.Children()[0])
				}

				// Check grouping
				if len(agg.GroupByClause) != 1 {
					t.Errorf("Expected 1 GroupBy clause")
				}
				// Check output schema of Agg node contains "COUNT(*)" or similar
				foundCount := false
				for _, col := range agg.OutputSchema() {
					if strings.Contains(strings.ToUpper(col.cname), "COUNT") {
						foundCount = true
					}
				}
				if !foundCount {
					t.Errorf("Expected Aggregation schema to contain a COUNT column")
				}
				_, ok = agg.Children()[0].(*LogicalScanNode)
				if !ok {
					t.Fatalf("Expected LogicalScanNode at the leaf, got %T", agg.Children()[0])
				}
			},
		},
		{
			name:        "Unknown Table Error",
			sql:         "SELECT * FROM non_existent_table",
			expectError: true,
		},
		{
			name:        "Unknown Column Error",
			sql:         "SELECT bananas FROM users",
			expectError: true,
		},
		{
			name: "Subquery in FROM Clause",
			// Tests if the builder correctly handles the subquery boundary and alias
			sql: "SELECT sub.id FROM (SELECT id FROM users WHERE age > 10) AS sub",
			validate: func(root LogicalPlanNode, t *testing.T) {
				// Expect: Projection(sub.id) -> SubqueryNode -> Projection(id) -> Filter -> Scan

				projOuter, ok := root.(*LogicalProjectionNode)
				if !ok {
					t.Fatalf("Expected Root Projection, got %T", root)
				}

				subNode, ok := projOuter.Children()[0].(*LogicalSubqueryNode)
				if !ok {
					t.Fatalf("Expected LogicalSubqueryNode, got %T", projOuter.Children()[0])
				}

				innerRoot := subNode.Child
				projInner, ok := innerRoot.(*LogicalProjectionNode)
				if !ok {
					t.Fatalf("Expected Inner Root Projection, got %T", innerRoot)
				}

				filter, ok := projInner.Children()[0].(*LogicalFilterNode)
				if !ok {
					t.Fatalf("Expected Inner Filter")
				}

				scan, ok := filter.Children()[0].(*LogicalScanNode)
				if !ok {
					t.Fatalf("Expected Inner Scan")
				}
				if scan.GetTableAlias() != "users" {
					t.Errorf("Inner scan alias mismatch. Expected 'users', got '%s'", scan.GetTableAlias())
				}
			},
		},
		{
			name: "Self-Join (Same Table, Different Aliases)",
			// Critical for verifying that RefIDs are unique and aliases are respected
			sql: "SELECT a.name, b.name FROM users AS a JOIN users AS b ON a.age = b.age",
			validate: func(root LogicalPlanNode, t *testing.T) {
				proj := root.(*LogicalProjectionNode)
				join, ok := proj.Children()[0].(*LogicalJoinNode)
				if !ok {
					t.Fatalf("Expected Join Node, got %T", proj.Children()[0])
				}

				leftScan, ok := join.Left.(*LogicalScanNode)
				if !ok || leftScan.GetTableAlias() != "a" {
					t.Errorf("Expected Left Alias 'a', got '%s'", leftScan.GetTableAlias())
				}

				rightScan, ok := join.Right.(*LogicalScanNode)
				if !ok || rightScan.GetTableAlias() != "b" {
					t.Errorf("Expected Right Alias 'b', got '%s'", rightScan.GetTableAlias())
				}

				if leftScan.GetTableOid() != rightScan.GetTableOid() {
					t.Errorf("Self join tables should have the same OIDs")
				}
			},
		},
		{
			name: "3-Way Join (Left-Deep Tree)",
			// Verifies recursion in planExplicitJoin
			sql: "SELECT u.name FROM users u JOIN orders o ON u.id = o.user_id JOIN orders o2 ON u.id = o2.user_id",
			validate: func(root LogicalPlanNode, t *testing.T) {
				// Expect: Projection -> Join(Join(u, o), o2)
				proj := root.(*LogicalProjectionNode)

				// Top Join (joins [u+o] with [o2])
				topJoin, ok := proj.Children()[0].(*LogicalJoinNode)
				if !ok {
					t.Fatalf("Expected Top Join, got %T", proj.Children()[0])
				}

				// Check Right child of Top Join (should be o2)
				rightScan, ok := topJoin.Right.(*LogicalScanNode)
				if !ok || rightScan.GetTableAlias() != "o2" {
					t.Errorf("Expected Top Join Right Child to be 'o2', got %v", rightScan.GetTableAlias())
				}

				// Bottom Join (joins [u] with [o])
				bottomJoin, ok := topJoin.Left.(*LogicalJoinNode)
				if !ok {
					t.Fatalf("Expected Left Child to be nested Join")
				}

				leftScan := bottomJoin.Left.(*LogicalScanNode)
				if leftScan.GetTableAlias() != "u" {
					t.Errorf("Expected bottom left to be 'u'")
				}
			},
		},
		{
			name: "Complex WHERE Clause",
			sql:  "SELECT * FROM users WHERE age > 20 AND (name = 'Bob' OR name = 'Alice')",
			validate: func(root LogicalPlanNode, t *testing.T) {
				// Expect: Projection -> Scan
				proj, ok := root.(*LogicalProjectionNode)
				if !ok {
					t.Fatalf("Expected root to be Projection, got %T", root)
				}
				filter, ok := proj.Children()[0].(*LogicalFilterNode)
				if !ok {
					t.Fatalf("Expected Filter")
				}

				_, ok = filter.Predicate.(*BinaryLogicExpression)

				if !ok {
					t.Fatalf("Expected binary logic expression")
				}
			},
		},
		{
			name: "Order By and Limit",
			// Tests layering of Sort and Limit nodes
			sql: "SELECT name FROM users ORDER BY age DESC LIMIT 5",
			validate: func(root LogicalPlanNode, t *testing.T) {
				// Expect: Projection -> Limit -> Sort -> Scan
				proj := root.(*LogicalProjectionNode)

				limit, ok := proj.Children()[0].(*LogicalLimitNode)
				if !ok {
					t.Fatalf("Expected Limit node, got %T", proj.Children()[0])
				}

				sort, ok := limit.Children()[0].(*LogicalSortNode)
				if !ok {
					t.Fatalf("Expected Sort node, got %T", limit.Children()[0])
				}

				if len(sort.OrderBy) != 1 {
					t.Errorf("Expected 1 Sort key")
				}
				if sort.OrderBy[0].Direction != SortOrderDescending {
					t.Errorf("Expected Descending sort")
				}

				_, ok = sort.Children()[0].(*LogicalScanNode)
				if !ok {
					t.Errorf("Expected Scan under Sort")
				}
			},
		},
		{
			name: "Insert Values (Implicit Columns)",
			sql:  "INSERT INTO users VALUES (100, 'NewUser', 25, 'USA', 2023)",
			validate: func(root LogicalPlanNode, t *testing.T) {
				// Expect: Insert -> Values
				ins, ok := root.(*LogicalInsertNode)
				if !ok {
					t.Fatalf("Expected LogicalInsertNode, got %T", root)
				}
				if ins.TableOid != 1 { // users OID = 1
					t.Errorf("Expected Table OID 1, got %d", ins.TableOid)
				}

				// Should go directly to Values because columns match perfectly (implicit)
				// Note: Might be wrapped in Projection for explicit schema alignment.
				vals, ok := ins.Children()[0].(*LogicalValuesNode)
				if !ok {
					if proj, ok := ins.Children()[0].(*LogicalProjectionNode); ok {
						vals, ok = proj.Children()[0].(*LogicalValuesNode)
					}
				}

				if !ok {
					t.Fatalf("Expected ValuesNode under Insert")
				}
				if len(vals.Values) != 1 {
					t.Errorf("Expected 1 row of values")
				}
			},
		},
		{
			name: "Insert Values (Explicit Columns & Reordering)",
			sql:  "INSERT INTO users (name, id) VALUES ('Bob', 101)",
			validate: func(root LogicalPlanNode, t *testing.T) {
				// Expect: Insert -> Projection (schema alignment) -> Values
				ins, ok := root.(*LogicalInsertNode)
				if !ok {
					t.Fatalf("Expected InsertNode")
				}

				// Must have Projection to align (name, id) -> (id, name, age...)
				proj, ok := ins.Children()[0].(*LogicalProjectionNode)
				if !ok {
					t.Fatalf("Expected ProjectionNode for column alignment, got %T", ins.Children()[0])
				}
				if len(proj.OutputSchema()) != 5 {
					t.Errorf("Expected Projection output schema to have 5 columns, got %d", len(proj.OutputSchema()))
				}

				vals, ok := proj.Children()[0].(*LogicalValuesNode)
				if !ok {
					t.Fatalf("Expected ValuesNode")
				}

				if len(vals.Values) != 1 {
					t.Errorf("Expected 1 row")
				}
			},
		},
		{
			name: "Insert Select",
			sql:  "INSERT INTO users SELECT * FROM users WHERE age > 50",
			validate: func(root LogicalPlanNode, t *testing.T) {
				// Expect: Insert -> Projection -> Filter -> Scan
				ins, ok := root.(*LogicalInsertNode)
				if !ok {
					t.Fatalf("Expected InsertNode")
				}

				proj, ok := ins.Children()[0].(*LogicalProjectionNode)
				if !ok {
					t.Fatalf("Expected Projection from select query")
				}
				if len(proj.OutputSchema()) != 5 {
					t.Errorf("Expected Projection output schema to have 5 columns, got %d", len(proj.OutputSchema()))
				}
			},
		},
		{
			name:        "Insert Column Mismatch Error",
			sql:         "INSERT INTO users (id) VALUES (1, 'TooMany')",
			expectError: true,
		},
		{
			name: "Delete All",
			sql:  "DELETE FROM users",
			validate: func(root LogicalPlanNode, t *testing.T) {
				// Expect: Delete -> Scan
				del, ok := root.(*LogicalDeleteNode)
				if !ok {
					t.Fatalf("Expected DeleteNode, got %s.", root.String())
				}

				scan, ok := del.Children()[0].(*LogicalScanNode)
				if !ok {
					t.Fatalf("Expected ScanNode, got %s", del.String())
				}

				if scan.GetTableAlias() != "users" {
					t.Errorf("Incorrect table alias, expected 'users', got %s", scan.GetTableAlias())
				}
				if del.TableOid != 1 {
					t.Errorf("Incorrect delete table oid, expected 1, got %d", del.TableOid)
				}
			},
		},
		{
			name: "Delete With Where",
			sql:  "DELETE FROM users WHERE id = 5",
			validate: func(root LogicalPlanNode, t *testing.T) {
				// Expect: Delete -> Filter -> Scan
				del, ok := root.(*LogicalDeleteNode)
				if !ok {
					t.Fatalf("Expected DeleteNode, got %s", root.String())
				}
				if del.TableOid != 1 {
					t.Errorf("Incorrect delete table oid, expected 1, got %d", del.TableOid)
				}

				filter, ok := del.Children()[0].(*LogicalFilterNode)
				if !ok {
					t.Fatalf("Expected FilterNode")
				}

				cmp, ok := filter.Predicate.(*ComparisonExpression)
				if !ok {
					t.Fatalf("Expected Comparison Predicate")
				}
				if cmp.compType != Equal {
					t.Error("Expected Equality check")
				}
			},
		},
		{
			name: "Update Simple",
			sql:  "UPDATE users SET age = 30 WHERE id = 1",
			validate: func(root LogicalPlanNode, t *testing.T) {
				// Expect: Update -> Filter -> Scan
				upd, ok := root.(*LogicalUpdateNode)
				if !ok {
					t.Fatalf("Expected UpdateNode, got %s", root.String())
				}

				if upd.TableOid != 1 {
					t.Errorf("Incorrect update table oid, expected 1, got %d", upd.TableOid)
				}

				if len(upd.Updates) != 1 {
					t.Errorf("Expected 1 column update, got %d", len(upd.Updates))
				}

				// Verify we are updating 'age'
				// Since map key is *LogicalColumn pointer, we iterate to check name
				foundAge := false
				for col, expr := range upd.Updates {
					if col.cname == "age" {
						foundAge = true
						// Verify Value is Constant(30)
						if _, ok := expr.(*ConstantValueExpr); !ok {
							t.Error("Expected Constant value for age update")
						}
					}
				}
				if !foundAge {
					t.Error("Column 'age' not found in update map")
				}
			},
		},
		{
			name: "Update Arithmetic",
			sql:  "UPDATE users SET age = age + 1",
			validate: func(root LogicalPlanNode, t *testing.T) {
				// Expect: Update -> Scan
				upd, ok := root.(*LogicalUpdateNode)
				if !ok {
					t.Fatalf("Expected UpdateNode")
				}

				for col, expr := range upd.Updates {
					if col.cname == "age" {
						// Value should be ArithmeticExpression
						arith, ok := expr.(*ArithmeticExpression)
						if !ok {
							t.Errorf("Expected Arithmetic expr, got %T", expr)
						}
						if arith.op != Add {
							t.Error("Expected Add operation")
						}
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("--- Testing: %s ---\nSQL: %s\n", tt.name, tt.sql)
			builder := NewLogicalPlanBuilder(cat)
			stmt, err := sqlparser.Parse(tt.sql)
			if err != nil {
				t.Fatalf("SQL Parse Error: %v", err)
			}

			root, err := builder.Plan(stmt)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected an error but got none")
				} else {
					t.Logf("Got Expected Error: %v\n", err)
				}
			} else {
				if err != nil {
					t.Fatalf("Unexpected Plan Error: %v", err)
				}

				// Print the tree for sanity check
				printPlanTree(t, root, 0)

				// Run specific validation logic
				if tt.validate != nil {
					tt.validate(root, t)
				}
			}
		})
	}
}
