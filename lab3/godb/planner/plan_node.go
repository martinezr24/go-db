package planner

import (
	"strings"

	"mit.edu/dsg/godb/common"
)

// PlanNode represents the static structure of a query plan.
// It is immutable and contains schema information and the plan tree structure.
type PlanNode interface {
	// OutputSchema returns the schema of the tuples produced by this node.
	OutputSchema() []common.Type

	// Children returns the child plan nodes.
	Children() []PlanNode

	// String returns a string representation of the plan node.
	String() string
}

// PrettyPrint generates a human-readable string representation of the PlanNode tree.
// It uses ASCII characters to visualize the hierarchy (e.g. └─, ├─).
func PrettyPrint(root PlanNode) string {
	var sb strings.Builder
	prettyPrintRecursive(&sb, root, "", "")
	return sb.String()
}

func prettyPrintRecursive(sb *strings.Builder, node PlanNode, prefix string, childrenPrefix string) {
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

		prettyPrintRecursive(sb, child, childPrefix, grandChildPrefix)
	}
}
