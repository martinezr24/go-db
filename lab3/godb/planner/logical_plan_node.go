package planner

import "strings"

// LogicalPlanNode represents the static structure of a logical query plan.
type LogicalPlanNode interface {
	// OutputSchema returns the schema of the tuples produced by this node.
	OutputSchema() LogicalSchema

	// Sets the required output schema
	// For projection pushdown
	SetRequiredSchema(schema LogicalSchema)
	GetRequiredSchema() LogicalSchema

	// LogicalColumns required for the node to perform its operation
	Dependencies() LogicalSchema

	// Children returns the child logical plan nodes.
	Children() []LogicalPlanNode

	// String returns a string representation of the logical plan node.
	String() string
}

type BaseLogicalPlanNode struct {
	requiredSchema LogicalSchema
}

func (b *BaseLogicalPlanNode) SetRequiredSchema(s LogicalSchema) {
	b.requiredSchema = s
}

func (b *BaseLogicalPlanNode) GetRequiredSchema() LogicalSchema {
	return b.requiredSchema
}

// PrettyPrint generates a human-readable string representation of the PlanNode tree.
// It uses ASCII characters to visualize the hierarchy (e.g. └─, ├─).
func PrettyPrintLogicalPlan(node LogicalPlanNode) string {
	var sb strings.Builder
	prettyPrintLogicalPlanRecursive(&sb, node, "", "")
	return sb.String()
}

func prettyPrintLogicalPlanRecursive(sb *strings.Builder, node LogicalPlanNode, prefix string, childrenPrefix string) {
	if node == nil {
		return
	}

	// Print the current node's string representation
	sb.WriteString(prefix)
	sb.WriteString(node.String())
	sb.WriteString("\n")

	children := node.Children()
	for i, child := range children {
		isLast := i == len(children)-1

		// Determine the prefixes for the current child and its own children
		var childPrefix string
		var grandChildPrefix string

		if isLast {
			childPrefix = childrenPrefix + "└─ "
			grandChildPrefix = childrenPrefix + "   "
		} else {
			childPrefix = childrenPrefix + "├─ "
			grandChildPrefix = childrenPrefix + "│  "
		}

		prettyPrintLogicalPlanRecursive(sb, child, childPrefix, grandChildPrefix)
	}
}
