package planner

import (
	"fmt"

	"regexp"
	"strings"

	"github.com/xwb1989/sqlparser"
	"mit.edu/dsg/godb/common"
	"mit.edu/dsg/godb/storage"
)

// Expr represents a node in an expression tree.
// Expressions are stateless and immutable plan nodes.
type Expr interface {
	// Eval evaluates the expression against the provided tuple.
	Eval(t storage.Tuple) common.Value

	// OutputType returns the type of value this expression produces.
	OutputType() common.Type

	// String returns a string representation of the expression.
	String() string

	// Returns a list of table/alias.colnames for logical query optimization
	GetReferencedColumns() []*LogicalColumn
}

// BoundValueExpr performs computation on an input tuple.
type BoundValueExpr struct {
	fieldOffset int // offset of the column in the projected tuple
	outputType  common.Type
	name        string
}

func NewColumnValueExpression(fieldOffset int, tupleSchema []common.Type, name string) *BoundValueExpr {
	return &BoundValueExpr{
		fieldOffset: fieldOffset,
		outputType:  tupleSchema[fieldOffset],
		name:        name,
	}
}

func (e *BoundValueExpr) Eval(t storage.Tuple) common.Value {
	return t.GetValue(e.fieldOffset)
}

func (e *BoundValueExpr) OutputType() common.Type {
	return e.outputType
}

func (e *BoundValueExpr) String() string {
	return fmt.Sprintf("{%s: offset %d type %s}", e.name, e.fieldOffset, e.outputType)
}

func (e *BoundValueExpr) Name() string {
	return e.name
}

func (e *BoundValueExpr) GetReferencedColumns() []*LogicalColumn {
	panic("Error: Attempted to call GetReferencedColumns on a BoundValueExpr. " +
		"This expr is physical and cannot be used in Logical Optimization.")
}

type ConstantValueExpr struct {
	val common.Value
}

func NewConstantValueExpression(val common.Value) *ConstantValueExpr {
	return &ConstantValueExpr{val: val}
}

func (e *ConstantValueExpr) Eval(t storage.Tuple) common.Value {
	return e.val
}

func (e *ConstantValueExpr) OutputType() common.Type {
	return e.val.Type()
}

func (e *ConstantValueExpr) String() string {
	return e.val.String()
}

func (e *ConstantValueExpr) GetReferencedColumns() []*LogicalColumn {
	return []*LogicalColumn{}
}

type ComparisonType int

const (
	Equal ComparisonType = iota
	NotEqual
	GreaterThan
	LessThan
	GreaterThanOrEqual
	LessThanOrEqual
)

func (c ComparisonType) String() string {
	switch c {
	case Equal:
		return "="
	case NotEqual:
		return "!="
	case GreaterThan:
		return ">"
	case LessThan:
		return "<"
	case GreaterThanOrEqual:
		return ">="
	case LessThanOrEqual:
		return "<="
	}
	return "???"
}

func sqlOpToComparisonType(op string) (ComparisonType, error) {
	switch strings.ToLower(op) {
	case sqlparser.EqualStr:
		return Equal, nil

	case sqlparser.NotEqualStr:
		return NotEqual, nil

	case sqlparser.GreaterThanStr:
		return GreaterThan, nil

	case sqlparser.LessThanStr:
		return LessThan, nil

	case sqlparser.GreaterEqualStr:
		return GreaterThanOrEqual, nil

	case sqlparser.LessEqualStr:
		return LessThanOrEqual, nil
	}

	return 0, fmt.Errorf("unsupported comparison operator: %q", op)
}

type ComparisonExpression struct {
	left     Expr
	right    Expr
	compType ComparisonType
}

func NewComparisonExpression(left Expr, right Expr, compType ComparisonType) *ComparisonExpression {
	return &ComparisonExpression{
		left:     left,
		right:    right,
		compType: compType,
	}
}

