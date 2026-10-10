package planner

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"mit.edu/dsg/godb/common"
	"mit.edu/dsg/godb/storage"
)

// Helper to create a standard tuple for testing
// Schema: [id(int), name(string), age(int), bio(string)]
// Values: [1, "alice", NULL, NULL]
func makeExprTestTuple() (storage.Tuple, []common.Type) {
	schema := []common.Type{common.IntType, common.StringType, common.IntType, common.StringType}
	tup := storage.FromValues(
		common.NewIntValue(1),          // 0: id
		common.NewStringValue("alice"), // 1: name
		common.NewNullInt(),            // 2: age (NULL)
		common.NewNullString(),         // 3: bio (NULL)
	)
	return tup, schema
}

func countLeaves(e Expr) int {
	leaves := 0
	if _, ok := e.(*BinaryLogicExpression); ok {
		bin := e.(*BinaryLogicExpression)
		leaves += countLeaves(bin.left)
		leaves += countLeaves(bin.right)
	} else {
		leaves++
	}
	return leaves
}

// TestBasicEvaluation checks Column references and Constants.
func TestBasicEvaluation(t *testing.T) {
	tup, schema := makeExprTestTuple()

	c1 := NewConstantValueExpression(common.NewIntValue(100))
	val := c1.Eval(tup)
	assert.Equal(t, int64(100), val.IntValue())

	c2 := NewConstantValueExpression(common.NewStringValue("test"))
	val = c2.Eval(tup)
	assert.Equal(t, "test", val.StringValue())

	colName := NewColumnValueExpression(1, schema, "name")
	val = colName.Eval(tup)
	assert.Equal(t, "alice", val.StringValue())

	colAge := NewColumnValueExpression(2, schema, "age")
	val = colAge.Eval(tup)
	assert.True(t, val.IsNull())
}

// TestComparisonLogic verifies standard comparisons and NULL handling.
func TestComparisonLogic(t *testing.T) {
	tup, schema := makeExprTestTuple()

	// Expressions
	id := NewColumnValueExpression(0, schema, "id")   // 1
	age := NewColumnValueExpression(2, schema, "age") // NULL
	bio := NewColumnValueExpression(3, schema, "bio") // NULL (String)
	const1 := NewConstantValueExpression(common.NewIntValue(1))
	const5 := NewConstantValueExpression(common.NewIntValue(5))

	tests := []struct {
		name     string
		left     Expr
		right    Expr
		op       ComparisonType
		expected int // 1=True, 0=False, -1=Null
	}{
		{"1=1", id, const1, Equal, 1},
		{"1=5", id, const5, Equal, 0},
		{"1<>5", id, const5, NotEqual, 1},
		{"1<5", id, const5, LessThan, 1},
		{"1>5", id, const5, GreaterThan, 0},

		// NULL comparisons (Should always be NULL)
		{"1=NULL", id, age, Equal, -1},
		{"1!=NULL", id, age, NotEqual, -1},
		{"NULL=NULL", age, age, Equal, -1}, // SQL Standard: NULL = NULL is Unknown
		{"NULL!=NULL", age, age, NotEqual, -1},
		{"Str=NULL", bio, bio, Equal, -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := NewComparisonExpression(tt.left, tt.right, tt.op)
			res := expr.Eval(tup)
			if tt.expected == -1 {
				assert.True(t, res.IsNull(), "Expected NULL")
			} else {
				assert.False(t, res.IsNull(), "Expected Value")
				assert.Equal(t, int64(tt.expected), res.IntValue())
			}
		})
	}
}

// TestThreeValuedLogic verifies AND/OR/NOT with True, False, and Null.
func TestThreeValuedLogic(t *testing.T) {
	tup := storage.FromValues() // Empty tuple, using constants

	// Helpers
	T := NewConstantValueExpression(common.NewIntValue(1))
	F := NewConstantValueExpression(common.NewIntValue(0))
	N := NewConstantValueExpression(common.NewNullInt())

	tests := []struct {
		name     string
		expr     Expr
		expected int // 1=True, 0=False, -1=Null
	}{
		// AND
		{"T AND T", NewBinaryLogicExpression(T, T, And), 1},
		{"T AND F", NewBinaryLogicExpression(T, F, And), 0},
		{"T AND N", NewBinaryLogicExpression(T, N, And), -1},
		{"F AND N", NewBinaryLogicExpression(F, N, And), 0}, // Short-circuit False
		{"N AND N", NewBinaryLogicExpression(N, N, And), -1},

		// OR
		{"T OR T", NewBinaryLogicExpression(T, T, Or), 1},
		{"T OR F", NewBinaryLogicExpression(T, F, Or), 1},
		{"T OR N", NewBinaryLogicExpression(T, N, Or), 1}, // Short-circuit True
		{"F OR N", NewBinaryLogicExpression(F, N, Or), -1},
		{"N OR N", NewBinaryLogicExpression(N, N, Or), -1},

		// NOT
		{"NOT T", NewNegationExpression(T), 0},
		{"NOT F", NewNegationExpression(F), 1},
		{"NOT N", NewNegationExpression(N), -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := tt.expr.Eval(tup)
			if tt.expected == -1 {
				assert.True(t, res.IsNull())
			} else {
				assert.False(t, res.IsNull())
				assert.Equal(t, int64(tt.expected), res.IntValue())
			}
		})
	}
}

