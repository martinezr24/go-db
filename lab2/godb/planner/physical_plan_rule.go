package planner

import (
	"fmt"

	"mit.edu/dsg/godb/catalog"
	"mit.edu/dsg/godb/common"
)

type PhysicalConversionRule interface {
	Name() string

	Match(node LogicalPlanNode, children []PlanNode, catalog *catalog.Catalog) bool

	Apply(node LogicalPlanNode, children []PlanNode, catalog *catalog.Catalog, exprBinder *ExpressionBinder) (PlanNode, error)

	// Handles the case where there are multiple matches.
	Priority() int
}

func tableSchemaToTypes(t *catalog.Table) []common.Type {
	types := make([]common.Type, len(t.Columns))
	for i, c := range t.Columns {
		types[i] = c.Type
	}
	return types
}

func wrapInFilter(child PlanNode, preds []Expr, logicalInputSchema LogicalSchema, exprBinder *ExpressionBinder) PlanNode {
	if len(preds) == 0 {
		return child
	}
	finalPred := preds[0]
	for i := 1; i < len(preds); i++ {
		finalPred = NewBinaryLogicExpression(finalPred, preds[i], And)
	}
	physPred := exprBinder.BindExpr(finalPred, logicalInputSchema, child.OutputSchema())
	return NewFilterNode(child, physPred)
}

/*
TODO: Passthrough
*/
func getIdentityPhysicalExprs(logicalSchema LogicalSchema, physicalInputSchema []common.Type, exprBinder *ExpressionBinder) []Expr {

	physExprs := make([]Expr, len(logicalSchema))
	for i, expr := range logicalSchema {
		physExprs[i] = exprBinder.BindExpr(expr, logicalSchema, physicalInputSchema)
	}

	return physExprs
}

type LimitRule struct{}

func (r *LimitRule) Name() string {
	return "LimitRule"
}

func (r *LimitRule) Match(node LogicalPlanNode, children []PlanNode, catalog *catalog.Catalog) bool {
	_, ok := node.(*LogicalLimitNode)
	return ok
}

func (r *LimitRule) Priority() int {
	return 0
}

func (r *LimitRule) Apply(node LogicalPlanNode, children []PlanNode, catalog *catalog.Catalog, exprBinder *ExpressionBinder) (PlanNode, error) {
	limitNode := node.(*LogicalLimitNode)
	childPlan := children[0]
	if sortNode, ok := childPlan.(*SortNode); ok {
		return NewTopNNode(sortNode.Child, limitNode.Limit, sortNode.OrderBy), nil
	}
	return NewLimitNode(childPlan, limitNode.Limit), nil
}

type SortRule struct{}

func (r *SortRule) Name() string  { return "SortRule" }
func (r *SortRule) Priority() int { return 0 }

func (r *SortRule) Match(node LogicalPlanNode, children []PlanNode, catalog *catalog.Catalog) bool {
	_, ok := node.(*LogicalSortNode)
	return ok
}

func (r *SortRule) Apply(node LogicalPlanNode, children []PlanNode, catalog *catalog.Catalog, exprBinder *ExpressionBinder) (PlanNode, error) {
	sort := node.(*LogicalSortNode)
	childPlan := children[0]

	// physicalize OrderBy expressions
	physOrderBy := make([]OrderByClause, len(sort.OrderBy))
	for i, order := range sort.OrderBy {
		physOrderBy[i] = OrderByClause{
			Expr:      exprBinder.BindExpr(order.Expr, sort.Child.OutputSchema(), childPlan.OutputSchema()),
			Direction: order.Direction,
		}
	}

	return NewSortNode(childPlan, physOrderBy), nil
}

type FilterRule struct{}

func (r *FilterRule) Name() string  { return "FilterRule" }
func (r *FilterRule) Priority() int { return 0 }

func (r *FilterRule) Match(node LogicalPlanNode, children []PlanNode, catalog *catalog.Catalog) bool {
	_, ok := node.(*LogicalFilterNode)
	return ok
}

func (r *FilterRule) Apply(node LogicalPlanNode, children []PlanNode, catalog *catalog.Catalog, exprBinder *ExpressionBinder) (PlanNode, error) {
	filter := node.(*LogicalFilterNode)
	childPlan := children[0]

	// physicalize the predicate expression
	// Note: Use the CHILD's schema to resolve columns
	physPred := exprBinder.BindExpr(filter.Predicate, filter.Child.OutputSchema(), childPlan.OutputSchema())

	return NewFilterNode(childPlan, physPred), nil
}

