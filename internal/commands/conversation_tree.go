package commands

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/storage"
)

// MessageIdNode represents a message and its relationship with its parent and children
// CreatedAt is the creation timestamp of the message.
type MessageIdNode struct {
	ID        string          `json:"id"`
	ParentID  string          `json:"parent_id"`
	CreatedAt time.Time       `json:"created_at"`
	Content   string          `json:"content"` // Short snippet or modality type
	Role      string          `json:"role"`    // user, assistant, or tool_result
	Children  []MessageIdNode `json:"children"`
}

// Define adaptive colors for roles
var (
	userRoleStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#2980b9", Dark: "#3498db"})
	assistantRoleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#16a085", Dark: "#1abc9c"})
	toolRoleStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#d35400", Dark: "#f1c40f"})
	unknownRoleStyle   = lipgloss.NewStyle().Bold(true)
)

const endOfBranch = "------"
const indent = "    "

// roleToString converts a gai.Role to its string representation for display
func roleToDisplayString(role gai.Role) string {
	switch role {
	case gai.User:
		return "user"
	case gai.Assistant:
		return "assistant"
	case gai.ToolResult:
		return "tool_result"
	default:
		return "unknown"
	}
}

// buildMessageForest constructs a forest of MessageIdNode trees from a flat list of messages.
// Messages are expected to have MessageIDKey and (optionally) MessageParentIDKey in ExtraFields.
func buildMessageForest(messages []gai.Message) []MessageIdNode {
	// Build nodes indexed by ID, preserving insertion order
	type nodeInfo struct {
		node     MessageIdNode
		parentID string
	}
	nodeMap := make(map[string]*nodeInfo)
	var orderedIDs []string

	for _, msg := range messages {
		id, _ := msg.ExtraFields[storage.MessageIDKey].(string)
		if id == "" {
			continue
		}
		parentID, _ := msg.ExtraFields[storage.MessageParentIDKey].(string)
		createdAt, _ := msg.ExtraFields[storage.MessageCreatedAtKey].(time.Time)

		// Extract content snippet
		content := ""
		foundText := false
		for _, block := range msg.Blocks {
			if block.ModalityType == gai.Text {
				snippet := block.Content.String()
				snippet = strings.ReplaceAll(snippet, "\n", " ")
				snippet = strings.ReplaceAll(snippet, "\r", " ")
				if len(snippet) > 50 {
					snippet = snippet[:50]
				}
				content = snippet
				foundText = true
				break
			}
		}
		if !foundText && len(msg.Blocks) > 0 {
			switch msg.Blocks[0].ModalityType {
			case gai.Text:
				content = "Text"
			case gai.Image:
				content = "Image"
			case gai.Audio:
				content = "Audio"
			case gai.Video:
				content = "Video"
			default:
				content = fmt.Sprintf("Unknown(%d)", msg.Blocks[0].ModalityType)
			}
		}

		ni := &nodeInfo{
			node: MessageIdNode{
				ID:        id,
				ParentID:  parentID,
				CreatedAt: createdAt,
				Content:   content,
				Role:      roleToDisplayString(msg.Role),
			},
			parentID: parentID,
		}
		nodeMap[id] = ni
		orderedIDs = append(orderedIDs, id)
	}

	// Assemble parent-child relationships using a two-pass approach:
	// First pass: link children to parents via pointer map
	// Second pass: collect roots and build the final tree by copying from pointer nodes
	childrenMap := make(map[string][]string)
	var rootIDs []string
	for _, id := range orderedIDs {
		ni := nodeMap[id]
		if ni.parentID != "" {
			if _, ok := nodeMap[ni.parentID]; ok {
				childrenMap[ni.parentID] = append(childrenMap[ni.parentID], id)
			} else {
				rootIDs = append(rootIDs, id)
			}
		} else {
			rootIDs = append(rootIDs, id)
		}
	}

	// Recursive function to build tree from nodeMap
	var buildTree func(id string) MessageIdNode
	buildTree = func(id string) MessageIdNode {
		ni := nodeMap[id]
		node := ni.node
		for _, childID := range childrenMap[id] {
			node.Children = append(node.Children, buildTree(childID))
		}
		return node
	}

	var roots []MessageIdNode
	for _, id := range rootIDs {
		roots = append(roots, buildTree(id))
	}

	return roots
}

// DefaultTreePrinter implements TreePrinter with styling
type DefaultTreePrinter struct{}

// PrintMessageForest prints a forest of trees with proper connectors, including Role and Content.
func (p *DefaultTreePrinter) PrintMessageForest(w io.Writer, roots []MessageIdNode) {
	type treeWithMax struct {
		node    MessageIdNode
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

// mostRecentTimestamp returns the latest CreatedAt timestamp anywhere in the tree
func mostRecentTimestamp(node MessageIdNode) time.Time {
	maxT := node.CreatedAt
	for _, child := range node.Children {
		childMax := mostRecentTimestamp(child)
		if childMax.After(maxT) {
			maxT = childMax
		}
	}
	return maxT
}

// sortTreeRecursively sorts children by their max descendant timestamp (oldest-to-newest)
func sortTreeRecursively(node *MessageIdNode) {
	for i := range node.Children {
		sortTreeRecursively(&node.Children[i])
	}
	sort.Slice(node.Children, func(i, j int) bool {
		return mostRecentTimestamp(node.Children[i]).Before(mostRecentTimestamp(node.Children[j]))
	})
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
func printSubTree(w io.Writer, node MessageIdNode, prefix string) {
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