// TestNullChecks verifies IS NULL / IS NOT NULL.
func TestNullChecks(t *testing.T) {
	tup, schema := makeExprTestTuple()
	id := NewColumnValueExpression(0, schema, "id")   // 1
	age := NewColumnValueExpression(2, schema, "age") // NULL

	// 1 IS NULL -> False
	val := NewNullCheckExpression(id, IsNull).Eval(tup)
	assert.Equal(t, int64(0), val.IntValue())
	// 1 IS NOT NULL -> True
	val = NewNullCheckExpression(id, IsNotNull).Eval(tup)
	assert.Equal(t, int64(1), val.IntValue())

	// NULL IS NULL -> True
	val = NewNullCheckExpression(age, IsNull).Eval(tup)
	assert.Equal(t, int64(1), val.IntValue())
	// NULL IS NOT NULL -> False
	val = NewNullCheckExpression(age, IsNotNull).Eval(tup)
	assert.Equal(t, int64(0), val.IntValue())
}

// TestArithmetic verifies math operations and error handling.
func TestArithmetic(t *testing.T) {
	tup := storage.FromValues()

	val10 := NewConstantValueExpression(common.NewIntValue(10))
	val2 := NewConstantValueExpression(common.NewIntValue(2))
	val0 := NewConstantValueExpression(common.NewIntValue(0))
	valNull := NewConstantValueExpression(common.NewNullInt())

	tests := []struct {
		name     string
		left     Expr
		right    Expr
		op       ArithmeticType
		expected int64
		isNull   bool
	}{
		{"10 + 2", val10, val2, Add, 12, false},
		{"10 - 2", val10, val2, Sub, 8, false},
		{"10 * 2", val10, val2, Mult, 20, false},
		{"10 / 2", val10, val2, Div, 5, false},
		{"10 % 2", val10, val2, Mod, 0, false},

		// Null Propagation
		{"10 + NULL", val10, valNull, Add, 0, true},
		{"NULL * 2", valNull, val2, Mult, 0, true},

		// Division by Zero (Should return NULL)
		{"10 / 0", val10, val0, Div, 0, true},
		{"10 % 0", val10, val0, Mod, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := NewArithmeticExpression(tt.left, tt.right, tt.op)
			res := expr.Eval(tup)
			if tt.isNull {
				assert.True(t, res.IsNull(), "Expected NULL result")
			} else {
				assert.Equal(t, tt.expected, res.IntValue())
			}
		})
	}
}

// TestStringOperations verifies Concat and LIKE.
func TestStringOperations(t *testing.T) {
	tup := storage.FromValues()

	hello := NewConstantValueExpression(common.NewStringValue("hello"))
	world := NewConstantValueExpression(common.NewStringValue("world"))
	nullStr := NewConstantValueExpression(common.NewNullString())

	// 1. Concatenation
	concat := NewStringConcatenation(hello, world)
	val := concat.Eval(tup)
	assert.Equal(t, "helloworld", val.StringValue())

	concatNull := NewStringConcatenation(hello, nullStr)
	val = concatNull.Eval(tup)
	assert.True(t, val.IsNull())

	// 2. LIKE Operator
	// "hello" LIKE "he%" -> True
	likePrefix := NewLikeExpression(hello, NewConstantValueExpression(common.NewStringValue("he%")))
	val = likePrefix.Eval(tup)
	assert.Equal(t, int64(1), val.IntValue())

	// "hello" LIKE "%ll%" -> True
	likeMid := NewLikeExpression(hello, NewConstantValueExpression(common.NewStringValue("%ll%")))
	val = likeMid.Eval(tup)
	assert.Equal(t, int64(1), val.IntValue())

	// "hello" LIKE "he__o" -> True (underscore wildcard)
	likeUnder := NewLikeExpression(hello, NewConstantValueExpression(common.NewStringValue("he__o")))
	val = likeUnder.Eval(tup)
	assert.Equal(t, int64(1), val.IntValue())

	// "hello" LIKE "world" -> False
	likeFalse := NewLikeExpression(hello, world)
	val = likeFalse.Eval(tup)
	assert.Equal(t, int64(0), val.IntValue())

	// Escape Char Test: "100%" LIKE "100\%"
	valPct := NewConstantValueExpression(common.NewStringValue("100%"))
	patPct := NewConstantValueExpression(common.NewStringValue("100\\%"))
	likeEsc := NewLikeExpression(valPct, patPct)
	val = likeEsc.Eval(tup)
	assert.Equal(t, int64(1), val.IntValue())
}