type ProjectionRule struct{}

func (r *ProjectionRule) Name() string  { return "ProjectionRule" }
func (r *ProjectionRule) Priority() int { return 0 }

func (r *ProjectionRule) Match(node LogicalPlanNode, children []PlanNode, catalog *catalog.Catalog) bool {
	_, ok := node.(*LogicalProjectionNode)
	return ok
}

func (r *ProjectionRule) Apply(node LogicalPlanNode, children []PlanNode, catalog *catalog.Catalog, exprBinder *ExpressionBinder) (PlanNode, error) {
	proj := node.(*LogicalProjectionNode)
	childPlan := children[0]

	physExprs := make([]Expr, len(proj.Expressions))

	if childProj, ok := childPlan.(*ProjectionNode); ok {
		// If my child is already a projection, we can fuse my projection expressions with my child's projection expressions.
		// This allows us to avoid creating back-to-back projection nodes in the physical plan, which can be inefficient.
		var projectionFused bool = true
		for i, expr := range proj.Expressions {
			fusedExpr, err := exprBinder.FuseProjectionIntoExpr(expr, proj.Child.OutputSchema(), childProj.Expressions)
			if err != nil {
				fmt.Printf("Failed to fuse projection expression: %v. Falling back to separate projection node.\n", err)
				projectionFused = false
				break
			}
			physExprs[i] = fusedExpr
		}
		if projectionFused {
			return NewProjectionNode(childProj.Child, physExprs), nil
		}
	}

	logicalInputSchema := proj.Child.OutputSchema()
	if scan, ok := proj.Child.(*LogicalScanNode); ok {
		logicalInputSchema = scan.RawOutputSchema()
	}
	for i, expr := range proj.Expressions {
		physExprs[i] = exprBinder.BindExpr(expr, logicalInputSchema, childPlan.OutputSchema())
	}

	return NewProjectionNode(childPlan, physExprs), nil
}

type AggregationRule struct{}

func (r *AggregationRule) Name() string  { return "AggregationRule" }
func (r *AggregationRule) Priority() int { return 0 }

func (r *AggregationRule) Match(node LogicalPlanNode, children []PlanNode, catalog *catalog.Catalog) bool {
	_, ok := node.(*LogicalAggregationNode)
	return ok
}

func (r *AggregationRule) Apply(node LogicalPlanNode, children []PlanNode, catalog *catalog.Catalog, exprBinder *ExpressionBinder) (PlanNode, error) {
	agg := node.(*LogicalAggregationNode)
	childPlan := children[0]

	physGroupBy := make([]Expr, len(agg.GroupByClause))
	for i, expr := range agg.GroupByClause {
		physGroupBy[i] = exprBinder.BindExpr(expr, agg.Child.OutputSchema(), childPlan.OutputSchema())
	}

	physAggs := make([]AggregateClause, len(agg.AggClauses))
	for i, clause := range agg.AggClauses {
		physAggs[i] = AggregateClause{
			Type: clause.Type,
			Expr: exprBinder.BindExpr(clause.Expr, agg.Child.OutputSchema(), childPlan.OutputSchema()),
		}
	}

	return NewAggregateNode(childPlan, physGroupBy, physAggs), nil
}

type SubqueryRule struct{}

func (r *SubqueryRule) Name() string  { return "SubqueryRule" }
func (r *SubqueryRule) Priority() int { return 0 }

func (r *SubqueryRule) Match(node LogicalPlanNode, children []PlanNode, catalog *catalog.Catalog) bool {
	_, ok := node.(*LogicalSubqueryNode)
	return ok
}

func (r *SubqueryRule) Apply(node LogicalPlanNode, children []PlanNode, catalog *catalog.Catalog, exprBinder *ExpressionBinder) (PlanNode, error) {
	// A LogicalSubqueryNode (in the FROM clause) is structurally just a wrapper.
	// The physical plan for the subquery is already built in children[0].
	// We simply return it.
	return NewMaterializeNode(children[0]), nil
}

type InsertRule struct{}

