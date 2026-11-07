package planner

// Logical -> logical rule
type LogicalRule interface {
	// Match returns true if the optimizer should attempt Apply.
	Match(node LogicalPlanNode) bool

	// Apply returns the transformed node.
	// If the rule performed a change, it returns the new root of this subtree.
	// If no change, it returns the original node.
	Apply(node LogicalPlanNode) LogicalPlanNode

	PostProcess(node LogicalPlanNode) LogicalPlanNode

	Name() string
}

type Optimizer struct {
	rules []LogicalRule
}

func NewOptimizer(customRules []LogicalRule) *Optimizer {
	return &Optimizer{
		rules: customRules,
	}
}

func (o *Optimizer) Optimize(node LogicalPlanNode) LogicalPlanNode {
	for _, rule := range o.rules {
		node = o.OptimizeWithRule(rule, node)
	}
	return node
}

func (o *Optimizer) OptimizeWithRule(rule LogicalRule, node LogicalPlanNode) LogicalPlanNode {
	for {
		startNode := node
		if rule.Match(node) {
			node = rule.Apply(node)
		}
		if startNode == node {
			break
		}
	}

	children := node.Children()
	if len(children) > 0 {
		newChildren := make([]LogicalPlanNode, len(children))
		changed := false
		for i, child := range children {
			newChildren[i] = o.OptimizeWithRule(rule, child)
			if newChildren[i] != child {
				changed = true
			}
		}

		if changed {
			node = setChildren(node, newChildren)
		}
	}

	if rule.Match(node) {
		node = rule.PostProcess(node)
	}

	return node
}

// re-attach children
func setChildren(node LogicalPlanNode, children []LogicalPlanNode) LogicalPlanNode {
	switch n := node.(type) {
	case *LogicalProjectionNode:
		return NewLogicalProjectionNode(children[0], n.Expressions, n.OutputSchema())
	case *LogicalFilterNode:
		return NewLogicalFilterNode(children[0], n.Predicate)
	case *LogicalSortNode:
		return NewLogicalSortNode(children[0], n.OrderBy)
	case *LogicalLimitNode:
		return NewLogicalLimitNode(children[0], n.Limit, n.Offset)
	case *LogicalAggregationNode:
		return NewLogicalAggregationNode(children[0], n.GroupByClause, n.AggClauses)
	case *LogicalJoinNode:
		return NewLogicalJoinNode(children[0], children[1], n.joinOn, n.joinType)
	case *LogicalInsertNode:
		return NewLogicalInsertNode(n.TableOid, children[0])
	case *LogicalDeleteNode:
		return NewLogicalDeleteNode(n.TableOid, children[0])
	case *LogicalUpdateNode:
		return NewLogicalUpdateNode(n.TableOid, children[0], n.Updates)
	case *LogicalSubqueryNode:
		return NewLogicalSubqueryNode(n.TableRef, children[0], n.subquery)
	default:
		panic("Unsupported node type in setChildren: " + node.String())
	}
	return node
}
