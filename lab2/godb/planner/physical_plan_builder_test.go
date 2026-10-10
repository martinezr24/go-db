package planner

import (
	"fmt"
	"testing"

	"mit.edu/dsg/godb/catalog"
	"mit.edu/dsg/godb/common"
	"mit.edu/dsg/godb/transaction"
)

func getPhysicalTestCatalog() *catalog.Catalog {
	result, _ := catalog.NewCatalog(catalog.NullPersistenceProvider{})
	_, _ = result.AddTable("users", []catalog.Column{
		{Name: "id", Type: common.IntType},
		{Name: "name", Type: common.StringType},
		{Name: "age", Type: common.IntType},
		{Name: "country", Type: common.StringType},
	})

	_, _ = result.AddTable("orders", []catalog.Column{
		{Name: "id", Type: common.IntType},
		{Name: "user_id", Type: common.IntType},
		{Name: "amount", Type: common.IntType},
		{Name: "country", Type: common.StringType},
		{Name: "year", Type: common.IntType},
	})

	_, _ = result.AddIndex("users_pk", "users", "btree", []string{"id"})
	_, _ = result.AddIndex("orders_uid_idx", "orders", "btree", []string{"user_id"})
	_, _ = result.AddIndex("orders_multi_idx", "orders", "btree", []string{"country", "year"})

	return result
}

func checkNodeType(t *testing.T, node PlanNode, expectedType string) {
	// Check for wrapped FilterNode first
	if filter, ok := node.(*FilterNode); ok {
		node = filter.Child
	}

	got := fmt.Sprintf("%T", node)
	if got != "*planner."+expectedType {
		t.Errorf("Expected node type %s, got %s", expectedType, got)
	}
}

func makeTableRef(t *catalog.Table, alias string, id uint64) *TableRef {
	ref := &TableRef{
		table: t,
		alias: alias,
		refID: id,
	}

	schema := make(LogicalSchema, len(t.Columns))
	for i, c := range t.Columns {
		schema[i] = &LogicalColumn{
			cname:  c.Name,
			ctype:  c.Type,
			origin: ref,
		}
	}
	ref.schema = schema
	return ref
}

// Helper to find a specific column in a TableRef by name.
// Use this to create predicates so the 'origin' pointer matches.
func findCol(ref *TableRef, name string) *LogicalColumn {
	for _, col := range ref.schema {
		if col.cname == name {
			return col
		}
	}
	panic("Column not found in test schema: " + name)
}

func assertLockMode(t *testing.T, node PlanNode, expected transaction.DBLockMode) {
	// Unwrap filters/projections if necessary
	if proj, ok := node.(*ProjectionNode); ok {
		node = proj.Child
	}
	if filter, ok := node.(*FilterNode); ok {
		node = filter.Child
	}

	switch n := node.(type) {
	case *SeqScanNode:
		if n.Mode != expected {
			t.Errorf("SeqScan: Expected lock Mode %v, got %v", expected, n.Mode)
		}
	// Note: IndexScanNode usually stores a bool 'ForUpdate' instead of raw mode
	// We can translate for the test check
	case *IndexScanNode:
		expectedBool := (expected == transaction.LockModeX)
		if n.ForUpdate != expectedBool {
			t.Errorf("IndexScan: Expected ForUpdate=%v, got %v", expectedBool, n.ForUpdate)
		}
	case *IndexLookupNode: // Same as IndexScan usually
		expectedBool := (expected == transaction.LockModeX)
		if n.ForUpdate != expectedBool {
			t.Errorf("IndexLookup: Expected ForUpdate=%v, got %v", expectedBool, n.ForUpdate)
		}
	default:
		t.Errorf("Node %T does not support locking inspection", node)
	}
}