// TestComplexExpressionTree constructs a deep tree of mixed operators and evaluates it.
//
// Scenario:
// Tuple: [id=10, val=50, status="active", region=NULL]
//
// Expression:
//
//	( (val * 2) > 90  AND  status = 'active' )
//	OR
//	( region IS NOT NULL )
//
// Evaluation:
// 1. (50 * 2) = 100 > 90 -> TRUE
// 2. status = 'active' -> TRUE
// 3. TRUE AND TRUE -> TRUE
// 4. region IS NOT NULL -> FALSE
// 5. TRUE OR FALSE -> TRUE
func TestComplexExpressionTree(t *testing.T) {
	schema := []common.Type{common.IntType, common.IntType, common.StringType, common.StringType}
	tup := storage.FromValues(
		common.NewIntValue(10),          // 0: id
		common.NewIntValue(50),          // 1: val
		common.NewStringValue("active"), // 2: status
		common.NewNullString(),          // 3: region (NULL)
	)

	// Column References
	valCol := NewColumnValueExpression(1, schema, "val")
	statusCol := NewColumnValueExpression(2, schema, "status")
	regionCol := NewColumnValueExpression(3, schema, "region")

	// Constants
	const90 := NewConstantValueExpression(common.NewIntValue(90))
	constActive := NewConstantValueExpression(common.NewStringValue("active"))
	const2 := NewConstantValueExpression(common.NewIntValue(2))

	// 1. Subtree A: (val * 2) > 90
	multExpr := NewArithmeticExpression(valCol, const2, Mult)         // 50 * 2 = 100
	gtExpr := NewComparisonExpression(multExpr, const90, GreaterThan) // 100 > 90 -> True

	// 2. Subtree B: status = 'active'
	eqExpr := NewComparisonExpression(statusCol, constActive, Equal) // "active" == "active" -> True

	// 3. Subtree C: A AND B
	andExpr := NewBinaryLogicExpression(gtExpr, eqExpr, And) // True AND True -> True

	// 4. Subtree D: region IS NOT NULL
	nullCheckExpr := NewNullCheckExpression(regionCol, IsNotNull) // False

	// 5. Root: C OR D
	rootExpr := NewBinaryLogicExpression(andExpr, nullCheckExpr, Or) // True OR False -> True

	// Execute
	res := rootExpr.Eval(tup)
	assert.False(t, res.IsNull(), "Result should not be NULL")
	assert.Equal(t, int64(1), res.IntValue(), "Expression tree failed to evaluate to TRUE")
}

// TestComplexExpressionTree_NullPropagate tests a case where NULLs bubble up.
//
// Expression:
// (val + 10) > 100 OR region LIKE 'US%'
//
// Tuple: [val=NULL, region=NULL]
//
// Evaluation:
// 1. NULL + 10 -> NULL
// 2. NULL > 100 -> NULL
// 3. NULL LIKE 'US%' -> NULL
// 4. NULL OR NULL -> NULL
func TestComplexExpressionTree_NullPropagate(t *testing.T) {
	schema := []common.Type{common.IntType, common.StringType}
	tup := storage.FromValues(
		common.NewNullInt(),
		common.NewNullString(),
	)

	valCol := NewColumnValueExpression(0, schema, "val")
	regionCol := NewColumnValueExpression(1, schema, "region")

	const10 := NewConstantValueExpression(common.NewIntValue(10))
	const100 := NewConstantValueExpression(common.NewIntValue(100))
	constUS := NewConstantValueExpression(common.NewStringValue("US%"))

	// Left: (val + 10) > 100
	addExpr := NewArithmeticExpression(valCol, const10, Add)
	gtExpr := NewComparisonExpression(addExpr, const100, GreaterThan)

	// Right: region LIKE 'US%'
	likeExpr := NewLikeExpression(regionCol, constUS)

	// Root: Left OR Right
	rootExpr := NewBinaryLogicExpression(gtExpr, likeExpr, Or)

	res := rootExpr.Eval(tup)
	assert.True(t, res.IsNull(), "Expression should have evaluated to NULL")
}

