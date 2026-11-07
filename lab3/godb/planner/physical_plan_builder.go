package planner

import (
	"fmt"

	"mit.edu/dsg/godb/catalog"
	"mit.edu/dsg/godb/common"
)

type ExpressionBinder struct{}

func NewExpressionBinder() *ExpressionBinder {
	return &ExpressionBinder{}
}

// BindExpr converts LogicalColumn pointers into BoundValueExpr (calculating integer offsets)
// The bindExpr function in logical plan builder only builds LOGICAL expression trees.
func (b *ExpressionBinder) BindExpr(e Expr, logicalSchema LogicalSchema, physicalSchema []common.Type) Expr {
	switch v := e.(type) {
	case *LogicalColumn:
		for i, col := range logicalSchema {
			if col.Equals(v) {
				// We use the index 'i' to find the type in the physical schema
				return NewColumnValueExpression(i, physicalSchema, v.cname)
			}
		}
		panic(fmt.Sprintf("ExpressionBinder Failed: Column %s not found in input logicalSchema, %v", v.cname, logicalSchema))

	case *ComparisonExpression:
		return NewComparisonExpression(
			b.BindExpr(v.left, logicalSchema, physicalSchema),
			b.BindExpr(v.right, logicalSchema, physicalSchema),
			v.compType,
		)
	case *BinaryLogicExpression:
		return NewBinaryLogicExpression(
			b.BindExpr(v.left, logicalSchema, physicalSchema),
			b.BindExpr(v.right, logicalSchema, physicalSchema),
			v.logicType,
		)
	case *ArithmeticExpression:
		return NewArithmeticExpression(
			b.BindExpr(v.left, logicalSchema, physicalSchema),
			b.BindExpr(v.right, logicalSchema, physicalSchema),
			v.op,
		)
	case *NegationExpression:
		return NewNegationExpression(
			b.BindExpr(v.child, logicalSchema, physicalSchema),
		)
	case *NullCheckExpression:
		return NewNullCheckExpression(
			b.BindExpr(v.child, logicalSchema, physicalSchema),
			v.checkType,
		)
	case *StringConcatExpression:
		return NewStringConcatenation(
			b.BindExpr(v.left, logicalSchema, physicalSchema),
			b.BindExpr(v.right, logicalSchema, physicalSchema),
		)
	case *LikeExpression:
		return NewLikeExpression(
			b.BindExpr(v.left, logicalSchema, physicalSchema),
			b.BindExpr(v.right, logicalSchema, physicalSchema),
		)
	case *ConstantValueExpr:
		return v
	}
	return e
}

// BindExpr shifts all BoundValueExprs' integer offsets by a fixed constant
// additional offset.
func (b *ExpressionBinder) ShiftExpr(e Expr, shiftOffset int) Expr {
	switch v := e.(type) {
	case *BoundValueExpr:
		return &BoundValueExpr{
			fieldOffset: v.fieldOffset + shiftOffset,
			outputType:  v.outputType,
			name:        v.name,
		}

	case *ComparisonExpression:
		return NewComparisonExpression(
			b.ShiftExpr(v.left, shiftOffset),
			b.ShiftExpr(v.right, shiftOffset),
			v.compType,
		)
	case *BinaryLogicExpression:
		return NewBinaryLogicExpression(
			b.ShiftExpr(v.left, shiftOffset),
			b.ShiftExpr(v.right, shiftOffset),
			v.logicType,
		)
	case *ArithmeticExpression:
		return NewArithmeticExpression(
			b.ShiftExpr(v.left, shiftOffset),
			b.ShiftExpr(v.right, shiftOffset),
			v.op,
		)
	case *NegationExpression:
		return NewNegationExpression(
			b.ShiftExpr(v.child, shiftOffset),
		)
	case *NullCheckExpression:
		return NewNullCheckExpression(
			b.ShiftExpr(v.child, shiftOffset),
			v.checkType,
		)
	case *StringConcatExpression:
		return NewStringConcatenation(
			b.ShiftExpr(v.left, shiftOffset),
			b.ShiftExpr(v.right, shiftOffset),
		)
	case *LikeExpression:
		return NewLikeExpression(
			b.ShiftExpr(v.left, shiftOffset),
			b.ShiftExpr(v.right, shiftOffset),
		)
	case *ConstantValueExpr:
		return v
	}
	return e
}

