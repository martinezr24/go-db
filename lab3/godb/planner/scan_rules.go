package planner

import (
	"fmt"

	"mit.edu/dsg/godb/catalog"
	"mit.edu/dsg/godb/common"
	"mit.edu/dsg/godb/indexing"
	"mit.edu/dsg/godb/storage"
	"mit.edu/dsg/godb/transaction"
)

type SeqScanRule struct{}

func (r *SeqScanRule) Name() string {
	return "SeqScanRule"
}

func (r *SeqScanRule) Match(node LogicalPlanNode, children []PlanNode, catalog *catalog.Catalog) bool {
	_, ok := node.(*LogicalScanNode)
	return ok
}

func (r *SeqScanRule) Priority() int {
	return 0
}

func (r *SeqScanRule) Apply(node LogicalPlanNode, children []PlanNode, c *catalog.Catalog, exprBinder *ExpressionBinder) (PlanNode, error) {
	scanNode := node.(*LogicalScanNode)
	table, err := c.GetTableByOid(scanNode.GetTableOid())
	if err != nil {
		return nil, err
	}
	lockMode := transaction.LockModeS
	if scanNode.ForUpdate {
		lockMode = transaction.LockModeX
	}
	physicalNode := NewSeqScanNode(scanNode.GetTableOid(), tableSchemaToTypes(table), lockMode)

	if scanNode.projection != nil {
		var plan PlanNode = physicalNode
		if len(scanNode.Predicates) > 0 {
			plan = wrapInFilter(plan, scanNode.Predicates, scanNode.RawOutputSchema(), exprBinder)
		}
		projectionRule := &ProjectionRule{}
		projNode, _ := projectionRule.Apply(scanNode.projection, []PlanNode{plan}, c, exprBinder)
		return projNode, nil
	}

	if len(scanNode.Predicates) > 0 {
		return wrapInFilter(physicalNode, scanNode.Predicates, scanNode.RawOutputSchema(), exprBinder), nil
	}
	return physicalNode, nil
}

func isEqualityScan(expr Expr, tableAlias string) (*LogicalColumn, common.Value, bool) {
	switch cmp := expr.(type) {
	case *ComparisonExpression:
		if cmp.compType != Equal {
			return nil, common.NewNullInt(), false
		} else {
			if col, ok := cmp.left.(*LogicalColumn); ok {
				if val, ok := cmp.right.(*ConstantValueExpr); ok {
					if col.origin.alias == tableAlias {
						return col, val.val, true
					}
				}
			}
			if col, ok := cmp.right.(*LogicalColumn); ok {
				if val, ok := cmp.left.(*ConstantValueExpr); ok {
					if col.origin.alias == tableAlias {
						return col, val.val, true
					}
				}
			}
		}
	}
	return nil, common.NewNullInt(), false
}

func isRangeScan(expr Expr, tableAlias string) (*LogicalColumn, common.Value, ComparisonType, bool) {
	switch cmp := expr.(type) {
	case *ComparisonExpression:
		if col, ok := cmp.left.(*LogicalColumn); ok {
			if val, ok := cmp.right.(*ConstantValueExpr); ok {
				if col.origin.alias == tableAlias {
					return col, val.val, cmp.compType, true
				}
			}
		}
		if col, ok := cmp.right.(*LogicalColumn); ok {
			if val, ok := cmp.left.(*ConstantValueExpr); ok {
				if col.origin.alias == tableAlias {
					flippedOp := flipComparison(cmp.compType)
					return col, val.val, flippedOp, true
				}
			}
		}
	case *NegationExpression:
		if col, val, op, ok := isRangeScan(cmp.child, tableAlias); ok {
			flippedOp := flipComparison(op)
			return col, val, flippedOp, true
		}
	}

	return nil, common.NewNullInt(), 0, false
}

func flipComparison(op ComparisonType) ComparisonType {
	switch op {
	case GreaterThan:
		return LessThan
	case GreaterThanOrEqual:
		return LessThanOrEqual
	case LessThan:
		return GreaterThan
	case LessThanOrEqual:
		return GreaterThanOrEqual
	}
	return op // Equal / NotEqual stay the same
}

func toDNFTerms(expr Expr) [][]Expr {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *BinaryLogicExpression:
		switch e.logicType {
		case Or:
			left := toDNFTerms(e.left)
			right := toDNFTerms(e.right)
			return append(left, right...)
		case And:
			left := toDNFTerms(e.left)
			right := toDNFTerms(e.right)
			if len(left) == 0 {
				return right
			}
			if len(right) == 0 {
				return left
			}
			terms := make([][]Expr, 0, len(left)*len(right))
			for _, l := range left {
				for _, r := range right {
					term := make([]Expr, 0, len(l)+len(r))
					term = append(term, l...)
					term = append(term, r...)
					terms = append(terms, term)
				}
			}
			return terms
		}
	case *NegationExpression:
		return negateToDNF(e.child)
	case *ComparisonExpression, *NullCheckExpression, *LikeExpression:
		return [][]Expr{{e}}
	default:
		panic(fmt.Sprintf("Unsupported expression type in toDNFTerms: %T", e))
	}

	return [][]Expr{{expr}}
}