func makeConstExpr(i int) Expr {
	return NewConstantValueExpression(common.NewIntValue(int64(i)))
}

// TestSplitPredicate verifies that AND trees are flattened into lists,
// while OR trees and leaf nodes remain intact.
func TestSplitPredicate(t *testing.T) {
	e1 := makeConstExpr(1)
	e2 := makeConstExpr(2)
	e3 := makeConstExpr(3)

	tests := []struct {
		name     string
		input    Expr
		expected int // Expected number of splits
	}{
		{
			name:     "Single Expression",
			input:    e1,
			expected: 1,
		},
		{
			name:     "Simple AND (1 AND 2)",
			input:    NewBinaryLogicExpression(e1, e2, And),
			expected: 2,
		},
		{
			name:     "Nested AND Left ((1 AND 2) AND 3)",
			input:    NewBinaryLogicExpression(NewBinaryLogicExpression(e1, e2, And), e3, And),
			expected: 3,
		},
		{
			name:     "Nested AND Right (1 AND (2 AND 3))",
			input:    NewBinaryLogicExpression(e1, NewBinaryLogicExpression(e2, e3, And), And),
			expected: 3,
		},
		{
			name:     "Mixed Logic (1 AND (2 OR 3)) - Should NOT split OR",
			input:    NewBinaryLogicExpression(e1, NewBinaryLogicExpression(e2, e3, Or), And),
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := SplitPredicate(tt.input)

			if len(results) != tt.expected {
				t.Errorf("Expected %d expressions, got %d", tt.expected, len(results))
			}

			if tt.name == "Mixed Logic (1 AND (2 OR 3)) - Should NOT split OR" {
				orExprLeft, okLeft := results[0].(*BinaryLogicExpression)
				orExprRight, okRight := results[1].(*BinaryLogicExpression)
				if (!okRight || orExprRight.logicType != Or) && (!okLeft || orExprLeft.logicType != Or) {
					t.Errorf("Expected second element to be OR expression, got %T", results[1])
				}
			}
		})
	}
}

// TestMergePredicates verifies that a list of expressions is correctly
// recombined into a single AND tree.
func TestMergePredicates(t *testing.T) {
	e1 := makeConstExpr(1)
	e2 := makeConstExpr(2)
	e3 := makeConstExpr(3)

	tests := []struct {
		name   string
		input  []Expr
		verify func(res Expr, t *testing.T)
	}{
		{
			name:  "Empty List",
			input: []Expr{},
			verify: func(res Expr, t *testing.T) {
				if res != nil {
					t.Error("Expected nil for empty list")
				}
			},
		},
		{
			name:  "Single Expression",
			input: []Expr{e1},
			verify: func(res Expr, t *testing.T) {
				if res != e1 {
					t.Error("Expected original expression unchanged")
				}
			},
		},
		{
			name:  "Multiple Expressions (1, 2, 3)",
			input: []Expr{e1, e2, e3},
			verify: func(res Expr, t *testing.T) {
				bin, ok := res.(*BinaryLogicExpression)
				if !ok || bin.logicType != And {
					t.Fatalf("Expected root AND expression")
				}

				leaves := countLeaves(res)

				if leaves != 3 {
					t.Errorf("Expected 3 leaf expressions.")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := MergePredicates(tt.input)
			tt.verify(res, t)
		})
	}
}

// TestSplitMergeRoundTrip ensures that splitting a complex tree and merging it back
// preserves the logical equivalence (all leaves are present).
func TestSplitMergePredicates(t *testing.T) {
	e1 := makeConstExpr(1)
	e2 := makeConstExpr(2)
	e3 := makeConstExpr(3)
	e4 := makeConstExpr(4)

	left := NewBinaryLogicExpression(e1, e2, And)
	right := NewBinaryLogicExpression(e3, e4, And)
	root := NewBinaryLogicExpression(left, right, And)

	parts := SplitPredicate(root)
	if len(parts) != 4 {
		t.Fatalf("Expected 4 parts after split, got %d", len(parts))
	}

	merged := MergePredicates(parts)

	if _, ok := merged.(*BinaryLogicExpression); !ok {
		t.Errorf("Expected Binary Logic Expression result")
	}

	leaves := countLeaves(merged)

	if leaves != 4 {
		t.Errorf("Expected 4 leaves after round trip, found %d", leaves)
	}
}