/*
For now, FuseProjectionIntoExpr only handles the case where the child projection does not involve any expression
evaluation (i.e. it's just a projection of columns from its child). In this case, we can directly replace
references to the LogicalColumns in the parent projection with the corresponding expressions from the child
projection.

TODO: Supports more complex cases where the child projection involves expression evaluation (e.g. projecting a + b).
*/
func (b *ExpressionBinder) FuseProjectionIntoExpr(e Expr, childLogicalSchema LogicalSchema, childExpressions []Expr) (Expr, error) {
	switch v := e.(type) {
	case *LogicalColumn:
		for i, logicalCol := range childLogicalSchema {
			if v.Equals(logicalCol) {
				return childExpressions[i], nil
			}
		}
		return nil, fmt.Errorf("ExpressionBinder.FuseProjectionIntoExpr: Column %s not found in child expressions, cannot fuse projection", v.cname)

	case *ComparisonExpression:
		leftFused, err := b.FuseProjectionIntoExpr(v.left, childLogicalSchema, childExpressions)
		if err != nil {
			return nil, err
		}
		rightFused, err := b.FuseProjectionIntoExpr(v.right, childLogicalSchema, childExpressions)
		if err != nil {
			return nil, err
		}
		return NewComparisonExpression(
			leftFused,
			rightFused,
			v.compType,
		), nil
	case *BinaryLogicExpression:
		leftFused, err := b.FuseProjectionIntoExpr(v.left, childLogicalSchema, childExpressions)
		if err != nil {
			return nil, err
		}
		rightFused, err := b.FuseProjectionIntoExpr(v.right, childLogicalSchema, childExpressions)
		if err != nil {
			return nil, err
		}
		return NewBinaryLogicExpression(
			leftFused,
			rightFused,
			v.logicType,
		), nil
	case *ArithmeticExpression:
		leftFused, err := b.FuseProjectionIntoExpr(v.left, childLogicalSchema, childExpressions)
		if err != nil {
			return nil, err
		}
		rightFused, err := b.FuseProjectionIntoExpr(v.right, childLogicalSchema, childExpressions)
		if err != nil {
			return nil, err
		}
		return NewArithmeticExpression(
			leftFused,
			rightFused,
			v.op,
		), nil
	case *NegationExpression:
		childFused, err := b.FuseProjectionIntoExpr(v.child, childLogicalSchema, childExpressions)
		if err != nil {
			return nil, err
		}
		return NewNegationExpression(
			childFused,
		), nil
	case *NullCheckExpression:
		childFused, err := b.FuseProjectionIntoExpr(v.child, childLogicalSchema, childExpressions)
		if err != nil {
			return nil, err
		}
		return NewNullCheckExpression(
			childFused,
			v.checkType,
		), nil
	case *StringConcatExpression:
		leftFused, err := b.FuseProjectionIntoExpr(v.left, childLogicalSchema, childExpressions)
		if err != nil {
			return nil, err
		}
		rightFused, err := b.FuseProjectionIntoExpr(v.right, childLogicalSchema, childExpressions)
		if err != nil {
			return nil, err
		}
		return NewStringConcatenation(
			leftFused,
			rightFused,
		), nil
	case *LikeExpression:
		leftFused, err := b.FuseProjectionIntoExpr(v.left, childLogicalSchema, childExpressions)
		if err != nil {
			return nil, err
		}
		rightFused, err := b.FuseProjectionIntoExpr(v.right, childLogicalSchema, childExpressions)
		if err != nil {
			return nil, err
		}
		return NewLikeExpression(
			leftFused,
			rightFused,
		), nil
	case *ConstantValueExpr:
		return v, nil
	}
	return nil, fmt.Errorf("ExpressionBinder.FuseProjectionIntoExpr: Unsupported expression type %T in projection fusion", e)
}

type PhysicalPlanBuilder struct {
	catalog    *catalog.Catalog
	rules      []PhysicalConversionRule
	exprBinder *ExpressionBinder
}

func NewPhysicalPlanBuilder(catalog *catalog.Catalog, customRules []PhysicalConversionRule) *PhysicalPlanBuilder {
	return &PhysicalPlanBuilder{
		catalog:    catalog,
		rules:      customRules,
		exprBinder: NewExpressionBinder(),
	}
}

func (b *PhysicalPlanBuilder) Build(logicalPlan LogicalPlanNode) (PlanNode, error) {
	logicalChildren := logicalPlan.Children()
	physicalChildren := make([]PlanNode, len(logicalChildren))

	for i, child := range logicalChildren {
		physChild, err := b.Build(child)
		if err != nil {
			return nil, err
		}
		physicalChildren[i] = physChild
	}

	var bestRule PhysicalConversionRule
	maxPriority := -1
	for _, rule := range b.rules {
		if rule.Match(logicalPlan, physicalChildren, b.catalog) && rule.Priority() > maxPriority {
			bestRule = rule
			maxPriority = rule.Priority()
		}
	}

	if bestRule == nil {
		return nil, fmt.Errorf("No physical conversion rule matched for node: %s", logicalPlan.String())
	}

	return bestRule.Apply(logicalPlan, physicalChildren, b.catalog, b.exprBinder)
}