func negateToDNF(expr Expr) [][]Expr {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *NegationExpression:
		return toDNFTerms(e.child)
	case *BinaryLogicExpression:
		switch e.logicType {
		case And:
			left := negateToDNF(e.left)
			right := negateToDNF(e.right)
			return append(left, right...)
		case Or:
			left := negateToDNF(e.left)
			right := negateToDNF(e.right)
			if len(left) == 0 {
				return right
			}
			if len(right) == 0 {
				return left
			}
			terms := make([][]Expr, 0, len(left)*len(right))
			for _, l := range left {
				for _, r := range right {
					term := make([]Expr, 0, len(l)+len(r))
					term = append(term, l...)
					term = append(term, r...)
					terms = append(terms, term)
				}
			}
			return terms
		}
	case *ComparisonExpression, *NullCheckExpression, *LikeExpression:
		return [][]Expr{{&NegationExpression{child: e}}}
	}
	return [][]Expr{{&NegationExpression{child: expr}}}
}

func predicatesToDNFTerms(predicates []Expr) [][]Expr {
	if len(predicates) == 0 {
		return nil
	}
	return toDNFTerms(MergePredicates(predicates))
}

// IndexMatchResult describes how well an index matches the scan predicates
type IndexMatchResult struct {
	NumMatchedColumns int
	Values            []common.Value
	IsRange           bool
	RangeOp           ComparisonType
	MatchedPreds      []Expr
}

/*
analyzeIndex checks if the given index can be used to satisfy the scan predicates.
Matches columns from left to right without gaps.
The predicates are logical expressions.
TODO: Normalize predicates. E.g. We don't want predicates like col = 5 AND col > 3 to prevent
confusion in index matching.
*/
func analyzeIndex(idx *catalog.Index, predicates []Expr, tableAlias string) *IndexMatchResult {
	terms := predicatesToDNFTerms(predicates)
	if len(terms) == 0 {
		return &IndexMatchResult{Values: make([]common.Value, 0), MatchedPreds: make([]Expr, 0)}
	}
	// For DNF with OR, pick the best single conjunctive term to drive the index.
	var best *IndexMatchResult
	for _, term := range terms {
		match := analyzeIndexTerm(idx, term, tableAlias)
		if best == nil || match.NumMatchedColumns > best.NumMatchedColumns {
			best = match
		}
	}
	if best == nil {
		return &IndexMatchResult{Values: make([]common.Value, 0), MatchedPreds: make([]Expr, 0)}
	}
	return best
}

/*
Every predicate in []Expr should be a simple logical expression:
- ComparisonExpression for equality or range scans (e.g., col = 5, col > 3)
- LikeExpression for pattern matching (e.g., col LIKE 'abc%')
- NullCheckExpression for IS NULL / IS NOT NULL (e.g., col IS NULL)

For b-tree indexes, we can see if the predicates can match the index columns in order.
For example, if the index is on (a, b, c), we first look for predicates on 'a',
then 'b', then 'c'. If we find a gap (e.g., no predicate on 'b' but there's one
on 'c'), we stop and only consider the matched columns up to that point. For now,
since partial matching is not supported yet, we require all ketys to be matched.

For hash indexes, we only look for equality predicates that match all index columns.
*/
func analyzeIndexTerm(idx *catalog.Index, predicates []Expr, tableAlias string) *IndexMatchResult {
	result := &IndexMatchResult{
		Values:       make([]common.Value, 0),
		MatchedPreds: make([]Expr, 0),
	}

	for _, colName := range idx.KeySchema {
		foundMatch := false

		// Check for equality scans first (e.g., col = 5).
		for _, pred := range predicates {
			if col, val, ok := isEqualityScan(pred, tableAlias); ok {
				if col.cname == colName {
					result.NumMatchedColumns++
					result.Values = append(result.Values, val)
					result.MatchedPreds = append(result.MatchedPreds, pred)
					foundMatch = true
					break
				}
			}
		}

		if foundMatch {
			continue
		}

		// If no equality, check for range scan (for the last matched column)
		for _, pred := range predicates {
			if col, val, op, ok := isRangeScan(pred, tableAlias); ok {
				if col.cname == colName {
					result.NumMatchedColumns++
					result.Values = append(result.Values, val)
					result.MatchedPreds = append(result.MatchedPreds, pred)
					result.IsRange = true
					result.RangeOp = op
					return result
				}
			}
		}

		// Assume we cannot skip columns in the index.
		break
	}

	// TODO: enable this when partial index matches are supported.
	// if idx.Type == "hash" && result.NumMatchedColumns != len(idx.KeySchema) {
	// For hash indexes, we require equality predicates on all index columns.
	if result.NumMatchedColumns < len(idx.KeySchema) {
		return &IndexMatchResult{
			Values:       make([]common.Value, 0),
			MatchedPreds: make([]Expr, 0),
		}
	}

	return result
}