func (e *ComparisonExpression) Eval(t storage.Tuple) common.Value {
	val1 := e.left.Eval(t)
	val2 := e.right.Eval(t)

	if val1.IsNull() || val2.IsNull() {
		return common.NewNullInt()
	}

	cmp := val1.Compare(val2)
	var result bool

	switch e.compType {
	case Equal:
		result = cmp == 0
	case NotEqual:
		result = cmp != 0
	case GreaterThan:
		result = cmp > 0
	case LessThan:
		result = cmp < 0
	case GreaterThanOrEqual:
		result = cmp >= 0
	case LessThanOrEqual:
		result = cmp <= 0
	}
	if result {
		return common.NewIntValue(1)
	}
	return common.NewIntValue(0)
}

func (e *ComparisonExpression) OutputType() common.Type {
	return common.IntType
}

func (e *ComparisonExpression) String() string {
	return fmt.Sprintf("(%s %s %s)", e.left.String(), e.compType.String(), e.right.String())
}

func (e *ComparisonExpression) GetReferencedColumns() []*LogicalColumn {
	refs := make([]*LogicalColumn, 0)
	refs = append(refs, e.left.GetReferencedColumns()...)
	refs = append(refs, e.right.GetReferencedColumns()...)
	return refs
}

func ExprIsTrue(v common.Value) bool {
	// Must be an integer type, not null, and non-zero.
	return v.Type() == common.IntType && !v.IsNull() && v.IntValue() != 0
}

func ExprIsFalse(v common.Value) bool {
	return v.Type() == common.IntType && !v.IsNull() && v.IntValue() == 0
}

type BinaryLogicType int

const (
	And BinaryLogicType = iota
	Or
)

func (l BinaryLogicType) String() string {
	switch l {
	case And:
		return "AND"
	case Or:
		return "OR"
	}
	return "???"
}

type BinaryLogicExpression struct {
	left      Expr
	right     Expr
	logicType BinaryLogicType
}

func NewBinaryLogicExpression(left Expr, right Expr, logicType BinaryLogicType) *BinaryLogicExpression {
	return &BinaryLogicExpression{
		left:      left,
		right:     right,
		logicType: logicType,
	}
}

func (e *BinaryLogicExpression) Eval(t storage.Tuple) common.Value {
	val1 := e.left.Eval(t)
	val2 := e.right.Eval(t)

	switch e.logicType {
	case And:
		if ExprIsTrue(val1) && ExprIsTrue(val2) {
			return common.NewIntValue(1)
		} else if ExprIsFalse(val1) || ExprIsFalse(val2) {
			return common.NewIntValue(0)
		}
		return common.NewNullInt()
	case Or:
		if ExprIsTrue(val1) || ExprIsTrue(val2) {
			return common.NewIntValue(1)
		} else if ExprIsFalse(val1) && ExprIsFalse(val2) {
			return common.NewIntValue(0)
		}
		return common.NewNullInt()
	default:
		panic("unknown logic type")
	}
}

func (e *BinaryLogicExpression) OutputType() common.Type {
	return common.IntType
}

func (e *BinaryLogicExpression) String() string {
	return fmt.Sprintf("(%s %s %s)", e.left.String(), e.logicType.String(), e.right.String())
}

func (e *BinaryLogicExpression) GetReferencedColumns() []*LogicalColumn {
	refs := make([]*LogicalColumn, 0)
	refs = append(refs, e.left.GetReferencedColumns()...)
	refs = append(refs, e.right.GetReferencedColumns()...)
	return refs
}

// SplitPredicate recursively breaks an AND expression tree into a slice of individual conditions.
// e.g. "a=1 AND b=2 AND c=3" -> [a=1, b=2, c=3]
func SplitPredicate(expr Expr) []Expr {
	if bin, ok := expr.(*BinaryLogicExpression); ok && bin.logicType == And {
		return append(SplitPredicate(bin.left), SplitPredicate(bin.right)...)
	}
	return []Expr{expr}
}

// MergePredicates recombines a slice of expressions into a single AND expression tree.
// e.g. [a=1, b=2] -> "a=1 AND b=2"
func MergePredicates(exprs []Expr) Expr {
	if len(exprs) == 0 {
		return nil
	}
	root := exprs[0]
	for i := 1; i < len(exprs); i++ {
		root = NewBinaryLogicExpression(root, exprs[i], And)
	}
	return root
}

