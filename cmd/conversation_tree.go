package cmd

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spachava753/cpe/internal/storage"
)

// Define adaptive colors for roles
var (
	userRoleStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#2980b9", Dark: "#3498db"})
	assistantRoleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#16a085", Dark: "#1abc9c"})
	toolRoleStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#d35400", Dark: "#f1c40f"})
	unknownRoleStyle   = lipgloss.NewStyle().Bold(true)
)

const endOfBranch = "------"
const indent = "    "

// MostRecentTimestamp returns the latest CreatedAt timestamp anywhere in the tree
func mostRecentTimestamp(node storage.MessageIdNode) time.Time {
	maxT := node.CreatedAt
	for _, child := range node.Children {
		childMax := mostRecentTimestamp(child)
		if childMax.After(maxT) {
			maxT = childMax
		}
	}
	return maxT
}

// Recursively sort children by their max descendant timestamp (oldest-to-newest)
func sortTreeRecursively(node *storage.MessageIdNode) {
	for i := range node.Children {
		sortTreeRecursively(&node.Children[i])
	}
	sort.Slice(node.Children, func(i, j int) bool {
		return mostRecentTimestamp(node.Children[i]).Before(mostRecentTimestamp(node.Children[j]))
	})
}

// PrintMessageForest prints a forest of trees with proper connectors, including Role and Content.
func PrintMessageForest(w io.Writer, roots []storage.MessageIdNode) {
	type treeWithMax struct {
		node    storage.MessageIdNode
		maxTime time.Time
	}

	var trees []treeWithMax
	for _, root := range roots {
		trees = append(trees, treeWithMax{
			node:    root,
			maxTime: mostRecentTimestamp(root),
		})
	}
	// Sort descending by maxTime
	sort.Slice(trees, func(i, j int) bool {
		return trees[i].maxTime.Before(trees[j].maxTime)
	})

	for i := range trees {
		sortTreeRecursively(&trees[i].node)
	}

	for _, tr := range trees {
		root := tr.node
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
		return userRoleStyle.Render("USER")
	case "assistant":
		return assistantRoleStyle.Render("ASSISTANT")
	case "tool_result":
		return toolRoleStyle.Render("TOOL RESULT")
	default:
		return unknownRoleStyle.Render(strings.ToUpper(role))
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