// makeMultiColumnKey constructs a composite key from a list of values
func makeMultiColumnKey(values []common.Value, types []common.Type) (indexing.Key, error) {
	if len(values) != len(types) {
		return indexing.Key{}, fmt.Errorf("key construction mismatch: got %d values for %d types", len(values), len(types))
	}

	// Create a tuple desc for the key
	desc := storage.NewRawTupleDesc(types)
	buf := make([]byte, desc.BytesPerTuple())

	for i, val := range values {
		desc.SetValue(buf, i, val)
	}
	return indexing.NewKey(buf, desc), nil
}

func applyIndexResidualsAndProjection(physicalNode PlanNode, scanNode *LogicalScanNode, match *IndexMatchResult, c *catalog.Catalog, exprBinder *ExpressionBinder) (PlanNode, error) {
	// Conditions that were not used by the Index Scan are implemented by a filter
	usedSet := make(map[Expr]bool)
	for _, expr := range match.MatchedPreds {
		usedSet[expr] = true
	}

	dnfPredicates := predicatesToDNFTerms(scanNode.Predicates)
	newDnfPredicates := make([][]Expr, 0, len(dnfPredicates))

	// If one DNF term is fully satisfied by the index (i.e., no residuals),
	// the entire DNF is satisfied, so no filter is needed.
	var hasFullyMatchedTerm bool
	for _, term := range dnfPredicates {
		var residuals []Expr
		for _, pred := range term {
			if !usedSet[pred] {
				residuals = append(residuals, pred)
			}
		}
		if len(residuals) == 0 {
			// This term is fully matched by the index, so the entire OR is satisfied
			hasFullyMatchedTerm = true
			break
		}
		newDnfPredicates = append(newDnfPredicates, residuals)
	}

	// If one complete DNF term was satisfied by the index, no filter needed at all
	var residuals []Expr
	if !hasFullyMatchedTerm {
		// Merge residual DNF predicates back into a single expression tree for the filter node.
		// Convert each residual term (conjunction) into an OR of conjunctions.
		for _, term := range newDnfPredicates {
			residuals = append(residuals, MergePredicates(term))
		}
	}

	if scanNode.projection != nil {
		var plan PlanNode = physicalNode
		if len(residuals) > 0 {
			plan = wrapInFilter(plan, residuals, scanNode.RawOutputSchema(), exprBinder)
		}
		projectionRule := &ProjectionRule{}
		projNode, _ := projectionRule.Apply(scanNode.projection, []PlanNode{plan}, c, exprBinder)
		return projNode, nil
	}

	if len(residuals) > 0 {
		return wrapInFilter(physicalNode, residuals, scanNode.RawOutputSchema(), exprBinder), nil
	}
	return physicalNode, nil
}

type IndexScanRule struct{}

func (r *IndexScanRule) Name() string { return "IndexScanRule" }

func (r *IndexScanRule) Priority() int { return 1 }

func (r *IndexScanRule) Match(node LogicalPlanNode, children []PlanNode, c *catalog.Catalog) bool {
	scan, ok := node.(*LogicalScanNode)
	if !ok || len(scan.Predicates) == 0 {
		return false
	}

	table, err := c.GetTableByOid(scan.GetTableOid())
	if err != nil {
		return false
	}

	for _, idx := range table.Indexes {
		match := analyzeIndex(&idx, scan.Predicates, scan.GetTableAlias())
		if match.NumMatchedColumns == len(idx.KeySchema) && match.IsRange {
			return true
		}
	}
	return false
}

