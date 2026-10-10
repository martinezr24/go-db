package planner

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/xwb1989/sqlparser"

	"mit.edu/dsg/godb/catalog"
	"mit.edu/dsg/godb/common"
)

type LogicalPlanBuilder struct {
	// Catalog is immutable during the lifetime of the builder.
	catalog *catalog.Catalog

	// Used for assining IDs to TableRefs created for LogicalScanNodes
	// and LogicalSubqueryNodes
	currentTableRefID uint64
}

func NewLogicalPlanBuilder(cat *catalog.Catalog) *LogicalPlanBuilder {
	return &LogicalPlanBuilder{
		catalog:           cat,
		currentTableRefID: 1,
	}
}

func (b *LogicalPlanBuilder) Plan(stmt sqlparser.Statement) (LogicalPlanNode, error) {
	switch s := stmt.(type) {
	case *sqlparser.Select:
		return b.planSelect(s)
	case *sqlparser.ParenSelect:
		return b.Plan(s.Select)
	case *sqlparser.Insert:
		return b.planInsert(s)
	case *sqlparser.Delete:
		return b.planDelete(s)
	case *sqlparser.Update:
		return b.planUpdate(s)
	default:
		return nil, fmt.Errorf("Statement %T not supported yet", stmt)
	}
}

/*
bindExpr turns an sqlparser expression tree into a godb Expr tree.
The leaves of expressions are either LogicalColumns or constants. The logical columns
are not qualified with namespace qualifiers. This bindExpr does not produce physical
BoundValueExprs directly.

TODOs:
 1. Typechecking maybe should be handled in expr.go
 2. Handle NULL checks
 3. Handle Like and String concat
*/
func (b *LogicalPlanBuilder) bindExpr(expr sqlparser.Expr, scope *FromScope) (Expr, error) {
	if sqlparser.IsNull(expr) {
		return nil, fmt.Errorf("NULL support not merged yet")
	}
	switch v := expr.(type) {
	case *sqlparser.ColName:
		return b.bindColumn(v, scope)
	case *sqlparser.SQLVal:
		return b.bindConstant(v)
	case *sqlparser.ComparisonExpr:
		left, err := b.bindExpr(v.Left, scope)
		if err != nil {
			return nil, err
		}
		right, err := b.bindExpr(v.Right, scope)
		if err != nil {
			return nil, err
		}
		if left.OutputType() != right.OutputType() {
			return nil, fmt.Errorf("bindExpr: two children of ComparisonExpr do not have the same type.")
		}
		comparisonType, err := sqlOpToComparisonType(v.Operator)
		if err != nil {
			return nil, err
		}
		return NewComparisonExpression(left, right, comparisonType), nil
	case *sqlparser.AndExpr:
		left, err := b.bindExpr(v.Left, scope)
		if err != nil {
			return nil, err
		}
		right, err := b.bindExpr(v.Right, scope)
		if err != nil {
			return nil, err
		}
		return NewBinaryLogicExpression(left, right, And), nil
	case *sqlparser.OrExpr:
		left, err := b.bindExpr(v.Left, scope)
		if err != nil {
			return nil, err
		}
		right, err := b.bindExpr(v.Right, scope)
		if err != nil {
			return nil, err
		}
		return NewBinaryLogicExpression(left, right, Or), nil
	case *sqlparser.NotExpr:
		child, err := b.bindExpr(v.Expr, scope)
		if err != nil {
			return nil, err
		}
		return NewNegationExpression(child), nil
	case *sqlparser.BinaryExpr:
		left, err := b.bindExpr(v.Left, scope)
		if err != nil {
			return nil, err
		}
		right, err := b.bindExpr(v.Right, scope)
		if err != nil {
			return nil, err
		}
		switch v.Operator {
		case sqlparser.PlusStr:
			return NewArithmeticExpression(left, right, Add), nil
		case sqlparser.MinusStr:
			return NewArithmeticExpression(left, right, Sub), nil
		case sqlparser.MultStr:
			return NewArithmeticExpression(left, right, Mult), nil
		case sqlparser.DivStr:
			return NewArithmeticExpression(left, right, Div), nil
		case sqlparser.ModStr:
			return NewArithmeticExpression(left, right, Mod), nil
		default:
			return nil, fmt.Errorf("Unsupported binary operator: %v", v.Operator)
		}
	case *sqlparser.IsExpr:
		return nil, fmt.Errorf("IS expression not supported yet")
	case *sqlparser.ParenExpr:
		return b.bindExpr(v.Expr, scope)
	case *sqlparser.FuncExpr:
		// Attempt to find an aggregated column in the current scope.
		// This relies on the exact string representation of aggregate function
		// matching
		funcName := sqlparser.String(v)

		findInScope := func(name string) Expr {
			for _, table := range scope.sourceTables {
				for _, col := range table.schema {
					if strings.EqualFold(col.cname, name) {
						return col
					}
				}
			}
			return nil
		}

		if match := findInScope(funcName); match != nil {
			return match, nil
		}

		// Handling dummy for COUNT(*)
		if strings.EqualFold(v.Name.String(), "COUNT") && len(v.Exprs) == 1 {
			if _, ok := v.Exprs[0].(*sqlparser.StarExpr); ok {
				if match := findInScope("COUNT(1)"); match != nil {
					return match, nil
				}
			}
			if _, ok := v.Exprs[0].(*sqlparser.AliasedExpr); ok {
				if match := findInScope(fmt.Sprintf("COUNT(%s)", sqlparser.String(v.Exprs[0]))); match != nil {
					return match, nil
				}
			}
		}

		return nil, fmt.Errorf("Unsupported function: %s", funcName)

	default:
		return nil, fmt.Errorf("Unsupported expression type: %T", expr)
	}
}

