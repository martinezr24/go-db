package planner

import (
	"fmt"

	"mit.edu/dsg/godb/catalog"
	"mit.edu/dsg/godb/common"
)

/*
isEquiJoin checks if an expression is a valid equality condition between the two schemas.
Returns:
1. The expression from the Left schema
2. The expression from the Right schema
3. True if it is a valid equi-join condition
*/
func isEquiJoin(expr Expr, leftSchema, rightSchema LogicalSchema) ([]Expr, []Expr, bool) {
	switch cmp := expr.(type) {
	case *ComparisonExpression:
		if cmp.compType != Equal {
			return nil, nil, false
		}
		leftExpr := cmp.left
		rightExpr := cmp.right

		if leftSchema.CoversExpr(leftExpr) && rightSchema.CoversExpr(rightExpr) {
			return []Expr{leftExpr}, []Expr{rightExpr}, true
		}

		if leftSchema.CoversExpr(rightExpr) && rightSchema.CoversExpr(leftExpr) {
			return []Expr{rightExpr}, []Expr{leftExpr}, true
		}
	case *BinaryLogicExpression:
		if cmp.logicType != And {
			return nil, nil, false
		}
		resultLKeys := make([]Expr, 0)
		resultRKeys := make([]Expr, 0)
		// Recursively check both sides of the AND for equi-join conditions.
		if lKey, rKey, ok := isEquiJoin(cmp.left, leftSchema, rightSchema); ok {
			resultLKeys = append(resultLKeys, lKey...)
			resultRKeys = append(resultRKeys, rKey...)
		}
		if lKey, rKey, ok := isEquiJoin(cmp.right, leftSchema, rightSchema); ok {
			resultLKeys = append(resultLKeys, lKey...)
			resultRKeys = append(resultRKeys, rKey...)
		}
		if len(resultLKeys) > 0 && len(resultRKeys) > 0 {
			return resultLKeys, resultRKeys, true
		}
	}
	return nil, nil, false
}

type JoinKeyPair struct {
	Left         Expr // LogicalColumn
	Right        Expr // LogicalColumn
	OriginalExpr Expr // ComparisonExpression (Logical)
}

func getEquiJoinKeys(n *LogicalJoinNode) []JoinKeyPair {
	var pairs []JoinKeyPair
	for _, cond := range n.joinOn {
		if lKeys, rKeys, ok := isEquiJoin(cond, n.Left.OutputSchema(), n.Right.OutputSchema()); ok {
			for i := range lKeys {
				pairs = append(pairs, JoinKeyPair{
					Left:         lKeys[i],
					Right:        rKeys[i],
					OriginalExpr: cond,
				})
			}
		}
	}
	return pairs
}

func getNonEquiConditions(n *LogicalJoinNode, equiKeys []JoinKeyPair) []Expr {
	var others []Expr

	isEqui := make(map[Expr]bool)
	for _, k := range equiKeys {
		isEqui[k.OriginalExpr] = true
	}

	for _, cond := range n.joinOn {
		if !isEqui[cond] {
			others = append(others, cond)
		}
	}
	return others
}

type JoinIndexMatch struct {
	NumMatchedColumns int
	ProbeKeys         []Expr // The left-side expressions in JoinOns use as lookup keys in the indexed join.
	MatchedJoinOns    []Expr // The JoinOn expressions used in the indexed join.
}

func analyzeJoinIndex(idx *catalog.Index, equiKeys []JoinKeyPair) *JoinIndexMatch {
	match := &JoinIndexMatch{
		ProbeKeys:      make([]Expr, 0),
		MatchedJoinOns: make([]Expr, 0),
	}

	for _, idxCol := range idx.KeySchema {
		found := false

		for _, pair := range equiKeys {
			if rightCol, ok := pair.Right.(*LogicalColumn); ok {
				if rightCol.cname == idxCol {
					match.NumMatchedColumns++
					match.ProbeKeys = append(match.ProbeKeys, pair.Left)
					match.MatchedJoinOns = append(match.MatchedJoinOns, pair.OriginalExpr)
					found = true
					break
				}
			}
		}

		if !found {
			break
		}
	}

	return match
}

type IndexNestedLoopJoinRule struct{}

func (r *IndexNestedLoopJoinRule) Name() string  { return "IndexNestedLoopJoin" }
func (r *IndexNestedLoopJoinRule) Priority() int { return 2 }

