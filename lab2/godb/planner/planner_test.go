package planner

import (
	"testing"

	"github.com/xwb1989/sqlparser"
	"mit.edu/dsg/godb/transaction"
)

func TestE2E_PhysicalPlanGeneration(t *testing.T) {
	cat := getBasicTestCatalog()
	_, _ = cat.AddIndex("idx_orders_uid", "orders", "btree", []string{"user_id"})
	_, _ = cat.AddIndex("idx_users_pk", "users", "btree", []string{"id"})

	logicalBuilder := NewLogicalPlanBuilder(cat)
	physicalBuilder := NewPhysicalPlanBuilder(cat, []PhysicalConversionRule{
		&SeqScanRule{},
		&IndexScanRule{},
		&IndexLookupRule{},
		&IndexNestedLoopJoinRule{},
		&SortMergeJoinRule{},
		&HashJoinRule{},
		&BlockNestedLoopJoinRule{},
		&LimitRule{},
		&SubqueryRule{},
		&AggregationRule{},
		&ProjectionRule{},
		&FilterRule{},
		&SortRule{},
		&InsertRule{},
		&DeleteRule{},
		&UpdateRule{},
		&ValuesRule{},
	})
	optimizer := NewOptimizer([]LogicalRule{
		&PredicatePushDownRule{},
		&ProjectionPushDownRule{},
	})

	tests := []struct {
		name     string
		sql      string
		validate func(root PlanNode, t *testing.T)
	}{
		{
			name: "simple select (index lookup)",
			sql:  "SELECT * FROM users WHERE id = 5",
			validate: func(root PlanNode, t *testing.T) {
				// Projection -> Filter -> IndexLookup
				proj, ok := root.(*ProjectionNode)
				if !ok {
					t.Fatalf("Expected ProjectionNode as root, got %T", root)
				}

				filter, ok := proj.Child.(*FilterNode)
				if !ok {
					t.Fatalf("Expected FilterNode under Projection, got %T", proj.Child)
				}

				lookup, ok := filter.Child.(*IndexLookupNode)
				if !ok {
					t.Fatalf("Expected IndexLookupNode under Filter, got %T", filter.Child)
				}

				if lookup.IndexOid != 5 {
					t.Errorf("Expected IndexOID 101 (users_pk), got %d", lookup.IndexOid)
				}
			},
		},
		{
			name: "join selection (index nested loop)",
			// users (left) join orders (right) on u.id = o.user_id
			sql: "SELECT * FROM users u, orders o WHERE u.id = o.user_id",
			validate: func(root PlanNode, t *testing.T) {
				// Expect: Projection -> IndexNestedLoopJoin
				proj, ok := root.(*ProjectionNode)
				if !ok {
					t.Fatalf("Expected ProjectionNode, got %T", root)
				}

				join, ok := proj.Child.(*IndexNestedLoopJoinNode)
				if !ok {
					t.Fatalf("Expected IndexNestedLoopJoinNode, got %T", proj.Child)
				}

				if join.RightIndexOid != 4 {
					t.Errorf("Expected usage of Index 100, got %d", join.RightIndexOid)
				}
			},
		},
		{
			name: "join selection (hash join)",
			sql:  "SELECT * FROM users u, orders o WHERE u.age = o.amount",
			validate: func(root PlanNode, t *testing.T) {
				// Expect: Projection -> HashJoin (amount is not indexed)
				proj, ok := root.(*ProjectionNode)
				if !ok {
					t.Fatalf("Expected ProjectionNode, got %T", root)
				}

				_, ok = proj.Child.(*HashJoinNode)
				if !ok {
					t.Fatalf("Expected HashJoinNode, got %T", proj.Child)
				}
			},
		},
		{
			name: "aggregation (group by)",
			sql:  "SELECT user_id, COUNT(*) FROM orders GROUP BY user_id",
			validate: func(root PlanNode, t *testing.T) {
				// Expect: Projection -> Aggregate
				proj, ok := root.(*ProjectionNode)
				if !ok {
					t.Fatalf("Expected ProjectionNode, got %T", root)
				}

				agg, ok := proj.Child.(*AggregateNode)
				if !ok {
					t.Fatalf("Expected AggregateNode, got %T", proj.Child)
				}

				if len(agg.GroupByClause) != 1 {
					t.Errorf("Expected 1 GroupBy clause")
				}
			},
		},
		{
			name: "order By + limit (TopN optimization)",
			sql:  "SELECT name FROM users ORDER BY age DESC LIMIT 5",
			validate: func(root PlanNode, t *testing.T) {
				// Expect: Projection -> TopN
				proj, ok := root.(*ProjectionNode)
				if !ok {
					t.Fatalf("Expected ProjectionNode, got %T", root)
				}

				topN, ok := proj.Child.(*TopNNode)
				if !ok {
					t.Fatalf("Expected TopNNode, got %T", proj.Child)
				}

				if topN.Limit != 5 {
					t.Errorf("Expected Limit 5, got %d", topN.Limit)
				}
			},
		},
		{
			name: "insert statement",
			sql:  "INSERT INTO users (id, name) VALUES (99, 'Tester')",
			validate: func(root PlanNode, t *testing.T) {
				// InsertNode -> Projection (Alignment) -> ValuesNode
				ins, ok := root.(*InsertNode)
				if !ok {
					t.Fatalf("Expected InsertNode as root, got %T", root)
				}

				proj, ok := ins.Child.(*ProjectionNode)
				if !ok {
					t.Fatalf("Expected ProjectionNode under Insert, got %T", ins.Child)
				}

				_, ok = proj.Child.(*ValuesNode)
				if !ok {
					t.Fatalf("Expected ValuesNode under Projection, got %T", proj.Child)
				}
			},
		},
		{
			name: "concurrency: simple select (read lock)",
			sql:  "SELECT * FROM users",
			validate: func(root PlanNode, t *testing.T) {
				// Expect: Projection -> SeqScan(LockModeS)
				proj, ok := root.(*ProjectionNode)
				if !ok {
					t.Fatalf("Expected ProjectionNode, got %T", root)
				}

				scan, ok := proj.Child.(*SeqScanNode)
				if !ok {
					t.Fatalf("Expected SeqScanNode, got %T", proj.Child)
				}

				if scan.Mode != transaction.LockModeS {
					t.Errorf("Expected LockModeS (Read), got %v", scan.Mode)
				}
			},
		},
		{
			name: "concurrency: delete (write lock)",
			sql:  "DELETE FROM users WHERE age > 50",
			validate: func(root PlanNode, t *testing.T) {
				// DeleteNode -> Filter -> SeqScan(LockModeX)
				del, ok := root.(*DeleteNode)
				if !ok {
					t.Fatalf("Expected DeleteNode, got %T", root)
				}

				filter, ok := del.Child.(*FilterNode)
				if !ok {
					t.Fatalf("Expected FilterNode under Delete, got %T", del.Child)
				}

				scan, ok := filter.Child.(*SeqScanNode)
				if !ok {
					t.Fatalf("Expected SeqScanNode under Filter, got %T", filter.Child)
				}

				if scan.Mode != transaction.LockModeX {
					t.Errorf("Expected LockModeX (Write), got %v", scan.Mode)
				}
			},
		},
		{
			name: "concurrency: delete (index write lock)",
			sql:  "DELETE FROM users WHERE id > 50",
			validate: func(root PlanNode, t *testing.T) {
				// DeleteNode -> Filter -> IndexScan(LockModeX)
				del, ok := root.(*DeleteNode)
				if !ok {
					t.Fatalf("Expected DeleteNode, got %T", root)
				}

				filter, ok := del.Child.(*FilterNode)
				if !ok {
					t.Fatalf("Expected FilterNode under Delete, got %T", del.Child)
				}

				scan, ok := filter.Child.(*IndexScanNode)
				if !ok {
					t.Fatalf("Expected IndexScanNode under Filter, got %T", filter.Child)
				}

				if !scan.ForUpdate {
					t.Error("Expected ForUpdate on index, got false")
				}
			},
		},
		{
			name: "concurrency: delete (index write lock)",
			sql:  "DELETE FROM users WHERE id = 50",
			validate: func(root PlanNode, t *testing.T) {
				// DeleteNode -> Filter -> IndexLookup(ForUpdate=True)
				del, ok := root.(*DeleteNode)
				if !ok {
					t.Fatalf("Expected DeleteNode, got %T", root)
				}

				filter, ok := del.Child.(*FilterNode)
				if !ok {
					t.Fatalf("Expected FilterNode under Delete, got %T", del.Child)
				}

				lookup, ok := filter.Child.(*IndexLookupNode)
				if !ok {
					t.Fatalf("Expected IndexScanNode under Filter, got %T", filter.Child)
				}

				if !lookup.ForUpdate {
					t.Error("Expected ForUpdate on index, got false")
				}
			},
		},
		{
			name: "concurrency: select (index read)",
			sql:  "SELECT * FROM users WHERE id = 50",
			validate: func(root PlanNode, t *testing.T) {
				// Projection -> Filter -> IndexLookup(ForUpdate=True)
				del, ok := root.(*ProjectionNode)
				if !ok {
					t.Fatalf("Expected ProjectionNode, got %T", root)
				}

				filter, ok := del.Child.(*FilterNode)
				if !ok {
					t.Fatalf("Expected FilterNode under Projection, got %T", del.Child)
				}

				lookup, ok := filter.Child.(*IndexLookupNode)
				if !ok {
					t.Fatalf("Expected IndexLookupNode under Filter, got %T", filter.Child)
				}

				if lookup.ForUpdate {
					t.Error("Expected not ForUpdate on index, got true")
				}
			},
		},
		{
			name: "concurrency: select (index read 2)",
			sql:  "SELECT * FROM users WHERE id > 50",
			validate: func(root PlanNode, t *testing.T) {
				// Projection -> Filter -> IndexScan(ForUpdate=True)
				del, ok := root.(*ProjectionNode)
				if !ok {
					t.Fatalf("Expected ProjectionNode, got %T", root)
				}

				filter, ok := del.Child.(*FilterNode)
				if !ok {
					t.Fatalf("Expected FilterNode under Projection, got %T", del.Child)
				}

				scan, ok := filter.Child.(*IndexScanNode)
				if !ok {
					t.Fatalf("Expected IndexScanNode under Filter, got %T", filter.Child)
				}

				if scan.ForUpdate {
					t.Error("Expected not ForUpdate on index, got true")
				}
			},
		},
		{
			name: "concurrency: update (write lock)",
			sql:  "UPDATE users SET age = 0 WHERE id = 1",
			validate: func(root PlanNode, t *testing.T) {
				// UpdateNode -> Filter -> IndexLookup(ForUpdate=True)
				// Note: Since id=1 is a PK look up, we expect index usage.
				upd, ok := root.(*UpdateNode)
				if !ok {
					t.Fatalf("Expected UpdateNode, got %T", root)
				}

				filter, ok := upd.Child.(*FilterNode)
				if !ok {
					t.Fatalf("Expected FilterNode under Update, got %T", upd.Child)
				}

				lookup, ok := filter.Child.(*IndexLookupNode)
				if !ok {
					t.Fatalf("Expected IndexLookupNode under Filter, got %T", filter.Child)
				}

				if !lookup.ForUpdate {
					t.Error("Expected ForUpdate on index, got false")
				}
			},
		},
		{
			name: "concurrency: insert select (read lock on source)",
			sql:  "INSERT INTO users_new SELECT * FROM users",
			validate: func(root PlanNode, t *testing.T) {
				// InsertNode -> Projection -> SeqScan(LockModeS)
				ins, ok := root.(*InsertNode)
				if !ok {
					t.Fatalf("Expected InsertNode, got %T", root)
				}

				proj, ok := ins.Child.(*ProjectionNode)
				if !ok {
					t.Fatalf("Expected ProjectionNode under Insert, got %T", ins.Child)
				}

				scan, ok := proj.Child.(*SeqScanNode)
				if !ok {
					t.Fatalf("Expected SeqScanNode under Projection, got %T", proj.Child)
				}

				// The scan on the *source* table must be Read-Only (IS),
				// even though the parent Insert is modifying the DB.
				if scan.Mode != transaction.LockModeS {
					t.Errorf("Expected Source Scan to be S (Read), got %v", scan.Mode)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stmt, err := sqlparser.Parse(tt.sql)
			if err != nil {
				t.Fatalf("SQL Parse Error: %v", err)
			}

			logicalPlan, err := logicalBuilder.Plan(stmt)
			if err != nil {
				t.Fatalf("Logical Plan Error: %v", err)
			}

			optPlan := optimizer.Optimize(logicalPlan)

			physPlan, err := physicalBuilder.Build(optPlan)
			if err != nil {
				t.Fatalf("Physical Plan Error: %v", err)
			}

			tt.validate(physPlan, t)
		})
	}
}
