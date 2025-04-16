package cmd

import (
	"fmt"
	"github.com/spachava753/cpe/internal/storage"
	"io"
)

const endOfBranch = "------"
const indent = "    "

// PrintMessageForest prints a forest of trees with proper connectors, including Role and Content.
func PrintMessageForest(w io.Writer, roots []storage.MessageIdNode) {
	for _, root := range roots {
		fmt.Fprintf(w, "%s (%s) [%s] %s\n", root.ID, root.CreatedAt.Format("2006-01-02 15:04"), prettifyRole(root.Role), root.Content)
		prefix := ""
		if len(root.Children) > 1 {
			prefix = indent
		}
		for _, child := range root.Children {
			printSubTree(w, child, prefix)
		}
	}
}

func prettifyRole(role string) string {
	switch role {
	case "user":
		return "user"
	case "assistant":
		return "assistant"
	case "tool_result":
		return "tool result"
	default:
		return role
	}
}

// printSubTree prints a node with the appropriate tree structure prefix (recursive)
func printSubTree(w io.Writer, node storage.MessageIdNode, prefix string) {
	fmt.Fprintf(w, "%s%s (%s) [%s] %s\n", prefix, node.ID, node.CreatedAt.Format("2006-01-02 15:04"), prettifyRole(node.Role), node.Content)
	childPrefix := prefix
	if len(node.Children) > 1 {
		childPrefix += indent
	}
	for _, child := range node.Children {
		printSubTree(w, child, childPrefix)
	}
	if len(node.Children) == 0 {
		fmt.Fprintf(w, "%s%s\n", prefix, endOfBranch)
	}
}