type NegationExpression struct {
	child Expr
}

func NewNegationExpression(child Expr) *NegationExpression {
	return &NegationExpression{
		child: child,
	}
}

func (e *NegationExpression) Eval(t storage.Tuple) common.Value {
	val := e.child.Eval(t)
	if val.IsNull() {
		return common.NewNullInt()
	}
	if ExprIsTrue(val) {
		return common.NewIntValue(0)
	}
	return common.NewIntValue(1)
}

func (e *NegationExpression) OutputType() common.Type {
	return common.IntType
}

func (e *NegationExpression) String() string {
	return fmt.Sprintf("!(%s)", e.child.String())
}

func (e *NegationExpression) GetReferencedColumns() []*LogicalColumn {
	return e.child.GetReferencedColumns()
}

type NullCheckType int

const (
	IsNull NullCheckType = iota
	IsNotNull
)

func (n NullCheckType) String() string {
	switch n {
	case IsNull:
		return "IS NULL"
	case IsNotNull:
		return "IS NOT NULL"
	}
	return "???"
}

type NullCheckExpression struct {
	child     Expr
	checkType NullCheckType
}

func NewNullCheckExpression(child Expr, checkType NullCheckType) *NullCheckExpression {
	return &NullCheckExpression{
		child:     child,
		checkType: checkType,
	}
}

func (e *NullCheckExpression) Eval(t storage.Tuple) common.Value {
	val := e.child.Eval(t)
	isNull := val.IsNull()

	var result bool
	switch e.checkType {
	case IsNull:
		result = isNull
	case IsNotNull:
		result = !isNull
	}

	if result {
		return common.NewIntValue(1)
	}
	return common.NewIntValue(0)
}

func (e *NullCheckExpression) OutputType() common.Type {
	return common.IntType
}

func (e *NullCheckExpression) String() string {
	return fmt.Sprintf("(%s %s)", e.child.String(), e.checkType.String())
}

func (e *NullCheckExpression) GetReferencedColumns() []*LogicalColumn {
	return []*LogicalColumn{}
}

type ArithmeticType int

const (
	Add ArithmeticType = iota
	Sub
	Mult
	Div
	Mod
)

func (a ArithmeticType) String() string {
	switch a {
	case Add:
		return "+"
	case Sub:
		return "-"
	case Mult:
		return "*"
	case Div:
		return "/"
	case Mod:
		return "%"
	}
	return "?"
}

type ArithmeticExpression struct {
	left  Expr
	right Expr
	op    ArithmeticType
}

func NewArithmeticExpression(left Expr, right Expr, op ArithmeticType) *ArithmeticExpression {
	return &ArithmeticExpression{
		left:  left,
		right: right,
		op:    op,
	}
}

func (e *ArithmeticExpression) Eval(t storage.Tuple) common.Value {
	val1 := e.left.Eval(t)
	val2 := e.right.Eval(t)

	if val1.IsNull() || val2.IsNull() {
		return common.NewNullInt()
	}

	v1 := val1.IntValue()
	v2 := val2.IntValue()
	var result int64

	switch e.op {
	case Add:
		result = v1 + v2
	case Sub:
		result = v1 - v2
	case Mult:
		result = v1 * v2
	case Div:
		if v2 == 0 {
			return common.NewNullInt()
		}
		result = v1 / v2
	case Mod:
		if v2 == 0 {
			return common.NewNullInt()
		}
		result = v1 % v2
	}
	return common.NewIntValue(result)
}

func (e *ArithmeticExpression) OutputType() common.Type {
	return common.IntType
}

func (e *ArithmeticExpression) String() string {
	return fmt.Sprintf("(%s %s %s)", e.left.String(), e.op.String(), e.right.String())
}

func (e *ArithmeticExpression) GetReferencedColumns() []*LogicalColumn {
	refs := make([]*LogicalColumn, 0)
	refs = append(refs, e.left.GetReferencedColumns()...)
	refs = append(refs, e.right.GetReferencedColumns()...)
	return refs
}