/*
printScope pretty prints the source tables and their schemas in the current scope.
Useful for debugging.
*/
func (b *LogicalPlanBuilder) printScope(scope *FromScope) {
	for _, tableRef := range scope.sourceTables {
		var tableName string
		if tableRef.table != nil {
			tableName = tableRef.table.Name
		} else {
			tableName = "subquery"
		}
		fmt.Printf("Table: %s, Alias: %s\n", tableName, tableRef.alias)
		for _, col := range tableRef.schema {
			fmt.Printf("  Column: %s, Type: %s\n", col.cname, col.ctype)
		}
	}
}

// bindColumn finds a column in the scope and construct a *LogicalColumn.
// We do not create physical BoundValueExprs directly here for optimization purposes.
func (b *LogicalPlanBuilder) bindColumn(col *sqlparser.ColName, scope *FromScope) (*LogicalColumn, error) {
	var tableAlias string
	if !col.Qualifier.IsEmpty() {
		tableAlias = col.Qualifier.Name.String()
	}

	var match *LogicalColumn

	for _, ref := range scope.sourceTables {
		if tableAlias != "" && !strings.HasPrefix(ref.alias, "agg") && ref.alias != tableAlias {
			continue
		}
		for _, logicalCol := range ref.schema {
			if col.Name.EqualString(logicalCol.cname) {
				if match != nil {
					return nil, fmt.Errorf("ambiguous column reference '%s'", col.Name.String())
				}
				match = logicalCol
			}
		}
	}

	if match == nil {
		return nil, fmt.Errorf("column '%s' not found in scope", col.Name.String())
	}

	return match, nil
}

// Compared to bindColumn, this col from a join expression is not qualified with a table alias.
func (b *LogicalPlanBuilder) bindUnqualifiedCol(col sqlparser.ColIdent, scope *FromScope) (*LogicalColumn, error) {
	var match *LogicalColumn

	for _, ref := range scope.sourceTables {
		for _, logicalCol := range ref.schema {
			if col.EqualString(logicalCol.cname) {
				if match != nil {
					return nil, fmt.Errorf("Ambiguous column reference '%s'", col.String())
				}
				match = logicalCol
			}
		}
	}
	if match == nil {
		return nil, fmt.Errorf("Column '%s' not found in scope", col.String())
	}
	return match, nil
}

