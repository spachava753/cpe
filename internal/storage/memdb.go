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
	id         string
	seq        int64
	parentID   string
	message    gai.Message
	isSubagent bool
	createdAt  time.Time
	children   []*memNode
}

// MemDB is an in-memory MessageDB implementation used primarily in tests.
//
// It mirrors SQLite semantics for parent-chain validation, dialog persistence,
// and metadata population while storing data in a mutex-protected forest.
// Each root node represents a conversation start; descendants represent
// continued turns.
type MemDB struct {
	mu    sync.Mutex
	roots []*memNode
	// byID provides O(1) lookup by message ID.
	byID map[string]*memNode
	// nextID is a simple incrementing counter for generating unique IDs.
	nextID int
	// nextSeq is a monotonic insertion sequence used as a stable tie-breaker.
	nextSeq int64
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

// SaveDialog validates and persists a root-to-leaf chain in memory.
//
// It mirrors the DialogSaver contract used by Sqlite:
//   - Existing message IDs are validated for existence and parent continuity.
//   - New messages are linked to the previously processed message.
//   - Validation/write errors roll back writes from this call.
//
// If the consumer stops iteration early, already processed messages remain
// persisted and the remaining input is not read.
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
			isSubagent := getExtraFieldBool(msg.ExtraFields, MessageIsSubagentKey)
			m.nextSeq++

			node := &memNode{
				id:         id,
				seq:        m.nextSeq,
				parentID:   prevID,
				message:    msg,
				isSubagent: isSubagent,
				createdAt:  time.Now(),
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

// DeleteMessages removes messages according to DeleteMessagesOptions.
//
// The operation is atomic across opts.MessageIDs: it first validates every
// target, then performs deletions. Non-existent IDs are ignored.
func (m *MemDB) DeleteMessages(ctx context.Context, opts DeleteMessagesOptions) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Validate all existing messages before mutating anything (atomicity).
	for _, id := range opts.MessageIDs {
		node, ok := m.byID[id]
		if !ok {
			continue
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

// ListMessages returns a snapshot of messages ordered by createdAt.
//
// When timestamps tie, insertion sequence is used as a deterministic
// tie-breaker so tests do not rely on map iteration order.
func (m *MemDB) ListMessages(ctx context.Context, opts ListMessagesOptions) (iter.Seq[gai.Message], error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Collect all nodes.
	all := make([]*memNode, 0, len(m.byID))
	for _, node := range m.byID {
		all = append(all, node)
	}

	// Sort by createdAt with insertion-sequence tie-breakers for determinism.
	slices.SortFunc(all, func(a, b *memNode) int {
		cmp := a.createdAt.Compare(b.createdAt)
		if cmp == 0 {
			if opts.AscendingOrder {
				if a.seq < b.seq {
					return -1
				}
				if a.seq > b.seq {
					return 1
				}
				return 0
			}
			if a.seq > b.seq {
				return -1
			}
			if a.seq < b.seq {
				return 1
			}
			return 0
		}
		if opts.AscendingOrder {
			return cmp
		}
		return -cmp
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

// GetMessages retrieves a snapshot for each requested ID.
//
// The call fails if any ID is missing. Returned messages include cloned blocks
// and storage metadata keys in ExtraFields.
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

func cloneExtraFieldsMap(extra map[string]any) map[string]any {
	if extra == nil {
		return nil
	}
	cloned := make(map[string]any, len(extra))
	for k, v := range extra {
		cloned[k] = v
	}
	return cloned
}

func cloneBlocks(blocks []gai.Block) []gai.Block {
	if len(blocks) == 0 {
		return nil
	}
	cloned := make([]gai.Block, len(blocks))
	for i := range blocks {
		cloned[i] = blocks[i]
		cloned[i].ExtraFields = cloneExtraFieldsMap(blocks[i].ExtraFields)
	}
	return cloned
}

// nodeToMessage converts a node to an immutable-ish read snapshot.
//
// It clones block data and replaces message-level ExtraFields with storage
// metadata keys so callers observe the same shape as Sqlite reads.
func (m *MemDB) nodeToMessage(node *memNode) gai.Message {
	msg := node.message
	msg.Blocks = cloneBlocks(msg.Blocks)

	// Create a fresh ExtraFields map with storage metadata.
	extra := make(map[string]any)
	extra[MessageIDKey] = node.id
	extra[MessageCreatedAtKey] = node.createdAt
	extra[MessageIsSubagentKey] = node.isSubagent
	if node.parentID != "" {
		extra[MessageParentIDKey] = node.parentID
	}
	msg.ExtraFields = extra
	return msg
}

// Nodes returns test snapshots of all currently stored nodes.
//
// Result order is unspecified. Each MemNode is detached from internal state;
// mutating the returned slice or entries does not affect MemDB.
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
			ID:         node.id,
			ParentID:   node.parentID,
			Role:       node.message.Role,
			Blocks:     cloneBlocks(node.message.Blocks),
			IsSubagent: node.isSubagent,
			CreatedAt:  node.createdAt,
			ChildIDs:   childIDs,
		})
	}
	return result
}

// MemNode is a test-visible snapshot of one stored message node.
type MemNode struct {
	// ID is the message identifier.
	ID string
	// ParentID is the parent message ID, or "" for roots.
	ParentID string
	// Role is the gai role stored for this message.
	Role gai.Role
	// Blocks is a cloned copy of the stored blocks.
	Blocks []gai.Block
	// IsSubagent reports whether the message was marked as subagent-originated.
	IsSubagent bool
	// CreatedAt is the node creation timestamp used for list ordering.
	CreatedAt time.Time
	// ChildIDs contains direct-child message IDs.
	ChildIDs []string
}