func (r *InsertRule) Name() string  { return "InsertRule" }
func (r *InsertRule) Priority() int { return 0 }

func (r *InsertRule) Match(node LogicalPlanNode, children []PlanNode, catalog *catalog.Catalog) bool {
	_, ok := node.(*LogicalInsertNode)
	return ok
}

func (r *InsertRule) Apply(node LogicalPlanNode, children []PlanNode, catalog *catalog.Catalog, exprBinder *ExpressionBinder) (PlanNode, error) {
	insert := node.(*LogicalInsertNode)
	return NewInsertNode(insert.TableOid, children[0]), nil
}

type DeleteRule struct{}

func (r *DeleteRule) Name() string  { return "DeleteRule" }
func (r *DeleteRule) Priority() int { return 0 }

func (r *DeleteRule) Match(node LogicalPlanNode, children []PlanNode, catalog *catalog.Catalog) bool {
	_, ok := node.(*LogicalDeleteNode)
	return ok
}

func (r *DeleteRule) Apply(node LogicalPlanNode, children []PlanNode, catalog *catalog.Catalog, exprBinder *ExpressionBinder) (PlanNode, error) {
	del := node.(*LogicalDeleteNode)
	return NewDeleteNode(del.TableOid, children[0]), nil
}

type UpdateRule struct{}

func (r *UpdateRule) Name() string  { return "UpdateRule" }
func (r *UpdateRule) Priority() int { return 0 }

func (r *UpdateRule) Match(node LogicalPlanNode, children []PlanNode, c *catalog.Catalog) bool {
	_, ok := node.(*LogicalUpdateNode)
	return ok
}

func (r *UpdateRule) Apply(node LogicalPlanNode, children []PlanNode, c *catalog.Catalog, exprBinder *ExpressionBinder) (PlanNode, error) {
	update := node.(*LogicalUpdateNode)
	physicalChild := children[0]

	table, err := c.GetTableByOid(update.TableOid)
	if err != nil {
		return nil, err
	}

	// Convert Map: LogicalColumn -> Expr  ==TO==>  ColIndex -> PhysicalExpr
	physUpdates := make([]Expr, len(table.Columns))
	for colIdx, targetCol := range table.Columns {
		// lookup the LogicalColumn for this column name in the update's set clause
		var logicalCol *LogicalColumn
		for col := range update.Updates {
			if col.cname == targetCol.Name {
				logicalCol = col
				break
			}
		}

		if logicalCol == nil {
			// apply identity projection for columns not being updated
			physUpdates[colIdx] = &BoundValueExpr{
				fieldOffset: colIdx,
				outputType:  targetCol.Type,
				name:        targetCol.Name,
			}
			continue
		}

		// get the corresponding update expression for this column
		updateExpr := update.Updates[logicalCol]
		physUpdates[colIdx] = exprBinder.BindExpr(updateExpr, update.Child.OutputSchema(), physicalChild.OutputSchema())

	}

	return NewUpdateNode(update.TableOid, physicalChild, physUpdates), nil
}

type ValuesRule struct{}

func (r *ValuesRule) Name() string  { return "ValuesRule" }
func (r *ValuesRule) Priority() int { return 0 }

func (r *ValuesRule) Match(node LogicalPlanNode, children []PlanNode, catalog *catalog.Catalog) bool {
	_, ok := node.(*LogicalValuesNode)
	return ok
}

func (r *ValuesRule) Apply(node LogicalPlanNode, children []PlanNode, catalog *catalog.Catalog, exprBinder *ExpressionBinder) (PlanNode, error) {
	logicalValues := node.(*LogicalValuesNode)

	physSchema := make([]common.Type, len(logicalValues.Schema))
	for i, col := range logicalValues.Schema {
		physSchema[i] = col.ctype
	}

	emptySchema := make(LogicalSchema, 0)
	emptyPhys := make([]common.Type, 0)

	physValues := make([][]Expr, len(logicalValues.Values))
	for i, row := range logicalValues.Values {
		physRow := make([]Expr, len(row))
		for j, expr := range row {
			physRow[j] = exprBinder.BindExpr(expr, emptySchema, emptyPhys)
		}
		physValues[i] = physRow
	}

	return NewValuesNode(physValues, physSchema), nil
}