func TestPhysicalRules(t *testing.T) {
	c := getPhysicalTestCatalog()
	users, _ := c.GetTableMetadata("users")
	orders, _ := c.GetTableMetadata("orders")

	binder := NewExpressionBinder()

	t.Run("Scan Rules", func(t *testing.T) {

		t.Run("SeqScan", func(t *testing.T) {
			ref := makeTableRef(users, "u", 1)
			scan := NewLogicalScanNode(ref, false)

			// predicate: u.age = 25
			scan.Predicates = []Expr{
				NewComparisonExpression(
					findCol(ref, "age"),
					NewConstantValueExpression(common.NewIntValue(25)),
					Equal,
				),
			}

			rule := &SeqScanRule{}
			if !rule.Match(scan, nil, c) {
				t.Fatal("SeqScan should match")
			}

			plan, _ := rule.Apply(scan, nil, c, binder)
			checkNodeType(t, plan, "SeqScanNode")
		})

		t.Run("IndexLookup (PK Equality)", func(t *testing.T) {
			ref := makeTableRef(users, "u", 2)
			scan := NewLogicalScanNode(ref, false)

			// predicate: u.id = 100 (indexed)
			scan.Predicates = []Expr{
				NewComparisonExpression(
					findCol(ref, "id"),
					NewConstantValueExpression(common.NewIntValue(100)),
					Equal,
				),
			}

			idxScanRule := &IndexScanRule{}
			if idxScanRule.Match(scan, nil, c) {
				t.Fatal("IndexScanRule should not match primary key equality predicate")
			}

			rule := &IndexLookupRule{}
			if !rule.Match(scan, nil, c) {
				t.Fatal("IndexLookup should match users id")
			}

			plan, err := rule.Apply(scan, nil, c, binder)
			if err != nil {
				t.Fatal(err)
			}

			realPlan := plan
			if f, ok := plan.(*FilterNode); ok {
				realPlan = f.Child
			}

			lookup, ok := realPlan.(*IndexLookupNode)
			if !ok {
				t.Fatalf("Expected IndexLookupNode, got %T", realPlan)
			}

			if lookup.IndexOid != 3 {
				t.Errorf("Wrong index. Expected 100, got %d", lookup.IndexOid)
			}
		})

		t.Run("IndexScan (Multi-Column)", func(t *testing.T) {
			ref := makeTableRef(orders, "o", 3)
			scan := NewLogicalScanNode(ref, false)

			// Predicate: country = 'USA' AND year > 2020
			scan.Predicates = []Expr{
				NewComparisonExpression(
					findCol(ref, "country"),
					NewConstantValueExpression(common.NewStringValue("USA")),
					Equal,
				),
				NewComparisonExpression(
					findCol(ref, "year"),
					NewConstantValueExpression(common.NewIntValue(2020)),
					GreaterThan,
				),
			}

			rule := &IndexScanRule{}
			if !rule.Match(scan, nil, c) {
				t.Fatal("IndexScan should match orders_multi_idx")
			}

			plan, _ := rule.Apply(scan, nil, c, binder)

			realPlan := plan
			if f, ok := plan.(*FilterNode); ok {
				realPlan = f.Child
			}

			idxScan, ok := realPlan.(*IndexScanNode)
			if !ok {
				t.Fatalf("Expected IndexScanNode, got %T", realPlan)
			}

			if idxScan.IndexOid != 5 {
				t.Errorf("Wrong index. Expected 201, got %d", idxScan.IndexOid)
			}
		})
	})

}

func TestJoinRules(t *testing.T) {
	cat := getPhysicalTestCatalog()
	users, _ := cat.GetTableMetadata("users")
	orders, _ := cat.GetTableMetadata("orders")

	binder := NewExpressionBinder()

	// 1. Setup Logical Nodes with valid TableRefs
	uRef := makeTableRef(users, "u", 10)
	oRef := makeTableRef(orders, "o", 11)

	leftLogical := NewLogicalScanNode(uRef, false)
	rightLogical := NewLogicalScanNode(oRef, false)

	leftPhys := NewSeqScanNode(users.Oid, tableSchemaToTypes(users), transaction.LockModeS)
	rightPhys := NewSeqScanNode(orders.Oid, tableSchemaToTypes(orders), transaction.LockModeS)

	children := []PlanNode{leftPhys, rightPhys}

	t.Run("IndexNestedLoopJoin", func(t *testing.T) {
		// join: u.id = o.user_id
		// u.id is col 0 of users. o.user_id is col 1 of orders.

		joinCond := NewComparisonExpression(
			findCol(uRef, "id"),      // LogicalColumn
			findCol(oRef, "user_id"), // LogicalColumn
			Equal,
		)
		join := NewLogicalJoinNode(leftLogical, rightLogical, []Expr{joinCond}, Inner)

		rule := &IndexNestedLoopJoinRule{}

		// Match should pass because 'orders' has an index on 'user_id'
		if !rule.Match(join, children, cat) {
			t.Fatal("Should match INLJ")
		}

		// Apply should succeed because 'leftPhys' now has a schema.
		// The binder will correctly map 'u.id' to 'input[0]' of the left child.
		plan, err := rule.Apply(join, children, cat, binder)
		if err != nil {
			t.Fatalf("Apply failed: %v", err)
		}

		inlj, ok := plan.(*IndexNestedLoopJoinNode)
		if !ok {
			t.Fatalf("Expected IndexNestedLoopJoinNode, got %T", plan)
		}

		// Verify correct index selection (orders_uid_idx OID is 200)
		if inlj.RightIndexOid != 4 {
			t.Errorf("Expected Index 200, got %d", inlj.RightIndexOid)
		}
	})

	t.Run("HashJoin", func(t *testing.T) {
		// join: u.age = o.amount (no index)
		joinCond := NewComparisonExpression(
			findCol(uRef, "age"),
			findCol(oRef, "amount"),
			Equal,
		)
		join := NewLogicalJoinNode(leftLogical, rightLogical, []Expr{joinCond}, Inner)

		rule := &HashJoinRule{}
		if !rule.Match(join, children, cat) {
			t.Fatal("HashJoin should match")
		}

		plan, err := rule.Apply(join, children, cat, binder)
		if err != nil {
			t.Fatal(err)
		}

		if _, ok := plan.(*HashJoinNode); !ok {
			t.Errorf("Expected HashJoinNode, got %T", plan)
		}
	})
}