func (b *LogicalPlanBuilder) bindConstant(v *sqlparser.SQLVal) (Expr, error) {
	switch v.Type {
	case sqlparser.StrVal, sqlparser.HexVal:
		return NewConstantValueExpression(common.NewStringValue(string(v.Val))), nil
	case sqlparser.IntVal:
		i, err := strconv.ParseInt(string(v.Val), 10, 64)
		if err != nil {
			return nil, err
		}
		return NewConstantValueExpression(common.NewIntValue(int64(i))), nil
	}
	return nil, fmt.Errorf("unsupported constant type: %v", v.Type)
}

/*
planFrom handles e.g. FROM A, B and outputs the scope and either
 1. a logical node generated by a single table expression
 2. a LogicalJoinNode representing an implicit cross join. We generate a left-deep join tree
    instead of using a multiway join for simplicity
*/
func (b *LogicalPlanBuilder) planFrom(tableExprs sqlparser.TableExprs) (*FromScope, LogicalPlanNode, error) {
	if len(tableExprs) == 0 {
		return &FromScope{}, nil, fmt.Errorf("FROM clause cannot be empty")
	}

	scope, plan, err := b.planTableExpr(tableExprs[0])
	if err != nil {
		return nil, nil, err
	}

	for i := 1; i < len(tableExprs); i++ {
		nextScope, nextPlan, err := b.planTableExpr(tableExprs[i])
		if err != nil {
			return nil, nil, err
		}

		scope.sourceTables = append(scope.sourceTables, nextScope.sourceTables...)

		// Treat as SELECT * FROM A, B, C
		// No explicit join condition
		plan = NewLogicalJoinNode(plan, nextPlan, nil, Cross)
	}

	return scope, plan, nil
}

/*
planTableExpr handles a single table in the FROM clause
*/
func (b *LogicalPlanBuilder) planTableExpr(te sqlparser.TableExpr) (*FromScope, LogicalPlanNode, error) {
	switch expr := te.(type) {
	case *sqlparser.AliasedTableExpr:
		return b.planAliasedTable(expr)

	case *sqlparser.JoinTableExpr:
		return b.planExplicitJoin(expr)

	case *sqlparser.ParenTableExpr:
		// Handle parentheses: e.g. FROM (A JOIN B)
		return b.planFrom(expr.Exprs)

	default:
		return nil, nil, fmt.Errorf("Unsupported table expression: %T", expr)
	}
}

/*
getNextTableRefID assigns refIDs to TableRefs. Each subquery/scan node maintains a
unique immutable TableRef.
*/
func (b *LogicalPlanBuilder) getNextTableRefID() uint64 {
	result := b.currentTableRefID
	b.currentTableRefID += 1
	return result
}

/*
planAliasedTable handles base tables and subqueries and may generate either
1. LogicalScanNode
2. LogicalSubqueryNode

input: expr.Expr has type sqlparser.SimpleTableExpr
sqlparser.TableName and sqlparser *Subquery implement sqlparser.SimpleTableExpr

TODOs:
 1. Handle index hints
 2. Handle sqlparser.TableName.Qualifier (database or keyspace name)
 3. Look into AliasedExpr vs AliasedTableExpr from sqlparser.
*/
func (b *LogicalPlanBuilder) planAliasedTable(expr *sqlparser.AliasedTableExpr) (*FromScope, LogicalPlanNode, error) {
	var plan LogicalPlanNode
	var tableRef *TableRef

	alias := expr.As.String()

	switch source := expr.Expr.(type) {
	case sqlparser.TableName:
		tableName := source.Name.CompliantName()
		tableMetadata, err := b.catalog.GetTableMetadata(tableName)
		if err != nil {
			return nil, nil, err
		}

		if alias == "" {
			alias = tableName
		}

		tableRef = &TableRef{
			table:  tableMetadata,
			alias:  alias,
			refID:  b.getNextTableRefID(),
			schema: make(LogicalSchema, len(tableMetadata.Columns)),
		}

		for i, col := range tableMetadata.Columns {
			tableRef.schema[i] = &LogicalColumn{
				cname:  col.Name,
				ctype:  col.Type,
				origin: tableRef,
			}
		}

		plan = NewLogicalScanNode(tableRef, false)

	case *sqlparser.Subquery:
		subquery := sqlparser.String(source)
		subPlan, err := b.Plan(source.Select)
		if err != nil {
			return nil, nil, err
		}

		childOutput := subPlan.OutputSchema()

		tableRef = &TableRef{
			table:  nil,
			alias:  alias, // can be ""
			refID:  b.getNextTableRefID(),
			schema: make(LogicalSchema, len(childOutput)),
		}

		if alias == "" { // The parser we use does not support unaliased subqueries
			return nil, nil, fmt.Errorf("Unexpected unaliased subquery: %s", subquery)
		} else { // aliased subquery
			// subquery: (SELECT id FROM users) AS sub -> [sub.id] (origin: sub)
			for i, childCol := range childOutput {
				tableRef.schema[i] = &LogicalColumn{
					cname:  childCol.cname,
					ctype:  childCol.ctype,
					origin: tableRef,
				}
			}
		}

		plan = NewLogicalSubqueryNode(tableRef, subPlan, subquery)

	default:
		return nil, nil, fmt.Errorf("Unsupported source in FROM: %T", source)
	}

	return &FromScope{sourceTables: []*TableRef{tableRef}}, plan, nil
}

