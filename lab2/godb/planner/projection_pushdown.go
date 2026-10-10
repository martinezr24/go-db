package planner

type ProjectionPushDownRule struct{}

func (r *ProjectionPushDownRule) Name() string { return "ProjectionPushDown" }

func (r *ProjectionPushDownRule) Match(node LogicalPlanNode) bool {
	return true
}

func (r *ProjectionPushDownRule) Apply(node LogicalPlanNode) LogicalPlanNode {
	myReqs := node.GetRequiredSchema()

	r.propagateToChildren(node, myReqs)

	if proj, isProj := node.(*LogicalProjectionNode); isProj {
		if len(proj.OutputSchema()) > len(myReqs) {

			// Filter expressions to only those needed by myReqs
			newExprs := make([]Expr, 0, len(myReqs))
			newSchema := make(LogicalSchema, 0, len(myReqs))

			for _, reqCol := range myReqs {
				for i, outCol := range proj.OutputSchema() {
					if outCol.Equals(reqCol) {
						newExprs = append(newExprs, proj.Expressions[i])
						newSchema = append(newSchema, outCol)
						break
					}
				}
			}

			return NewLogicalProjectionNode(proj.Child, newExprs, newSchema)
		}

		return node
	}

	return node
}

/*
ProjectionPushDownRule.PostProcess does projection wrapping or cleans up redundant projections
*/
func (r *ProjectionPushDownRule) PostProcess(node LogicalPlanNode) LogicalPlanNode {
	myReqs := node.GetRequiredSchema()
	if len(myReqs) == 0 {
		myReqs = node.OutputSchema()
	}

	if proj, ok := node.(*LogicalProjectionNode); ok {
		if childProj, isChildProj := proj.Child.(*LogicalProjectionNode); isChildProj {

			if childProj.OutputSchema().Covers(myReqs) && childProj.OutputSchema().Covers(proj.OutputSchema()) {
				proj.Child = childProj.Child
				return proj
			} else {
				panic("Error in logical query optimization - ProjectionPushDownRule PostProcess: child projection does not cover required/output schema of parent projection.")
			}
		}
		if childScan, isChildScan := proj.Child.(*LogicalScanNode); isChildScan {
			// Attach projection node to the scan node for more idiomatic optimizations during the
			// physicalization step.
			childScan.AddProjection(proj)
			return childScan
		}
		return node
	}

	output := node.OutputSchema()

	if len(output) <= len(myReqs) {
		return node
	}

	switch node.(type) {
	case *LogicalScanNode, *LogicalAggregationNode, *LogicalJoinNode:
		projections := myReqs.GetExprs()
		newNode := NewLogicalProjectionNode(node, projections, myReqs)
		newNode.SetRequiredSchema(myReqs)
		return r.PostProcess(newNode)
	}

	return node
}

func (r *ProjectionPushDownRule) propagateToChildren(parent LogicalPlanNode, parentReqs LogicalSchema) {
	internalDeps := parent.Dependencies()

	switch n := parent.(type) {

	// These nodes ignore the schema they are required to output by their parents
	// for the child's contract.
	case *LogicalProjectionNode:
		n.Child.SetRequiredSchema(uniqueColumns(internalDeps))

	case *LogicalAggregationNode:
		n.Child.SetRequiredSchema(uniqueColumns(internalDeps))

	// These nodes pass down the schema they are required to output by their parents,
	// adding their own dependencies.
	case *LogicalFilterNode:
		reqs := append(parentReqs, internalDeps...)
		n.Child.SetRequiredSchema(uniqueColumns(reqs))
	case *LogicalSortNode:
		reqs := append(parentReqs, internalDeps...)
		n.Child.SetRequiredSchema(uniqueColumns(reqs))

		// These nodes only pass down the schema they are required to output by their parents
	case *LogicalLimitNode:
		n.Child.SetRequiredSchema(parentReqs)

	// Join is a special case since it has two children.
	case *LogicalJoinNode:
		allReqs := append(parentReqs, internalDeps...)
		distinctReqs := uniqueColumns(allReqs)

		leftReqs, rightReqs := r.splitRequirements(n, distinctReqs)

		n.Left.SetRequiredSchema(leftReqs)
		n.Right.SetRequiredSchema(rightReqs)
	}
}

// Splits columns based on which side of the join they come from
func (r *ProjectionPushDownRule) splitRequirements(join *LogicalJoinNode, reqs []*LogicalColumn) ([]*LogicalColumn, []*LogicalColumn) {
	var left, right []*LogicalColumn

	leftSchema := join.Left.OutputSchema()
	rightSchema := join.Right.OutputSchema()

	for _, req := range reqs {
		if leftSchema.Contains(req) {
			left = append(left, req)
		} else if rightSchema.Contains(req) {
			right = append(right, req)
		}
	}
	return left, right
}

func uniqueColumns(cols []*LogicalColumn) []*LogicalColumn {
	seen := make(map[string]bool)
	var result []*LogicalColumn
	for _, c := range cols {
		// key: TableAlias.ColName
		key := c.String()
		if !seen[key] {
			seen[key] = true
			result = append(result, c)
		}
	}
	return result
}