func (r *IndexScanRule) Apply(node LogicalPlanNode, children []PlanNode, c *catalog.Catalog, exprBinder *ExpressionBinder) (PlanNode, error) {
	scanNode := node.(*LogicalScanNode)
	table, _ := c.GetTableByOid(scanNode.GetTableOid())

	var bestIdx *catalog.Index
	var bestMatch *IndexMatchResult

	for _, idx := range table.Indexes {
		currentIdx := idx
		match := analyzeIndex(&currentIdx, scanNode.Predicates, scanNode.GetTableAlias())

		if match.NumMatchedColumns > 0 && match.IsRange {
			if bestMatch == nil || match.NumMatchedColumns > bestMatch.NumMatchedColumns {
				bestMatch = match
				bestIdx = &currentIdx
			}
		}
	}

	if bestIdx == nil {
		return nil, fmt.Errorf("IndexScanRule matched but failed to find index during Apply")
	}

	direction := indexing.ScanDirectionForward
	op := bestMatch.RangeOp
	if op == LessThan || op == LessThanOrEqual {
		direction = indexing.ScanDirectionBackward
	}
	if op == LessThan {
		// For exclusive range, we need to adjust the key to be the predecessor of the given value.
		lastVal := bestMatch.Values[len(bestMatch.Values)-1]
		adjustedVal := lastVal.Decrement()
		bestMatch.Values[len(bestMatch.Values)-1] = adjustedVal
	} else if op == GreaterThan {
		// For exclusive range, we need to adjust the key to be the successor of the given value.
		lastVal := bestMatch.Values[len(bestMatch.Values)-1]
		adjustedVal := lastVal.Increment()
		bestMatch.Values[len(bestMatch.Values)-1] = adjustedVal
	}

	keyTypes := make([]common.Type, bestMatch.NumMatchedColumns)
	for i := 0; i < bestMatch.NumMatchedColumns; i++ {
		colName := bestIdx.KeySchema[i]
		for _, colDef := range table.Columns {
			if colDef.Name == colName {
				keyTypes[i] = colDef.Type
				break
			}
		}
	}
	key, err := makeMultiColumnKey(bestMatch.Values, keyTypes)
	if err != nil {
		return nil, err
	}

	physicalNode := NewIndexScanNode(
		bestIdx.Oid,
		scanNode.GetTableOid(),
		tableSchemaToTypes(table),
		direction,
		key,
		scanNode.ForUpdate,
	)

	return applyIndexResidualsAndProjection(physicalNode, scanNode, bestMatch, c, exprBinder)
}

type IndexLookupRule struct{}

func (r *IndexLookupRule) Name() string {
	return "IndexLookupRule"
}

func (r *IndexLookupRule) Match(node LogicalPlanNode, children []PlanNode, c *catalog.Catalog) bool {
	scan, ok := node.(*LogicalScanNode)
	if !ok || len(scan.Predicates) == 0 {
		return false
	}

	table, err := c.GetTableByOid(scan.GetTableOid())
	if err != nil {
		return false
	}

	// Look for any index that supports a pure equality lookup
	for _, idx := range table.Indexes {
		match := analyzeIndex(&idx, scan.Predicates, scan.GetTableAlias())
		if match.NumMatchedColumns > 0 && !match.IsRange {
			return true
		}
	}
	return false
}

func (r *IndexLookupRule) Priority() int {
	return 2
}

func (r *IndexLookupRule) Apply(node LogicalPlanNode, children []PlanNode, c *catalog.Catalog, exprBinder *ExpressionBinder) (PlanNode, error) {
	scanNode := node.(*LogicalScanNode)
	table, _ := c.GetTableByOid(scanNode.GetTableOid())

	var bestIdx *catalog.Index
	var bestMatch *IndexMatchResult

	for _, idx := range table.Indexes {
		currentIdx := idx
		match := analyzeIndex(&currentIdx, scanNode.Predicates, scanNode.GetTableAlias())

		if match.NumMatchedColumns > 0 && !match.IsRange {
			if bestMatch == nil || match.NumMatchedColumns > bestMatch.NumMatchedColumns {
				bestMatch = match
				bestIdx = &currentIdx
			}
		}
	}

	if bestIdx == nil {
		return nil, fmt.Errorf("IndexLookupRule matched but failed to find index during Apply")
	}

	keyTypes := make([]common.Type, bestMatch.NumMatchedColumns)
	for i := 0; i < bestMatch.NumMatchedColumns; i++ {
		// Find the column definition in the table to get its Type
		colName := bestIdx.KeySchema[i]
		for _, colDef := range table.Columns {
			if colDef.Name == colName {
				keyTypes[i] = colDef.Type
				break
			}
		}
	}
	key, err := makeMultiColumnKey(bestMatch.Values, keyTypes)
	if err != nil {
		return nil, err
	}

	physicalNode := NewIndexLookupNode(
		bestIdx.Oid,
		scanNode.GetTableOid(),
		tableSchemaToTypes(table),
		key,
		scanNode.ForUpdate,
	)

	return applyIndexResidualsAndProjection(physicalNode, scanNode, bestMatch, c, exprBinder)
}