/*
planExplicitJoin processes a JoinTableExpr (e.g the B JOIN C from SELECT ... FROM A, B JOIN C)
and generates a LogicalJoinNode. The scope returned are the combined scope of both join participants
since any column in these should be available.

TODOs:
 1. Consider handling coalescing the columns tables are joined on for e.g. SELECT id from A JOIN B USING id.
 2. planExplicitJoin typechecks the join conditions right now but this should maybe be handled in expr.
 3. Handle StraightJoinStr, NaturalJoinStr, NaturalLeftJoinStr, NaturalRightJoinStr from sqlparser.
*/
func (b *LogicalPlanBuilder) planExplicitJoin(expr *sqlparser.JoinTableExpr) (*FromScope, LogicalPlanNode, error) {
	leftScope, leftPlan, err := b.planTableExpr(expr.LeftExpr)
	if err != nil {
		return nil, nil, err
	}

	rightScope, rightPlan, err := b.planTableExpr(expr.RightExpr)
	if err != nil {
		return nil, nil, err
	}

	mergedScope := &FromScope{}
	mergedScope.sourceTables = append(mergedScope.sourceTables, leftScope.sourceTables...)
	mergedScope.sourceTables = append(mergedScope.sourceTables, rightScope.sourceTables...)

	var joinConditions []Expr

	if expr.Condition.On != nil {
		condition, err := b.bindExpr(expr.Condition.On, mergedScope)
		if err != nil {
			return nil, nil, err
		}
		joinConditions = append(joinConditions, condition)
	}
	for _, col := range expr.Condition.Using {
		leftCol, err := b.bindUnqualifiedCol(col, leftScope)
		if err != nil {
			return nil, nil, fmt.Errorf("In USING, column %s not found in left table: %v", col.String(), err)
		}
		rightCol, err := b.bindUnqualifiedCol(col, rightScope)
		if err != nil {
			return nil, nil, fmt.Errorf("In USING, column %s not found in right table: %v", col.String(), err)
		}
		if leftCol.OutputType() != rightCol.OutputType() {
			return nil, nil, fmt.Errorf("In USING clause %s, matching columns have different types.", col.String())
		}
		eqExpr := NewComparisonExpression(leftCol, rightCol, Equal)
		joinConditions = append(joinConditions, eqExpr)
	}

	joinType := Inner
	switch expr.Join {
	case sqlparser.LeftJoinStr:
		joinType = Left
	case sqlparser.RightJoinStr:
		joinType = Right
	case sqlparser.JoinStr:
		joinType = Inner
	default:
		return nil, nil, fmt.Errorf("Unsupported join type: %s", expr.Join)
	}

	joinPlan := NewLogicalJoinNode(leftPlan, rightPlan, joinConditions, joinType)

	return mergedScope, joinPlan, nil
}