func TestUpdateRules(t *testing.T) {
	cat := getPhysicalTestCatalog()
	users, _ := cat.GetTableMetadata("users")

	binder := NewExpressionBinder()

	userTypes := make([]common.Type, len(users.Columns))
	for i, c := range users.Columns {
		userTypes[i] = c.Type
	}

	t.Run("Values Rule", func(t *testing.T) {
		// Logical: VALUES (1, 'Alice'), (2, 'Bob')
		row1 := []Expr{
			NewConstantValueExpression(common.NewIntValue(1)),
			NewConstantValueExpression(common.NewStringValue("Alice")),
		}
		row2 := []Expr{
			NewConstantValueExpression(common.NewIntValue(2)),
			NewConstantValueExpression(common.NewStringValue("Bob")),
		}

		valSchema := LogicalSchema{
			{cname: "id", ctype: common.IntType},
			{cname: "name", ctype: common.StringType},
		}

		logicalValues := NewLogicalValuesNode([][]Expr{row1, row2}, valSchema)

		rule := &ValuesRule{}
		if !rule.Match(logicalValues, nil, cat) {
			t.Fatal("ValuesRule should match")
		}

		plan, err := rule.Apply(logicalValues, nil, cat, binder)
		if err != nil {
			t.Fatalf("Apply failed: %v", err)
		}

		physValues, ok := plan.(*ValuesNode)
		if !ok {
			t.Fatalf("Expected ValuesNode, got %T", plan)
		}

		if len(physValues.Values) != 2 {
			t.Errorf("Expected 2 rows, got %d", len(physValues.Values))
		}
	})

	t.Run("Insert Rule Basic", func(t *testing.T) {
		mockChildLogical := NewLogicalValuesNode(nil, nil)
		logicalInsert := NewLogicalInsertNode(users.Oid, mockChildLogical)

		physChild := NewValuesNode(nil, nil)
		children := []PlanNode{physChild}

		rule := &InsertRule{}
		if !rule.Match(logicalInsert, children, cat) {
			t.Fatal("InsertRule should match")
		}

		plan, err := rule.Apply(logicalInsert, children, cat, binder)
		if err != nil {
			t.Fatal(err)
		}

		insNode, ok := plan.(*InsertNode)
		if !ok {
			t.Fatalf("Expected InsertNode, got %T", plan)
		}

		if insNode.TableOid != users.Oid {
			t.Errorf("Expected TableOID %d, got %d", users.Oid, insNode.TableOid)
		}
	})

	t.Run("Delete Rule Basic", func(t *testing.T) {
		ref := makeTableRef(users, "u", 50)
		logicalScan := NewLogicalScanNode(ref, true)
		logicalDelete := NewLogicalDeleteNode(users.Oid, logicalScan)

		physScan := NewSeqScanNode(users.Oid, userTypes, transaction.LockModeX)
		children := []PlanNode{physScan}

		rule := &DeleteRule{}
		if !rule.Match(logicalDelete, children, cat) {
			t.Fatal("DeleteRule should match")
		}

		plan, err := rule.Apply(logicalDelete, children, cat, binder)
		if err != nil {
			t.Fatal(err)
		}

		delNode, ok := plan.(*DeleteNode)
		if !ok {
			t.Fatalf("Expected DeleteNode, got %T", plan)
		}

		if delNode.TableOid != users.Oid {
			t.Error("Incorrect TableOID")
		}
	})

	t.Run("Update Rule", func(t *testing.T) {
		ref := makeTableRef(users, "u", 60)
		logicalScan := NewLogicalScanNode(ref, true)

		// Map: LogicalColumn(age) -> Arithmetic(age + 1)
		ageCol := findCol(ref, "age") // "age" is index 2 in users table

		// expr: age + 1
		updateExpr := NewArithmeticExpression(
			ageCol,
			NewConstantValueExpression(common.NewIntValue(1)),
			Add,
		)

		updates := map[*LogicalColumn]Expr{
			ageCol: updateExpr,
		}

		logicalUpdate := NewLogicalUpdateNode(users.Oid, logicalScan, updates)

		physScan := NewSeqScanNode(users.Oid, userTypes, transaction.LockModeX)
		children := []PlanNode{physScan}
		rule := &UpdateRule{}
		if !rule.Match(logicalUpdate, children, cat) {
			t.Fatal("UpdateRule should match")
		}

		plan, err := rule.Apply(logicalUpdate, children, cat, binder)
		if err != nil {
			t.Fatalf("Apply failed: %v", err)
		}

		updNode, ok := plan.(*UpdateNode)
		if !ok {
			t.Fatalf("Expected UpdateNode, got %T", plan)
		}

		// users columns: id(0), name(1), age(2), country(3)
		if len(updNode.Expressions) != 4 {
			t.Errorf("Expected 4 update column, got %d", len(updNode.Expressions))
		}

		physExpr := updNode.Expressions[2] // age

		// verify binding
		// The expression should now be a Physical Arithmetic Expression
		// Left side should be ColumnValueExpr(Index 2)
		arith, ok := physExpr.(*ArithmeticExpression)
		if !ok {
			t.Fatalf("Expected ArithmeticExpr, got %T", physExpr)
		}

		colVal, ok := arith.left.(*BoundValueExpr)
		if !ok {
			t.Fatalf("Expected Left side to be BoundValueExpr, got %T", arith.left)
		}

		if colVal.fieldOffset != 2 {
			t.Errorf("Expected 'age' to be bound to Input Index 2, got %d", colVal.fieldOffset)
		}
	})
}