// TODO: Handle symmetry (index on the left side)
func (r *IndexNestedLoopJoinRule) Match(node LogicalPlanNode, children []PlanNode, c *catalog.Catalog) bool {
	join, ok := node.(*LogicalJoinNode)
	if !ok {
		return false
	}

	equiKeys := getEquiJoinKeys(join)
	if len(equiKeys) == 0 {
		return false
	}

	rightScan, ok := join.Right.(*LogicalScanNode)
	if !ok {
		return false
	}

	table, err := c.GetTableByOid(rightScan.GetTableOid())
	if err != nil {
		return false
	}

	// Check whether the first column of any index matches any join key on the right side.
	for _, idx := range table.Indexes {
		for _, pair := range equiKeys {
			if rightCol, ok := pair.Right.(*LogicalColumn); ok {
				if rightCol.cname == idx.KeySchema[0] {
					return true
				}
			}
		}
	}

	return false
}

func (r *IndexNestedLoopJoinRule) Apply(node LogicalPlanNode, children []PlanNode, c *catalog.Catalog, exprBinder *ExpressionBinder) (PlanNode, error) {
	common.Assert(len(children) == 2, "IndexedNestedLoopJoin must have 2 children.")
	join := node.(*LogicalJoinNode)
	rightScan := join.Right.(*LogicalScanNode)
	leftPlan := children[0]

	equiKeys := getEquiJoinKeys(join)
	table, _ := c.GetTableByOid(rightScan.GetTableOid())

	var bestIdx *catalog.Index
	var bestMatch *JoinIndexMatch

	for _, idx := range table.Indexes {
		currentIdx := idx
		match := analyzeJoinIndex(&currentIdx, equiKeys)

		// TODO: Support partial index match (e.g., index on (a, b, c) but only 'a' is matched). For now, we require all keys to be used during lookup.
		if match.NumMatchedColumns == len(idx.KeySchema) {
			if bestMatch == nil {
				bestMatch = match
				bestIdx = &currentIdx
			}
		}
	}

	if bestIdx == nil {
		return nil, fmt.Errorf("IndexNestedLoopJoinRule: matched but failed to find index in Apply")
	}

	// physicalize probe keys
	physProbeKeys := make([]Expr, len(bestMatch.ProbeKeys))
	for i, pk := range bestMatch.ProbeKeys {
		physProbeKeys[i] = exprBinder.BindExpr(pk, join.Left.OutputSchema(), leftPlan.OutputSchema())
	}

	joinNode := NewIndexNestedLoopJoinNode(
		leftPlan,
		rightScan.GetTableOid(),
		bestIdx.Oid,
		physProbeKeys,
		tableSchemaToTypes(table),
		false,
	)

	// Conditions that were not used by the INLJ implemented by a filter
	usedSet := make(map[Expr]bool)
	for _, expr := range bestMatch.MatchedJoinOns {
		usedSet[expr] = true
	}

	var residuals []Expr
	for _, cond := range join.joinOn {
		if !usedSet[cond] {
			residuals = append(residuals, cond)
		}
	}

	// If right child involves a projection, push the projection back up.
	// Left side uses identity projections (can implement pass-through in the future)
	// Right side projections modified with additional offset
	if rightScan.projection != nil {
		// Start with left side identity projections
		fullProjections := getIdentityPhysicalExprs(join.Left.OutputSchema(), leftPlan.OutputSchema(), exprBinder)
		leftWidth := len(fullProjections)
		proj, projOk := children[1].(*ProjectionNode)
		common.Assert(projOk, "If logical Scan carries projection, physical scan must be wrapped in projection.")
		for _, e := range proj.Expressions {
			fullProjections = append(fullProjections, exprBinder.ShiftExpr(e, leftWidth))
		}
		projNode := NewProjectionNode(joinNode, fullProjections)
		if len(residuals) > 0 {
			fullLogicalSchema := append(join.Left.OutputSchema(), join.Right.OutputSchema()...)
			return wrapInFilter(projNode, residuals, fullLogicalSchema, exprBinder), nil
		}
		return projNode, nil
	}

	if len(residuals) > 0 {
		fullLogicalSchema := append(join.Left.OutputSchema(), join.Right.OutputSchema()...)
		return wrapInFilter(joinNode, residuals, fullLogicalSchema, exprBinder), nil
	}

	return joinNode, nil
}

type SortMergeJoinRule struct{}

func (r *SortMergeJoinRule) Name() string  { return "SortMergeJoin" }
func (r *SortMergeJoinRule) Priority() int { return 3 }

func (r *SortMergeJoinRule) Match(node LogicalPlanNode, children []PlanNode, catalog *catalog.Catalog) bool {
	join, ok := node.(*LogicalJoinNode)
	if !ok || len(getEquiJoinKeys(join)) == 0 {
		return false
	}

	// Optimization: Only match if BOTH children are IndexScans (already sorted).
	// This prevents us from picking a slow SMJ plan when a Hash Join would be better.
	// (Note: In a real DB, we would check if the underlying streams are sorted and potentially inspect whether we
	//  expect the output to be sorted)
	var smjOk = true
	switch c1 := children[0].(type) {
	case *IndexScanNode:
	case *ProjectionNode:
		// Allow a projection on top of an index scan
		if _, ok := c1.Child.(*IndexScanNode); !ok {
			smjOk = false
		}
	default:
		smjOk = false
	}
	switch c2 := children[1].(type) {
	case *IndexScanNode:
	case *ProjectionNode:
		// Allow a projection on top of an index scan
		if _, ok := c2.Child.(*IndexScanNode); !ok {
			smjOk = false
		}
	default:
		smjOk = false
	}

	return smjOk
}

