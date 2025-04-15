package cmd

import (
	"fmt"
	"github.com/spachava753/cpe/internal/storage"
	"io"
)

// PrintMessageForest prints a forest of trees with proper connectors.
func PrintMessageForest(w io.Writer, roots []storage.MessageIdNode) {
	for _, root := range roots {
		fmt.Fprintf(w, "%s (%s)\n", root.ID, root.CreatedAt.Format("2006-01-02 15:04"))
		num := len(root.Children)
		for j, child := range root.Children {
			isLast := j == num-1
			printSubTree(w, child, "", isLast)
		}
	}
}

// printSubTree prints a node with the appropriate tree structure prefix (recursive)
func printSubTree(w io.Writer, node storage.MessageIdNode, prefix string, isLast bool) {
	connector := "├── "
	if isLast {
		connector = "└── "
	}
	fmt.Fprintf(w, "%s%s%s (%s)\n", prefix, connector, node.ID, node.CreatedAt.Format("2006-01-02 15:04"))

	childPrefix := prefix
	if isLast {
		childPrefix += "    "
	} else {
		childPrefix += "│   "
	}
	for i, child := range node.Children {
		childIsLast := i == len(node.Children)-1
		printSubTree(w, child, childPrefix, childIsLast)
	}
}