func TestConcurrencyRules(t *testing.T) {
	cat := getPhysicalTestCatalog()
	users, err := cat.GetTableMetadata("users")
	if err != nil {
		t.Fatalf("Failed to get users metadata: %v", err)
	}
	orders, err := cat.GetTableMetadata("orders")
	if err != nil {
		t.Fatalf("Failed to get orders metadata: %v", err)
	}
	binder := NewExpressionBinder()

	t.Run("Read-Only Scan (SELECT)", func(t *testing.T) {
		// Logical: SELECT * FROM users
		ref := makeTableRef(users, "u", 1)
		scan := NewLogicalScanNode(ref, false)

		rule := &SeqScanRule{}
		plan, _ := rule.Apply(scan, nil, cat, binder)

		assertLockMode(t, plan, transaction.LockModeS)
	})

	t.Run("Write-Intent Scan (UPDATE/DELETE)", func(t *testing.T) {
		ref := makeTableRef(users, "u", 2)
		scan := NewLogicalScanNode(ref, true)

		rule := &SeqScanRule{}
		plan, _ := rule.Apply(scan, nil, cat, binder)

		assertLockMode(t, plan, transaction.LockModeX)
	})

	t.Run("Index Scan Locking", func(t *testing.T) {
		// Logical: DELETE FROM orders WHERE user_id = 5 (Index Scan)
		ref := makeTableRef(orders, "o", 3)
		scan := NewLogicalScanNode(ref, true)

		scan.Predicates = []Expr{
			NewComparisonExpression(findCol(ref, "user_id"), NewConstantValueExpression(common.NewIntValue(5)), Equal),
		}

		rule := &IndexLookupRule{}
		if !rule.Match(scan, nil, cat) {
			t.Fatal("Should match index")
		}

		plan, _ := rule.Apply(scan, nil, cat, binder)

		// Expect ForUpdate = true
		assertLockMode(t, plan, transaction.LockModeX)
	})
}