// StringConcatExpression handles string manipulation.
type StringConcatExpression struct {
	left  Expr
	right Expr
}

func NewStringConcatenation(left Expr, right Expr) *StringConcatExpression {
	return &StringConcatExpression{left: left, right: right}
}

func (e *StringConcatExpression) Eval(t storage.Tuple) common.Value {
	val1 := e.left.Eval(t)
	val2 := e.right.Eval(t)

	if val1.IsNull() || val2.IsNull() {
		return common.NewNullString()
	}

	return common.NewStringValue(val1.StringValue() + val2.StringValue())
}

func (e *StringConcatExpression) OutputType() common.Type {
	return common.StringType
}

func (e *StringConcatExpression) String() string {
	return fmt.Sprintf("(%s || %s)", e.left.String(), e.right.String())
}

func (e *StringConcatExpression) GetReferencedColumns() []*LogicalColumn {
	return []*LogicalColumn{}
}

type LikeExpression struct {
	left  Expr // The value to check
	right Expr // The pattern (usually a constant)

	cachedRegex *regexp.Regexp
}

func sqlLikeToRegexPattern(pattern string) string {
	var regexPattern strings.Builder
	regexPattern.WriteString("^")
	chars := []rune(pattern)
	for i := 0; i < len(chars); i++ {
		c := chars[i]
		if c == '\\' {
			// Look ahead for escape
			if i+1 < len(chars) {
				next := chars[i+1]
				if next == '%' || next == '_' {
					// It was a literal % or _
					regexPattern.WriteString(regexp.QuoteMeta(string(next)))
					i++
					continue
				}
			}
			// Just a normal backslash (or followed by non-wildcard), treat as literal
			regexPattern.WriteString(regexp.QuoteMeta(string(c)))
		} else if c == '%' {
			regexPattern.WriteString(".*")
		} else if c == '_' {
			regexPattern.WriteString(".")
		} else {
			regexPattern.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	regexPattern.WriteString("$")
	return regexPattern.String()
}

func NewLikeExpression(left Expr, right Expr) *LikeExpression {
	expr := &LikeExpression{left: left, right: right}

	// FIX: Pre-compile regex if the pattern is a constant
	if constExpr, ok := right.(*ConstantValueExpr); ok {
		// Evaluate the constant (tuple is ignored for constants)
		patternVal := constExpr.Eval(storage.EmptyTuple)
		if !patternVal.IsNull() {
			regexStr := sqlLikeToRegexPattern(patternVal.StringValue())
			// We ignore errors here; Eval will handle runtime failure or fallback
			if re, err := regexp.Compile(regexStr); err == nil {
				expr.cachedRegex = re
			}
		}
	}
	return expr
}

func (e *LikeExpression) Eval(t storage.Tuple) common.Value {
	val := e.left.Eval(t)
	// Short-circuit nulls
	if val.IsNull() {
		return common.NewNullInt()
	}

	target := val.StringValue()
	var matched bool

	if e.cachedRegex != nil {
		matched = e.cachedRegex.MatchString(target)
	} else {
		// Fallback: Pattern is dynamic (e.g. comparing two columns), recompile every time
		patternVal := e.right.Eval(t)
		if patternVal.IsNull() {
			return common.NewNullInt()
		}
		regexStr := sqlLikeToRegexPattern(patternVal.StringValue())
		matchResult, err := regexp.MatchString(regexStr, target)
		if err != nil {
			return common.NewIntValue(0)
		}
		matched = matchResult
	}

	if matched {
		return common.NewIntValue(1)
	}
	return common.NewIntValue(0)
}

func (e *LikeExpression) OutputType() common.Type {
	return common.IntType
}

func (e *LikeExpression) String() string {
	return fmt.Sprintf("(%s LIKE %s)", e.left.String(), e.right.String())
}

func (e *LikeExpression) GetReferencedColumns() []*LogicalColumn {
	refs := make([]*LogicalColumn, 0)
	refs = append(refs, e.left.GetReferencedColumns()...)
	refs = append(refs, e.right.GetReferencedColumns()...)
	return refs
}