func (r *SortMergeJoinRule) Apply(node LogicalPlanNode, children []PlanNode, catalog *catalog.Catalog, exprBinder *ExpressionBinder) (PlanNode, error) {
	join := node.(*LogicalJoinNode)
	leftPlan, rightPlan := children[0], children[1]

	keys := getEquiJoinKeys(join)

	physLeftKeys := make([]Expr, len(keys))
	physRightKeys := make([]Expr, len(keys))

	for i, k := range keys {
		physLeftKeys[i] = exprBinder.BindExpr(k.Left, join.Left.OutputSchema(), leftPlan.OutputSchema())
		physRightKeys[i] = exprBinder.BindExpr(k.Right, join.Right.OutputSchema(), rightPlan.OutputSchema())
	}

	smj := NewSortMergeJoinNode(leftPlan, rightPlan, physLeftKeys, physRightKeys)

	others := getNonEquiConditions(join, keys)
	if len(others) > 0 {
		fullLogicalSchema := append(join.Left.OutputSchema(), join.Right.OutputSchema()...)
		return wrapInFilter(smj, others, fullLogicalSchema, exprBinder), nil
	}

	return smj, nil
}

type HashJoinRule struct{}

func (r *HashJoinRule) Name() string  { return "HashJoin" }
func (r *HashJoinRule) Priority() int { return 1 }

func (r *HashJoinRule) Match(node LogicalPlanNode, children []PlanNode, catalog *catalog.Catalog) bool {
	join, ok := node.(*LogicalJoinNode)
	if !ok {
		return false
	}
	return len(getEquiJoinKeys(join)) > 0
}

func (r *HashJoinRule) Apply(node LogicalPlanNode, children []PlanNode, catalog *catalog.Catalog, exprBinder *ExpressionBinder) (PlanNode, error) {
	join := node.(*LogicalJoinNode)
	leftPlan, rightPlan := children[0], children[1]

	keys := getEquiJoinKeys(join)

	physLeftKeys := make([]Expr, len(keys))
	physRightKeys := make([]Expr, len(keys))

	for i, k := range keys {
		physLeftKeys[i] = exprBinder.BindExpr(k.Left, join.Left.OutputSchema(), leftPlan.OutputSchema())
		physRightKeys[i] = exprBinder.BindExpr(k.Right, join.Right.OutputSchema(), rightPlan.OutputSchema())
	}

	hjNode := NewHashJoinNode(leftPlan, rightPlan, physLeftKeys, physRightKeys)

	others := getNonEquiConditions(join, keys)
	if len(others) > 0 {
		fullSchema := append(join.Left.OutputSchema(), join.Right.OutputSchema()...)
		return wrapInFilter(hjNode, others, fullSchema, exprBinder), nil
	}

	return hjNode, nil
}

type BlockNestedLoopJoinRule struct{}

func (r *BlockNestedLoopJoinRule) Name() string  { return "BlockNestedLoopJoin" }
func (r *BlockNestedLoopJoinRule) Priority() int { return 0 }

func (r *BlockNestedLoopJoinRule) Match(node LogicalPlanNode, children []PlanNode, catalog *catalog.Catalog) bool {
	_, ok := node.(*LogicalJoinNode)
	return ok
}

func (r *BlockNestedLoopJoinRule) Apply(node LogicalPlanNode, children []PlanNode, catalog *catalog.Catalog, exprBinder *ExpressionBinder) (PlanNode, error) {
	join := node.(*LogicalJoinNode)
	leftPlan, rightPlan := children[0], children[1]

	switch rightPlan.(type) {
	case *SeqScanNode, *IndexScanNode, *IndexLookupNode, *MaterializeNode:
	default:
		rightPlan = NewMaterializeNode(rightPlan)
	}

	var finalPred Expr
	if len(join.joinOn) > 0 {
		finalPred = MergePredicates(join.joinOn)
	} else {
		finalPred = NewConstantValueExpression(common.NewIntValue(1)) // Cross Join
	}

	fullLogical := append(join.Left.OutputSchema(), join.Right.OutputSchema()...)
	fullPhysical := append(leftPlan.OutputSchema(), rightPlan.OutputSchema()...)

	physPred := exprBinder.BindExpr(finalPred, fullLogical, fullPhysical)

	return NewBlockNestedLoopJoinNode(leftPlan, rightPlan, physPred), nil
}