func (b *LogicalPlanBuilder) planOrder(orderByStmt sqlparser.OrderBy, child LogicalPlanNode, scope *FromScope) (LogicalPlanNode, error) {
	if len(orderByStmt) == 0 {
		return child, nil
	}

	orderByClauses := make([]OrderByClause, 0, len(orderByStmt))
	for _, ob := range orderByStmt {
		expr, err := b.bindExpr(ob.Expr, scope)
		if err != nil {
			return nil, err
		}
		direction := SortOrderAscending
		if ob.Direction == sqlparser.DescScr {
			direction = SortOrderDescending
		}
		orderByClauses = append(orderByClauses, OrderByClause{
			Expr:      expr,
			Direction: direction,
		})
	}

	return NewLogicalSortNode(child, orderByClauses), nil
}

func (b *LogicalPlanBuilder) resolveConstantInt(expr sqlparser.Expr) (int, error) {
	if val, ok := expr.(*sqlparser.SQLVal); ok {
		if val.Type == sqlparser.IntVal {
			i, err := strconv.ParseInt(string(val.Val), 10, 64)
			if err != nil {
				return 0, err
			}
			return int(i), nil
		}
	}
	return 0, fmt.Errorf("Expression input for resolveConstantInt is not a constant integer.")
}

/*
planLimit generates a LogicalLimitNode
*/
func (b *LogicalPlanBuilder) planLimit(limitStmt *sqlparser.Limit, child LogicalPlanNode) (LogicalPlanNode, error) {
	if limitStmt == nil {
		return child, nil
	}
	var limit, offset int
	if limitStmt.Rowcount != nil {
		l, err := b.resolveConstantInt(limitStmt.Rowcount)
		if err != nil {
			return nil, err
		}
		limit = l
	}
	if limitStmt.Offset != nil {
		o, err := b.resolveConstantInt(limitStmt.Offset)
		if err != nil {
			return nil, err
		}
		offset = o
	}
	return NewLogicalLimitNode(child, limit, offset), nil
}

func (b *LogicalPlanBuilder) resolveAggregatorType(funcName string) (AggregatorType, error) {
	switch strings.ToLower(funcName) {
	case "count":
		return AggCount, nil
	case "sum":
		return AggSum, nil
	case "min":
		return AggMin, nil
	case "max":
		return AggMax, nil
	default:
		return -1, fmt.Errorf("Unrecognized aggregator function: %s", funcName)
	}
}

/*
planAggregation generates a LogicalAggregationNode

TODO:
 1. Currently, the aggregated column needs to be qualified for subsequent projections etc. to work.
    E.g. SELECT MAX(users.id) from users GROUPBY age HAVING age > 10 would work but not
    SELECT MAX(id) from users GROUPBY age HAVING age > 10.
    This is because LogicalAggregation Node acts as a schema barrier.
*/
func (b *LogicalPlanBuilder) planAggregation(sel *sqlparser.Select, child LogicalPlanNode, scope *FromScope) (LogicalPlanNode, *FromScope, error) {
	hasGroupBy := len(sel.GroupBy) > 0
	hasAggregates := false

	aggClauses := make([]AggregateClause, 0)

	// Helper to traverse expressions and find aggregates
	findAggs := func(node sqlparser.SQLNode) (bool, error) {
		found := false
		err := sqlparser.Walk(func(node sqlparser.SQLNode) (bool, error) {
			if funcExpr, ok := node.(*sqlparser.FuncExpr); ok {
				if funcExpr.IsAggregate() {
					found = true

					var argExpr Expr
					var err error

					isStar := false
					if len(funcExpr.Exprs) == 1 {
						if _, ok := funcExpr.Exprs[0].(*sqlparser.StarExpr); ok {
							isStar = true
						}
					}

					if isStar {
						// Ensure it is COUNT (SUM(*) is illegal)
						if strings.ToUpper(funcExpr.Name.String()) != "COUNT" {
							return false, fmt.Errorf("'*' argument is only allowed in COUNT")
						}
						// Dummy argument for COUNT(*)
						argExpr = NewConstantValueExpression(common.NewIntValue(1))
					} else {
						aliasedArg, ok := funcExpr.Exprs[0].(*sqlparser.AliasedExpr)
						if !ok {
							return false, fmt.Errorf("unexpected argument type in aggregate")
						}
						argExpr, err = b.bindExpr(aliasedArg.Expr, scope)
						if err != nil {
							return false, err
						}
					}

					aggType, err := b.resolveAggregatorType(funcExpr.Name.CompliantName())
					if err != nil {
						return false, err
					}

					aggClauses = append(aggClauses, AggregateClause{
						Type: aggType,
						Expr: argExpr,
					})

					return false, nil
				}
			}
			return true, nil
		}, node)
		return found, err
	}

	for _, expr := range sel.SelectExprs {
		f, err := findAggs(expr)
		if err != nil {
			return nil, nil, err
		}
		if f {
			hasAggregates = true
		}
	}

	if sel.Having != nil {
		f, err := findAggs(sel.Having.Expr)
		if err != nil {
			return nil, nil, err
		}
		if f {
			hasAggregates = true
		}
	}

	// If no aggregation, return the original plan and scope
	if !hasGroupBy && !hasAggregates {
		return child, scope, nil
	}

	groupByExprs := make([]Expr, 0)
	for _, gb := range sel.GroupBy {
		bound, err := b.bindExpr(gb, scope)
		if err != nil {
			return nil, nil, err
		}
		groupByExprs = append(groupByExprs, bound)
	}

	aggNode := NewLogicalAggregationNode(child, groupByExprs, aggClauses)

	// Update the Scope
	refID := b.getNextTableRefID()
	newScope := &FromScope{
		sourceTables: []*TableRef{
			{
				schema: aggNode.OutputSchema(),
				table:  nil,
				alias:  fmt.Sprintf("agg_%d", refID),
				refID:  refID,
			},
		},
	}

	if sel.Having != nil {
		pred, err := b.bindExpr(sel.Having.Expr, newScope)
		if err != nil {
			return nil, nil, err
		}

		return NewLogicalFilterNode(aggNode, pred), newScope, nil
	}

	return aggNode, newScope, nil
}

