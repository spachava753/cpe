package storage

import (
	"context"
	"fmt"
	"iter"
	"slices"
	"sync"
	"time"

	"github.com/spachava753/gai"
)

// memNode is a node in the in-memory conversation tree. Each node holds a
// single message and references its children, forming a tree that mirrors
// the branching conversation structure stored in SQLite.
type memNode struct {
	id        string
	parentID  string
	message   gai.Message
	title     string
	createdAt time.Time
	children  []*memNode
}

// MemDB is an in-memory implementation of MessageDB backed by a tree
// structure. It is intended for use in tests as a drop-in replacement for
// the SQLite-backed store.
//
// The underlying data structure is a forest (slice of root nodes). Each root
// node represents the start of a conversation, and child nodes represent
// subsequent messages. This mirrors the parent-child relationship used by
// the SQLite implementation.
type MemDB struct {
	mu    sync.Mutex
	roots []*memNode
	// byID provides O(1) lookup by message ID.
	byID map[string]*memNode
	// nextID is a simple incrementing counter for generating unique IDs.
	nextID int
}

// NewMemDB creates a new empty in-memory message database.
func NewMemDB() *MemDB {
	return &MemDB{
		byID: make(map[string]*memNode),
	}
}

func (m *MemDB) generateID() string {
	m.nextID++
	return fmt.Sprintf("mem_%d", m.nextID)
}

// SaveDialog saves a dialog — a sequence of related messages forming a
// conversation thread. See the DialogSaver interface for full semantics.
func (m *MemDB) SaveDialog(ctx context.Context, msgs iter.Seq[gai.Message]) iter.Seq2[gai.Message, error] {
	return func(yield func(gai.Message, error) bool) {
		m.mu.Lock()
		defer m.mu.Unlock()

		var prevID string
		first := true

		// Track new nodes added in this call so we can roll back on error.
		var added []string

		rollback := func() {
			for _, id := range added {
				node := m.byID[id]
				if node == nil {
					continue
				}
				// Remove from parent's children slice.
				if node.parentID != "" {
					parent := m.byID[node.parentID]
					if parent != nil {
						parent.children = slices.DeleteFunc(parent.children, func(n *memNode) bool {
							return n.id == id
						})
					}
				} else {
					// Remove from roots.
					m.roots = slices.DeleteFunc(m.roots, func(n *memNode) bool {
						return n.id == id
					})
				}
				delete(m.byID, id)
			}
		}

		for msg := range msgs {
			existingID := getExtraFieldString(msg.ExtraFields, MessageIDKey)

			if existingID != "" {
				// Verify existing message.
				node, ok := m.byID[existingID]
				if !ok {
					rollback()
					yield(gai.Message{}, fmt.Errorf("failed to verify message %s exists: not found", existingID))
					return
				}

				if first {
					if node.parentID != "" {
						rollback()
						yield(gai.Message{}, fmt.Errorf("first message %s must be a root message but has parent %q in storage", existingID, node.parentID))
						return
					}
				} else {
					if node.parentID != prevID {
						rollback()
						yield(gai.Message{}, fmt.Errorf("message %s has parent %q in storage but expected %q", existingID, node.parentID, prevID))
						return
					}
				}

				prevID = existingID
				first = false

				if !yield(msg, nil) {
					return
				}
				continue
			}

			// New message — save it.
			id := m.generateID()
			title := getExtraFieldString(msg.ExtraFields, MessageTitleKey)

			node := &memNode{
				id:        id,
				parentID:  prevID,
				message:   msg,
				title:     title,
				createdAt: time.Now(),
			}

			if prevID != "" {
				parent := m.byID[prevID]
				if parent != nil {
					parent.children = append(parent.children, node)
				}
			} else {
				m.roots = append(m.roots, node)
			}
			m.byID[id] = node
			added = append(added, id)

			// Set ExtraFields on the message.
			if msg.ExtraFields == nil {
				msg.ExtraFields = make(map[string]any)
			}
			msg.ExtraFields[MessageIDKey] = id
			if prevID != "" {
				msg.ExtraFields[MessageParentIDKey] = prevID
			}

			prevID = id
			first = false

			if !yield(msg, nil) {
				return
			}
		}
	}
}

