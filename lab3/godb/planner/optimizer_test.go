package planner

import (
	"testing"

	"github.com/xwb1989/sqlparser"
)

// Helper to manually construct a filter node on top of a plan
func wrapInFilterTest(t *testing.T, child LogicalPlanNode, sqlExpr string, scope *FromScope, builder *LogicalPlanBuilder) *LogicalFilterNode {
	exprAST := parseExpr(t, sqlExpr)
	expr, err := builder.bindExpr(exprAST, scope)
	if err != nil {
		t.Fatalf("Failed to bind filter expr '%s': %v", sqlExpr, err)
	}
	return NewLogicalFilterNode(child, expr)
}

func TestPredicatePushDownOnce(t *testing.T) {
	cat := getBasicTestCatalog()
	builder := NewLogicalPlanBuilder(cat)
	rule := &PredicatePushDownRule{}

	tests := []struct {
		name     string
		setupSQL string // Used to build the initial plan (e.g., "SELECT * FROM ...")

		// Inject the filter
		filterSQL string

		validate func(orig, opt LogicalPlanNode, t *testing.T)
	}{
		{
			name:      "Push to Scan",
			setupSQL:  "SELECT * FROM users",
			filterSQL: "age > 21",
			validate: func(orig, opt LogicalPlanNode, t *testing.T) {
				scan, ok := opt.(*LogicalScanNode)
				if !ok {
					t.Fatalf("Expected Filter to be absorbed into Scan, got %T", opt)
				}

				if len(scan.Predicates) != 1 {
					t.Errorf("Expected 1 predicate on the LogicalScanNode, got %v.", scan.Predicates)
				}

				cols := scan.Predicates[0].GetReferencedColumns()
				if len(cols) != 1 || cols[0].cname != "age" {
					t.Errorf("Expected 1 referenced column (age) on the predicate on the LogicalScanNode, got %v", cols)
				}
				compExpr, ok := scan.Predicates[0].(*ComparisonExpression)
				if !ok || compExpr.compType != GreaterThan {
					t.Errorf("Expected predicate 'age > 21, got '%s'", scan.Predicates[0].String())
				}
			},
		},
		{
			name: "Push Through Inner Join (Distribute)",
			// Plan: Filter(age>20 AND amount>50) -> Join(users, orders)
			setupSQL:  "SELECT * FROM users JOIN orders ON users.id = orders.user_id",
			filterSQL: "age > 20 AND amount > 50",
			validate: func(orig, opt LogicalPlanNode, t *testing.T) {
				// The root should now be the Join (Filter split and pushed down)
				join, ok := opt.(*LogicalJoinNode)
				if !ok {
					t.Fatalf("Expected root to be JoinNode, got %T", opt)
				}

				// Verify Left Child (users) has Filter(age > 20)
				// Note: The rule wraps the scan in a NEW FilterNode,
				// it doesn't merge into scan immediately (unless recursive).
				leftFilter, ok := join.Left.(*LogicalFilterNode)
				if !ok {
					t.Errorf("Expected Left Child to be Filter (pushed down predicate)")
				} else {
					expr, predOk := leftFilter.Predicate.(*ComparisonExpression)
					if !predOk || expr.compType != GreaterThan || expr.GetReferencedColumns()[0].cname != "age" {
						t.Errorf("Expected Left Child to have predicate age > 20, got %v", expr)
					}
				}

				// Verify Right Child (orders) has Filter(amount > 50)
				rightFilter, ok := join.Right.(*LogicalFilterNode)
				if !ok {
					t.Errorf("Expected Right Child to be Filter")
				} else {
					expr, predOk := rightFilter.Predicate.(*ComparisonExpression)
					if !predOk || expr.compType != GreaterThan || expr.GetReferencedColumns()[0].cname != "amount" {
						t.Errorf("Expected Left Child to have predicate amount > 50, got %v", expr)
					}
				}
			},
		},
		{
			name: "Push Into Join Condition (Cross -> Inner)",
			// Plan: Filter(users.id = orders.user_id) -> CrossJoin(users, orders)
			setupSQL:  "SELECT * FROM users, orders", // Implicit Cross Join
			filterSQL: "users.id = orders.user_id",
			validate: func(orig, opt LogicalPlanNode, t *testing.T) {
				// Expect: Inner Join with condition, NO filter on top
				join, ok := opt.(*LogicalJoinNode)
				if !ok {
					t.Fatalf("Expected JoinNode, got %T", opt)
				}

				if join.joinType != Inner {
					t.Errorf("Expected Join to be upgraded to INNER, got %v", join.joinType)
				}

				// Check Join Conditions
				if len(join.joinOn) == 0 {
					t.Error("Expected join condition to be populated")
				}
			},
		},
		{
			name: "Block Pushdown on Left Join (Right Side)",
			// SQL: SELECT * FROM users LEFT JOIN orders ON ... WHERE orders.amount > 50
			// Since orders is the Nullable side, 'amount > 50' filters out NULLs, turning it into Inner.
			// However, we don't currently implement this optimization.
			setupSQL:  "SELECT * FROM users LEFT JOIN orders ON users.id = orders.user_id",
			filterSQL: "amount > 50", // 'amount' is in 'orders' (Right/Nullable side)
			validate: func(orig, opt LogicalPlanNode, t *testing.T) {
				// Filter -> Join

				filter, ok := opt.(*LogicalFilterNode)
				if !ok {
					t.Fatalf("Expected Filter to remain on top of Left Join")
				}

				join, ok := filter.Child.(*LogicalJoinNode)
				if !ok {
					t.Fatal("Filter child should be Join")
				}
				if join.joinType != Left {
					t.Error("Join type should remain Left")
				}
			},
		},
		{
			name: "Push Through Left Join (Left Side)",
			// SQL: SELECT * FROM users LEFT JOIN orders ... WHERE users.age > 20
			// Can safely push to Left side because filtering the "Preserved" row source is always valid.
			setupSQL:  "SELECT * FROM users LEFT JOIN orders ON users.id = orders.user_id",
			filterSQL: "age > 20", // 'age' is in 'users' (Left side)
			validate: func(orig, opt LogicalPlanNode, t *testing.T) {
				// Expect: Join (Filter pushed to left)
				join, ok := opt.(*LogicalJoinNode)
				if !ok {
					t.Fatalf("Expected root to be Join (Filter pushed)")
				}

				// Left child should now be a Filter
				_, ok = join.Left.(*LogicalFilterNode)
				if !ok {
					t.Error("Expected Left child to have the pushed filter")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			stmt, _ := sqlparser.Parse(tt.setupSQL)
			sel := stmt.(*sqlparser.Select)
			scope, rootNode, _ := builder.planFrom(sel.From)

			filterNode := wrapInFilterTest(t, rootNode, tt.filterSQL, scope, builder)

			if !rule.Match(filterNode) {
				t.Fatal("Rule failed to match FilterNode")
			}
			optimizedNode := rule.Apply(filterNode)

			tt.validate(filterNode, optimizedNode, t)
		})
	}
}

func TestProjectionPushDownRule(t *testing.T) {
	cat := getBasicTestCatalog()
	builder := NewLogicalPlanBuilder(cat)
	optimizer := NewOptimizer([]LogicalRule{&ProjectionPushDownRule{}})

	tests := []struct {
		name     string
		setupSQL string

		validate func(node LogicalPlanNode, t *testing.T)
	}{
		{
			name:     "Prune Scan (Wrap in Project)",
			setupSQL: "SELECT name FROM users", // Outputs: id, name, age, bio
			validate: func(node LogicalPlanNode, t *testing.T) {
				// Expect: Project([name]) -> Scan
				proj, ok := node.(*LogicalProjectionNode)
				if !ok {
					t.Fatalf("Expected Scan to be wrapped in Project, got %T", node)
				}

				if len(proj.OutputSchema()) != 1 || proj.OutputSchema()[0].cname != "name" {
					t.Errorf("Expected Project to output [name], got %v", proj.OutputSchema())
				}

				_, ok = proj.Child.(*LogicalScanNode)
				if !ok {
					t.Errorf("Expected child of top level Project to be Scan")
				}
			},
		},
		{
			name: "Prune Join (Wrap in Project)",
			// Join outputs: u.id, u.name... o.id, o.uid...
			setupSQL: "SELECT name FROM users JOIN orders ON users.id = orders.user_id",
			validate: func(node LogicalPlanNode, t *testing.T) {
				// Expect: Project([name]) -> Join
				proj, ok := node.(*LogicalProjectionNode)
				if !ok {
					t.Fatalf("Expected Join to be wrapped in Project, got %T", node)
				}

				join, ok := proj.Child.(*LogicalJoinNode)
				if !ok {
					t.Fatal("Expected child to be Join")
				}
				if _, ok := join.Left.(*LogicalProjectionNode); !ok {
					t.Error("Expected Left child of Join to be pruned (wrapped in Project)")
				}
			},
		},
		{
			name:     "Aggregation Pruning",
			setupSQL: "SELECT count(*) FROM users GROUP BY age",
			validate: func(node LogicalPlanNode, t *testing.T) {
				// Expect: Project([count]) -> Aggregation
				proj, ok := node.(*LogicalProjectionNode)
				if !ok {
					t.Fatalf("Expected Aggregation to be wrapped, got %T", node)
				}

				if len(proj.OutputSchema()) != 1 {
					t.Errorf("Expected 1 output column, got %d", len(proj.OutputSchema()))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stmt, _ := sqlparser.Parse(tt.setupSQL)
			sel := stmt.(*sqlparser.Select)
			root, err := builder.planSelect(sel)
			if err != nil {
				t.Errorf("planSelect failed with %v", err)
			}

			optNode := optimizer.Optimize(root)
			tt.validate(optNode, t)
		})
	}
}

func TestPredicatePushDownFullRule(t *testing.T) {
	cat := getBasicTestCatalog()
	builder := NewLogicalPlanBuilder(cat)
	optimizer := NewOptimizer([]LogicalRule{&PredicatePushDownRule{}})

	tests := []struct {
		name     string
		setupSQL string // Used to build the initial plan (e.g., "SELECT * FROM ...")

		// We inject the filter manually to simulate the unoptimized state
		filterSQL string

		validate func(orig, opt LogicalPlanNode, t *testing.T)
	}{
		{
			name:      "Push to Scan",
			setupSQL:  "SELECT * FROM users",
			filterSQL: "age > 21",
			validate: func(orig, opt LogicalPlanNode, t *testing.T) {
				// 1. Result should be a ScanNode (Filter absorbed)
				scan, ok := opt.(*LogicalScanNode)
				if !ok {
					t.Fatalf("Expected Filter to be absorbed into Scan, got %T", opt)
				}

				if len(scan.Predicates) != 1 {
					t.Errorf("Expected 1 predicate on the LogicalScanNode, got %v.", scan.Predicates)
				}

				cols := scan.Predicates[0].GetReferencedColumns()
				if len(cols) != 1 || cols[0].cname != "age" {
					t.Errorf("Expected 1 referenced column (age) on the predicate on the LogicalScanNode, got %v", cols)
				}
				compExpr, ok := scan.Predicates[0].(*ComparisonExpression)
				if !ok || compExpr.compType != GreaterThan {
					t.Errorf("Expected predicate 'age > 21, got '%s'", scan.Predicates[0].String())
				}
			},
		},
		{
			name: "Push Through Inner Join (Distribute)",
			// Plan: Filter(age>20 AND amount>50) -> Join(users, orders)
			setupSQL:  "SELECT * FROM users JOIN orders ON users.id = orders.user_id",
			filterSQL: "age > 20 AND amount > 50",
			validate: func(orig, opt LogicalPlanNode, t *testing.T) {
				// The root should now be the Scan (Filter split and pushed all the waydown)
				join, ok := opt.(*LogicalJoinNode)
				if !ok {
					t.Fatalf("Expected root to be JoinNode, got %T", opt)
				}

				// Verify Left Child (users) is a Scan with Filter(age > 20)
				scan, ok := join.Left.(*LogicalScanNode)
				if !ok || len(scan.Predicates) != 1 {
					t.Errorf("Expected Left Child to be a Scan with 1 pushed down predicate")
				}

				// Verify Right Child (orders) is a Scan with Filter(amount > 50)
				scan, ok = join.Right.(*LogicalScanNode)
				if !ok || len(scan.Predicates) != 1 {
					t.Errorf("Expected Right Child to be a Scan with 1 pushed down predicate")
				}
			},
		},
		{
			name: "Push Into Join Condition (Cross -> Inner)",
			// Plan: Filter(users.id = orders.user_id) -> CrossJoin(users, orders)
			setupSQL:  "SELECT * FROM users, orders", // Implicit Cross Join
			filterSQL: "users.id = orders.user_id",
			validate: func(orig, opt LogicalPlanNode, t *testing.T) {
				// Expect: Inner Join with condition, NO filter on top
				join, ok := opt.(*LogicalJoinNode)
				if !ok {
					t.Fatalf("Expected JoinNode, got %T", opt)
				}

				if join.joinType != Inner {
					t.Errorf("Expected Join to be upgraded to INNER, got %v", join.joinType)
				}

				// Check Join Conditions
				if len(join.joinOn) == 0 {
					t.Error("Expected join condition to be populated")
				}
			},
		},
		{
			name: "Block Pushdown on Left Join (Right Side)",
			// SQL: SELECT * FROM users LEFT JOIN orders ON ... WHERE orders.amount > 50
			// Since orders is the Nullable side, 'amount > 50' filters out NULLs
			// However, we don't currently implement this optimization.
			setupSQL:  "SELECT * FROM users LEFT JOIN orders ON users.id = orders.user_id",
			filterSQL: "amount > 50", // 'amount' is in 'orders' (Right/Nullable side)
			validate: func(orig, opt LogicalPlanNode, t *testing.T) {
				// Expect: Filter -> Join
				filter, ok := opt.(*LogicalFilterNode)
				if !ok {
					t.Fatalf("Expected Filter to remain on top of Left Join")
				}

				join, ok := filter.Child.(*LogicalJoinNode)
				if !ok {
					t.Fatal("Filter child should be Join")
				}
				if join.joinType != Left {
					t.Error("Join type should remain Left")
				}
			},
		},
		{
			name: "Push Through Left Join (Left Side)",
			// SQL: SELECT * FROM users LEFT JOIN orders ... WHERE users.age > 20
			// Can safely push to Left side because filtering the "Preserved" row source is always valid.
			setupSQL:  "SELECT * FROM users LEFT JOIN orders ON users.id = orders.user_id",
			filterSQL: "age > 20", // 'age' is in 'users' (Left side)
			validate: func(orig, opt LogicalPlanNode, t *testing.T) {
				// Expect: Join (Filter pushed to left)
				join, ok := opt.(*LogicalJoinNode)
				if !ok {
					t.Fatalf("Expected root to be Join (Filter pushed)")
				}

				// Left child should now be a Scan
				scan, ok := join.Left.(*LogicalScanNode)
				if !ok || len(scan.Predicates) != 1 {
					t.Error("Expected Left child to be a LogicalScanNode with 1 pushed filter")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			stmt, _ := sqlparser.Parse(tt.setupSQL)
			sel := stmt.(*sqlparser.Select)
			scope, rootNode, _ := builder.planFrom(sel.From)

			filterNode := wrapInFilterTest(t, rootNode, tt.filterSQL, scope, builder)

			optimizedNode := optimizer.Optimize(filterNode)

			tt.validate(filterNode, optimizedNode, t)
		})
	}
}

func TestOptimizerIntegrated(t *testing.T) {
	cat := getBasicTestCatalog()
	builder := NewLogicalPlanBuilder(cat)
	optimizer := NewOptimizer([]LogicalRule{&PredicatePushDownRule{}, &ProjectionPushDownRule{}})

	tests := []struct {
		name     string
		fullSQL  string // Full SELECT statement
		validate func(root LogicalPlanNode, t *testing.T)
	}{
		{
			name:    "Scan Pruning (Select Subset)",
			fullSQL: "SELECT name FROM users",
			validate: func(root LogicalPlanNode, t *testing.T) {
				// Project(name)  -> Scan
				topProj, ok := root.(*LogicalProjectionNode)
				if !ok {
					t.Fatalf("Expected Root Projection")
				}

				if len(topProj.OutputSchema()) != 1 || topProj.OutputSchema()[0].cname != "name" {
					t.Errorf("Project should output only [name]")
				}

				_, ok = topProj.Child.(*LogicalScanNode)
				if !ok {
					t.Errorf("Expected child of Projection to be Scan")
				}
			},
		},
		{
			name:    "Filter and Projection Pushdown",
			fullSQL: "SELECT name FROM users WHERE age > 21",
			validate: func(root LogicalPlanNode, t *testing.T) {
				// Expected Tree:
				// Project(name) -> Scan with Filter(age>21)

				topProj := root.(*LogicalProjectionNode)
				scan, ok := topProj.Child.(*LogicalScanNode)
				if !ok {
					t.Fatalf("Expected Scan under top Project, got %s", topProj.Child.String())
				}

				// check that scan has the required filter
				if len(scan.Predicates) != 1 {
					t.Errorf("Expected 1 predicate on the LogicalScanNode, got %v.", scan.Predicates)
				} else {
					cols := scan.Predicates[0].GetReferencedColumns()
					if len(cols) != 1 || cols[0].cname != "age" {
						t.Errorf("Expected 1 referenced column (age) on the predicate on the LogicalScanNode, got %v", cols)
					}
					compExpr, ok := scan.Predicates[0].(*ComparisonExpression)
					if !ok || compExpr.compType != GreaterThan {
						t.Errorf("Expected predicate 'age > 21, got '%s'", scan.Predicates[0].String())
					}
				}
			},
		},
		{
			name:    "Join Pruning",
			fullSQL: "SELECT u.name FROM users u JOIN orders o ON u.id = o.user_id",
			validate: func(root LogicalPlanNode, t *testing.T) {
				// Expected Tree:
				// Project(u.name) -> Join -> (Left, Right)
				// Left (Users): Project(name, id) -> Scan
				// Right (Orders): Project(user_id) -> Scan

				topProj := root.(*LogicalProjectionNode)
				join, ok := topProj.Child.(*LogicalJoinNode)

				if proj, isProj := topProj.Child.(*LogicalProjectionNode); isProj {
					join, ok = proj.Child.(*LogicalJoinNode)
				}

				if !ok {
					t.Fatalf("Expected Join Node")
				}

				// Verify Left Pruning (Users)
				// Needs: name (output), id (join key)
				leftWrapper, ok := join.Left.(*LogicalProjectionNode)
				if !ok {
					t.Error("Expected Left child (Users) to be pruned")
				} else if len(leftWrapper.OutputSchema()) != 2 {
					t.Errorf("Expected Left pruned schema length 2 (name, id), got %d", len(leftWrapper.OutputSchema()))
				}

				// Verify Right Pruning (Orders)
				// Needs: user_id (join key only). We don't select anything from orders!
				rightWrapper, ok := join.Right.(*LogicalProjectionNode)
				if !ok {
					t.Error("Expected Right child (Orders) to be pruned")
				} else if len(rightWrapper.OutputSchema()) != 1 {
					t.Errorf("Expected Right pruned schema length 1 (user_id), got %d", len(rightWrapper.OutputSchema()))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stmt, _ := sqlparser.Parse(tt.fullSQL)
			sel := stmt.(*sqlparser.Select)

			plan, err := builder.Plan(sel)
			if err != nil {
				t.Fatalf("Failed to build plan: %v", err)
			}

			optPlan := optimizer.Optimize(plan)
			tt.validate(optPlan, t)
		})
	}
}