func (b *LogicalPlanBuilder) planSelect(sel *sqlparser.Select) (LogicalPlanNode, error) {
	scope, plan, err := b.planFrom(sel.From)
	if err != nil {
		return nil, err
	}

	if sel.Where != nil {
		pred, err := b.bindExpr(sel.Where.Expr, scope)
		if err != nil {
			return nil, err
		}
		plan = NewLogicalFilterNode(plan, pred)
	}

	plan, scope, err = b.planAggregation(sel, plan, scope)
	if err != nil {
		return nil, err
	}

	plan, err = b.planOrder(sel.OrderBy, plan, scope)
	if err != nil {
		return nil, err
	}

	plan, err = b.planLimit(sel.Limit, plan)
	if err != nil {
		return nil, err
	}

	var projExpressions []Expr
	var outputSchema LogicalSchema

	for _, expr := range sel.SelectExprs {
		switch selectExpr := expr.(type) {
		case *sqlparser.AliasedExpr:
			projExpr, err := b.bindExpr(selectExpr.Expr, scope)
			if err != nil {
				return nil, err
			}
			projExpressions = append(projExpressions, projExpr)

			// Handle aliasing
			// selectExpr.As has type sqlparser.ColIdent
			alias := selectExpr.As.CompliantName()
			finalAlias := alias
			if finalAlias == "" {
				if col, ok := projExpr.(*LogicalColumn); ok {
					finalAlias = col.cname
				} else {
					finalAlias = projExpr.String()
				}
			}

			var newOrigin *TableRef = nil
			if col, ok := projExpr.(*LogicalColumn); ok {
				newOrigin = col.origin
			}
			// If AliasedExpr is a constant expr, origin is nil

			logicalCol := &LogicalColumn{
				cname:  finalAlias,
				ctype:  projExpr.OutputType(),
				origin: newOrigin,
			}
			outputSchema = append(outputSchema, logicalCol)
		case *sqlparser.StarExpr:
			// Handle SELECT *
			for _, col := range plan.OutputSchema() {
				projExpressions = append(projExpressions, col)
				outputSchema = append(outputSchema, col)
			}
		default:
			return nil, fmt.Errorf("Unexpected select expression: %T", expr)
		}
	}
	plan = NewLogicalProjectionNode(plan, projExpressions, outputSchema)

	return plan, nil
}