// DeleteMessages deletes the specified messages. The entire operation is
// atomic: if any message cannot be deleted, no changes are made. See
// MessagesDeleter for full semantics.
func (m *MemDB) DeleteMessages(ctx context.Context, opts DeleteMessagesOptions) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Validate all messages before mutating anything (atomicity).
	for _, id := range opts.MessageIDs {
		node, ok := m.byID[id]
		if !ok {
			return fmt.Errorf("message %s not found", id)
		}

		if !opts.Recursive && len(node.children) > 0 {
			return fmt.Errorf("cannot delete message with ID %s: message has children", id)
		}
	}

	// All validations passed — now perform the actual deletions.
	for _, id := range opts.MessageIDs {
		node := m.byID[id]
		if node == nil {
			continue // already deleted by a prior recursive deletion in this batch
		}
		m.deleteNode(node, opts.Recursive)
	}
	return nil
}

func (m *MemDB) deleteNode(node *memNode, recursive bool) {
	if recursive {
		// Delete children first (copy slice to avoid mutation during iteration).
		for _, child := range slices.Clone(node.children) {
			m.deleteNode(child, true)
		}
	}

	// Remove from parent's children or from roots.
	if node.parentID != "" {
		parent := m.byID[node.parentID]
		if parent != nil {
			parent.children = slices.DeleteFunc(parent.children, func(n *memNode) bool {
				return n.id == node.id
			})
		}
	} else {
		m.roots = slices.DeleteFunc(m.roots, func(n *memNode) bool {
			return n.id == node.id
		})
	}
	delete(m.byID, node.id)
}

// ListMessages returns messages ordered by creation timestamp. See
// MessagesLister for full semantics.
func (m *MemDB) ListMessages(ctx context.Context, opts ListMessagesOptions) (iter.Seq[gai.Message], error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Collect all nodes.
	all := make([]*memNode, 0, len(m.byID))
	for _, node := range m.byID {
		all = append(all, node)
	}

	// Sort by createdAt.
	slices.SortFunc(all, func(a, b *memNode) int {
		if opts.AscendingOrder {
			return a.createdAt.Compare(b.createdAt)
		}
		return b.createdAt.Compare(a.createdAt)
	})

	// Apply offset.
	offset := int(opts.Offset)
	if offset > len(all) {
		offset = len(all)
	}
	all = all[offset:]

	// Build messages.
	msgs := make([]gai.Message, 0, len(all))
	for _, node := range all {
		msgs = append(msgs, m.nodeToMessage(node))
	}

	return slices.Values(msgs), nil
}

// GetMessages retrieves messages by their IDs. See MessagesGetter for full
// semantics.
func (m *MemDB) GetMessages(ctx context.Context, messageIDs []string) (iter.Seq[gai.Message], error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	msgs := make([]gai.Message, 0, len(messageIDs))
	for _, id := range messageIDs {
		node, ok := m.byID[id]
		if !ok {
			return nil, fmt.Errorf("message %s not found", id)
		}
		msgs = append(msgs, m.nodeToMessage(node))
	}

	return slices.Values(msgs), nil
}

// nodeToMessage converts a memNode to a gai.Message with ExtraFields populated.
// Blocks are shallow-copied so callers cannot mutate stored data.
func (m *MemDB) nodeToMessage(node *memNode) gai.Message {
	msg := node.message

	// Copy the blocks slice so mutations by callers don't affect storage.
	if len(msg.Blocks) > 0 {
		blocks := make([]gai.Block, len(msg.Blocks))
		copy(blocks, msg.Blocks)
		msg.Blocks = blocks
	}

	// Create a fresh ExtraFields map with storage metadata.
	extra := make(map[string]any)
	extra[MessageIDKey] = node.id
	extra[MessageCreatedAtKey] = node.createdAt
	if node.parentID != "" {
		extra[MessageParentIDKey] = node.parentID
	}
	if node.title != "" {
		extra[MessageTitleKey] = node.title
	}
	msg.ExtraFields = extra
	return msg
}

// Nodes returns all nodes in the tree for test assertions. Each returned
// MemNode is a snapshot — mutations do not affect the MemDB.
func (m *MemDB) Nodes() []MemNode {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([]MemNode, 0, len(m.byID))
	for _, node := range m.byID {
		childIDs := make([]string, len(node.children))
		for i, c := range node.children {
			childIDs[i] = c.id
		}
		result = append(result, MemNode{
			ID:        node.id,
			ParentID:  node.parentID,
			Role:      node.message.Role,
			Blocks:    node.message.Blocks,
			Title:     node.title,
			CreatedAt: node.createdAt,
			ChildIDs:  childIDs,
		})
	}
	return result
}

// MemNode is a test-visible snapshot of a node in the MemDB tree.
type MemNode struct {
	ID        string
	ParentID  string
	Role      gai.Role
	Blocks    []gai.Block
	Title     string
	CreatedAt time.Time
	ChildIDs  []string
}
