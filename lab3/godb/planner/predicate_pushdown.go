package planner

type PredicatePushDownRule struct{}

func (r *PredicatePushDownRule) Name() string { return "PredicatePushDown" }

func (r *PredicatePushDownRule) PostProcess(node LogicalPlanNode) LogicalPlanNode {
	return node
}

func (r *PredicatePushDownRule) Match(node LogicalPlanNode) bool {
	_, ok := node.(*LogicalFilterNode)
	return ok
}

func (r *PredicatePushDownRule) Apply(node LogicalPlanNode) LogicalPlanNode {
	filter := node.(*LogicalFilterNode)
	child := filter.Children()[0]

	if scan, ok := child.(*LogicalScanNode); ok {
		if r.canPushToScan(filter.Predicate, scan) {
			scan.AddPredicate(filter.Predicate)
			return scan
		}
	}

	if join, ok := child.(*LogicalJoinNode); ok {
		return r.pushThroughJoin(filter, join)
	}

	// TODO: Push through Projection (requires mapping columns back to child schema)

	// TODO: Push through Subquery
	return node
}

// canPushToScan checks if the expression only references columns available in the scan
func (r *PredicatePushDownRule) canPushToScan(expr Expr, scan *LogicalScanNode) bool {
	cols := expr.GetReferencedColumns()
	for _, col := range cols {
		if col.origin == nil {
			return false // Derived column, e.g. aggregation
		}
		if col.origin.alias != scan.GetTableAlias() {
			return false // References a different table
		}
	}
	return true
}

// pushThroughJoin handles distributing predicates to the left/right children
// or merging them into the join condition based on the join type.
func (r *PredicatePushDownRule) pushThroughJoin(filter *LogicalFilterNode, join *LogicalJoinNode) LogicalPlanNode {

	predicates := SplitPredicate(filter.Predicate)

	var leftPush []Expr
	var rightPush []Expr
	var joinConds []Expr // To be merged into the join ON clause
	var keep []Expr      // Must remain in a Filter above the join

	for _, pred := range predicates {
		appliesLeft := join.Left.OutputSchema().CoversExpr(pred)
		appliesRight := join.Right.OutputSchema().CoversExpr(pred)

		switch join.joinType {
		case Inner:
			if appliesLeft && !appliesRight {
				leftPush = append(leftPush, pred)
			} else if appliesRight && !appliesLeft {
				rightPush = append(rightPush, pred)
			} else {
				// For Inner Joins, mixed predicates become Join Conditions
				joinConds = append(joinConds, pred)
			}

		case Left:
			// Can push to Left (Preserved side).
			if appliesLeft && !appliesRight {
				leftPush = append(leftPush, pred)
			} else {
				// Right-only or Mixed predicates must stay above to filter the result
				keep = append(keep, pred)
			}

		case Right:
			// Can push to Right (Preserved side).
			if appliesRight && !appliesLeft {
				rightPush = append(rightPush, pred)
			} else {
				keep = append(keep, pred)
			}

		case FullOuter:
			// Cannot push to either side safely without changing semantics.
			keep = append(keep, pred)

		case Cross:
			// Upgrade Cross to Inner if we find specific conditions
			if appliesLeft && !appliesRight {
				leftPush = append(leftPush, pred)
			} else if appliesRight && !appliesLeft {
				rightPush = append(rightPush, pred)
			} else {
				// Found a mixed predicate: Upgrade Cross -> Inner
				join.joinType = Inner
				joinConds = append(joinConds, pred)
			}
		}
	}

	// Push to Left
	if len(leftPush) > 0 {
		join.Left = NewLogicalFilterNode(join.Left, MergePredicates(leftPush))
	}

	// Push to Right
	if len(rightPush) > 0 {
		join.Right = NewLogicalFilterNode(join.Right, MergePredicates(rightPush))
	}

	// Merge into Join Conditions
	if len(joinConds) > 0 {
		join.joinOn = append(join.joinOn, joinConds...)
	}

	// If specific predicates must stay above, update the filter and return it.
	if len(keep) > 0 {
		filter.Predicate = MergePredicates(keep)
		filter.Child = join // Ensure filter points to the modified join
		return filter
	}

	// Otherwise, the filter is fully absorbed. Return the Join.
	return join
}