func (b *LogicalPlanBuilder) planInsert(ins *sqlparser.Insert) (LogicalPlanNode, error) {
	tableName := ins.Table.Name.String()
	tableMetadata, err := b.catalog.GetTableMetadata(tableName)
	if err != nil {
		return nil, err
	}
	// TableRef for the insertion.
	tableRef := &TableRef{
		table:  tableMetadata,
		alias:  tableName,
		refID:  b.getNextTableRefID(),
		schema: make(LogicalSchema, len(tableMetadata.Columns)),
	}
	for i, col := range tableMetadata.Columns {
		tableRef.schema[i] = &LogicalColumn{
			cname:  col.Name,
			ctype:  col.Type,
			origin: tableRef,
		}
	}
	insertScope := &FromScope{
		sourceTables: []*TableRef{tableRef},
	}

	var logicalSchema LogicalSchema
	if len(ins.Columns) > 0 {
		logicalSchema = make(LogicalSchema, len(ins.Columns))
		for i, col := range ins.Columns {
			boundCol, err := b.bindUnqualifiedCol(col, insertScope)
			if err != nil {
				return nil, fmt.Errorf("In INSERT, bindUnqualifiedCol fails for column %s in target table: %v", col.CompliantName(), err)
			}
			if boundCol.origin != tableRef {
				return nil, fmt.Errorf("In INSERT, column %s does not belong to target table", col.CompliantName())
			} else {
				logicalSchema[i] = boundCol
			}
		}
	} else {
		for _, col := range tableMetadata.Columns {
			logicalSchema = append(logicalSchema, &LogicalColumn{
				cname:  col.Name,
				ctype:  col.Type,
				origin: tableRef,
			})
		}
	}

	// Build source
	var sourceNode LogicalPlanNode
	switch rows := ins.Rows.(type) {
	case *sqlparser.Select:
		sourceNode, err = b.planSelect(rows)
		if err != nil {
			return nil, err
		}

	case sqlparser.Values:
		emptyScope := &FromScope{}

		values := make([][]Expr, len(rows))
		for i, tuple := range rows {
			rowExprs := make([]Expr, len(tuple))
			for j, val := range tuple {
				rowExprs[j], err = b.bindExpr(val, emptyScope)
				if err != nil {
					return nil, err
				}
			}
			values[i] = rowExprs
		}
		sourceNode = b.buildValues(values)
	case *sqlparser.ParenSelect:
		sourceNode, err = b.Plan(rows.Select)
		if err != nil {
			return nil, err
		}
	case *sqlparser.Union:
		return nil, fmt.Errorf("INSERT with UNION is not supported yet")
	default:
		return nil, fmt.Errorf("Unexpected INSERT rows type")
	}

	if len(logicalSchema) != len(sourceNode.OutputSchema()) {
		return nil, fmt.Errorf("Column count mismatch between INSERT target and source")
	}

	// Check for exact schema match
	exactMatch := len(logicalSchema) == len(tableMetadata.Columns)
	if len(logicalSchema) == len(tableMetadata.Columns) {
		for i := range logicalSchema {
			if logicalSchema[i].OutputType() != tableMetadata.Columns[i].Type ||
				logicalSchema[i].cname != tableMetadata.Columns[i].Name {
				exactMatch = false
				break
			}
		}
		if exactMatch {
			return NewLogicalInsertNode(tableMetadata.Oid, sourceNode), nil
		}
	}

	// The projection target is the table's full schema. We may reorder in the projection.
	var projExprs []Expr
	// logicalSchema only contains the specified input columns, not the full table schema.
	var projSchema LogicalSchema
	sourceMap := make(map[string]Expr)
	sourceSchema := sourceNode.OutputSchema()
	for i, col := range logicalSchema {
		sourceMap[col.cname] = sourceSchema[i]
	}

	for _, tableCol := range tableMetadata.Columns {
		if sourceCol, ok := sourceMap[tableCol.Name]; ok {
			// TODO: Type check or typecast here.
			projExprs = append(projExprs, sourceCol)
		} else {
			// TODO: Support default values. Using NULL for now.
			var nullVal common.Value
			switch tableCol.Type {
			case common.IntType:
				nullVal = common.NewNullInt()
			case common.StringType:
				nullVal = common.NewNullString()
			default:
				return nil, fmt.Errorf("Unsupported column type in INSERT: %v", tableCol.Type)
			}
			projExprs = append(projExprs, NewConstantValueExpression(nullVal))
		}
		projSchema = append(projSchema, &LogicalColumn{
			cname:  tableCol.Name,
			ctype:  tableCol.Type,
			origin: tableRef,
		})
	}

	alignedSourceNode := NewLogicalProjectionNode(sourceNode, projExprs, projSchema)
	return NewLogicalInsertNode(tableMetadata.Oid, alignedSourceNode), nil
}

func (b *LogicalPlanBuilder) planDelete(del *sqlparser.Delete) (LogicalPlanNode, error) {
	scope, plan, err := b.planFrom(del.TableExprs)
	if err != nil {
		return nil, err
	}

	scanNode, ok := plan.(*LogicalScanNode)
	if !ok {
		return nil, fmt.Errorf("DELETE only supports a base table now.")
	}
	scanNode.ForUpdate = true

	if len(scope.sourceTables) != 1 {
		return nil, fmt.Errorf("DELETE only supports a base table now.")
	}
	tableRef := scope.sourceTables[0]
	table := tableRef.table
	if table == nil {
		return nil, fmt.Errorf("DELETE only supports a base table now.")
	}

	if del.Where != nil {
		pred, err := b.bindExpr(del.Where.Expr, scope)
		if err != nil {
			return nil, err
		}
		plan = NewLogicalFilterNode(plan, pred)
	}
	plan, err = b.planOrder(del.OrderBy, plan, scope)
	if err != nil {
		return nil, err
	}
	plan, err = b.planLimit(del.Limit, plan)
	if err != nil {
		return nil, err
	}

	return NewLogicalDeleteNode(table.Oid, plan), nil
}

func (b *LogicalPlanBuilder) planUpdate(upd *sqlparser.Update) (LogicalPlanNode, error) {
	scope, plan, err := b.planFrom(upd.TableExprs)
	if err != nil {
		return nil, err
	}

	scanNode, ok := plan.(*LogicalScanNode)
	if !ok {
		return nil, fmt.Errorf("UPDATE only supports a base table now.")
	}
	scanNode.ForUpdate = true

	if len(scope.sourceTables) != 1 {
		return nil, fmt.Errorf("UPDATE only supports a base table now.")
	}
	tableRef := scope.sourceTables[0]
	table := tableRef.table
	if table == nil {
		return nil, fmt.Errorf("UPDATE only supports a base table now.")
	}

	// LogicalColumn -> Logical Expression
	assignments := make(map[*LogicalColumn]Expr)

	for _, expr := range upd.Exprs {
		col, err := b.bindColumn(expr.Name, scope)
		if err != nil {
			return nil, fmt.Errorf("In UPDATE, bindColumn fails for column %s in target table: %v", expr.Name.Name.String(), err)
		}
		if col.origin != tableRef {
			return nil, fmt.Errorf("In UPDATE, column %s does not belong to target table", expr.Name.Name.String())
		}

		valExpr, err := b.bindExpr(expr.Expr, scope)
		if err != nil {
			return nil, err
		}

		assignments[col] = valExpr
	}

	if upd.Where != nil {
		pred, err := b.bindExpr(upd.Where.Expr, scope)
		if err != nil {
			return nil, err
		}
		plan = NewLogicalFilterNode(plan, pred)
	}

	return NewLogicalUpdateNode(table.Oid, plan, assignments), nil
}

func (b *LogicalPlanBuilder) buildValues(rows [][]Expr) *LogicalValuesNode {
	if len(rows) == 0 {
		return nil
	}
	firstRow := rows[0]
	schema := make(LogicalSchema, len(firstRow))

	for i, expr := range firstRow {
		schema[i] = &LogicalColumn{
			cname:  fmt.Sprintf("values_col_%d", i),
			ctype:  expr.OutputType(),
			origin: nil, // No table origin
		}
	}

	return NewLogicalValuesNode(rows, schema)
}
