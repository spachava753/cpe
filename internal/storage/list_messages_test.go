package storage

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/spachava753/gai"
	"github.com/stretchr/testify/require"
)

// TestHardcodedTreeStructures tests ListMessages with hardcoded test expectations
func TestHardcodedTreeStructures(t *testing.T) {
	testCases := []struct {
		name         string
		setupFunc    func(ctx context.Context, storage *DialogStorage)
		expectedTree []MessageIdNode
	}{
		{
			name: "Empty database",
			setupFunc: func(ctx context.Context, storage *DialogStorage) {
				// Do nothing - empty database
			},
			expectedTree: []MessageIdNode{},
		},
		{
			name: "Single root message",
			setupFunc: func(ctx context.Context, storage *DialogStorage) {
				// Set id generator to return a known ID
				nextID := "root1"
				storage.idGenerator = func() string { return nextID }

				msg := createTextMessage(gai.User, "Single root message")
				storage.SaveMessage(ctx, msg, "", "Single")
			},
			expectedTree: []MessageIdNode{
				{
					ID:        "root1",
					ParentID:  "",
					CreatedAt: time.Time{}, // Timestamp unchecked in test
					Children:  []MessageIdNode{},
				},
			},
		},
		{
			name: "Root with multiple children",
			setupFunc: func(ctx context.Context, storage *DialogStorage) {
				// Create ID sequence
				ids := []string{"root1", "child1", "child2", "child3"}
				idIndex := 0
				storage.idGenerator = func() string {
					id := ids[idIndex]
					idIndex++
					return id
				}

				// Create root
				rootMsg := createTextMessage(gai.User, "Root message")
				rootID, _ := storage.SaveMessage(ctx, rootMsg, "", "Multiple Children")

				// Create three children
				for i := 1; i <= 3; i++ {
					childMsg := createTextMessage(gai.Assistant, "Child message")
					storage.SaveMessage(ctx, childMsg, rootID, "")
				}
			},
			expectedTree: []MessageIdNode{
				{
					ID:       "root1",
					ParentID: "",
					Children: []MessageIdNode{
						{
							ID:       "child1",
							ParentID: "root1",
							Children: []MessageIdNode{},
						},
						{
							ID:       "child2",
							ParentID: "root1",
							Children: []MessageIdNode{},
						},
						{
							ID:       "child3",
							ParentID: "root1",
							Children: []MessageIdNode{},
						},
					},
				},
			},
		},
		{
			name: "Two-level tree (grandchildren)",
			setupFunc: func(ctx context.Context, storage *DialogStorage) {
				// Create ID sequence
				ids := []string{"root1", "child1", "grandchild1", "grandchild2"}
				idIndex := 0
				storage.idGenerator = func() string {
					id := ids[idIndex]
					idIndex++
					return id
				}

				// Create root
				rootMsg := createTextMessage(gai.User, "Root message")
				rootID, _ := storage.SaveMessage(ctx, rootMsg, "", "Two-level Tree")

				// Create child
				childMsg := createTextMessage(gai.Assistant, "Child message")
				childID, _ := storage.SaveMessage(ctx, childMsg, rootID, "")

				// Create grandchildren
				for i := 1; i <= 2; i++ {
					grandchildMsg := createTextMessage(gai.User, "Grandchild message")
					storage.SaveMessage(ctx, grandchildMsg, childID, "")
				}
			},
			expectedTree: []MessageIdNode{
				{
					ID:       "root1",
					ParentID: "",
					Children: []MessageIdNode{
						{
							ID:       "child1",
							ParentID: "root1",
							Children: []MessageIdNode{
								{
									ID:       "grandchild1",
									ParentID: "child1",
									Children: []MessageIdNode{},
								},
								{
									ID:       "grandchild2",
									ParentID: "child1",
									Children: []MessageIdNode{},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Multiple branches with varying depth",
			setupFunc: func(ctx context.Context, storage *DialogStorage) {
				// Create ID sequence in specific order to match hierarchy
				ids := []string{"root1", "child1", "child2", "grandchild1", "greatgrandchild1", "grandchild2"}
				idIndex := 0
				storage.idGenerator = func() string {
					id := ids[idIndex]
					idIndex++
					return id
				}

				// Create root
				rootMsg := createTextMessage(gai.User, "Root message")
				rootID, _ := storage.SaveMessage(ctx, rootMsg, "", "Complex Tree")

				// Create first child (no grandchildren)
				child1Msg := createTextMessage(gai.Assistant, "Child 1 message")
				storage.SaveMessage(ctx, child1Msg, rootID, "")

				// Create second child (with grandchildren)
				child2Msg := createTextMessage(gai.Assistant, "Child 2 message")
				child2ID, _ := storage.SaveMessage(ctx, child2Msg, rootID, "")

				// Create first grandchild (with great-grandchild)
				grandchild1Msg := createTextMessage(gai.User, "Grandchild 1 message")
				grandchild1ID, _ := storage.SaveMessage(ctx, grandchild1Msg, child2ID, "")

				// Create great-grandchild
				greatGrandchildMsg := createTextMessage(gai.Assistant, "Great-grandchild message")
				storage.SaveMessage(ctx, greatGrandchildMsg, grandchild1ID, "")

				// Create second grandchild (no great-grandchildren)
				grandchild2Msg := createTextMessage(gai.User, "Grandchild 2 message")
				storage.SaveMessage(ctx, grandchild2Msg, child2ID, "")
			},
			expectedTree: []MessageIdNode{
				{
					ID:       "root1",
					ParentID: "",
					Children: []MessageIdNode{
						{
							ID:       "child1",
							ParentID: "root1",
							Children: []MessageIdNode{},
						},
						{
							ID:       "child2",
							ParentID: "root1",
							Children: []MessageIdNode{
								{
									ID:       "grandchild1",
									ParentID: "child2",
									Children: []MessageIdNode{
										{
											ID:       "greatgrandchild1",
											ParentID: "grandchild1",
											Children: []MessageIdNode{},
										},
									},
								},
								{
									ID:       "grandchild2",
									ParentID: "child2",
									Children: []MessageIdNode{},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Multiple roots",
			setupFunc: func(ctx context.Context, storage *DialogStorage) {
				// Create ID sequence
				ids := []string{"root1", "child1", "root2", "child2", "grandchild1"}
				idIndex := 0
				storage.idGenerator = func() string {
					id := ids[idIndex]
					idIndex++
					return id
				}

				// Create first root
				root1Msg := createTextMessage(gai.User, "Root 1 message")
				root1ID, _ := storage.SaveMessage(ctx, root1Msg, "", "First Root")

				// Child for first root
				child1Msg := createTextMessage(gai.Assistant, "Child of Root 1")
				storage.SaveMessage(ctx, child1Msg, root1ID, "")

				// Create second root
				root2Msg := createTextMessage(gai.User, "Root 2 message")
				root2ID, _ := storage.SaveMessage(ctx, root2Msg, "", "Second Root")

				// Child for second root
				child2Msg := createTextMessage(gai.Assistant, "Child of Root 2")
				child2ID, _ := storage.SaveMessage(ctx, child2Msg, root2ID, "")

				// Grandchild for second root
				grandchildMsg := createTextMessage(gai.User, "Grandchild of Root 2")
				storage.SaveMessage(ctx, grandchildMsg, child2ID, "")
			},
			expectedTree: []MessageIdNode{
				{
					ID:       "root1",
					ParentID: "",
					Children: []MessageIdNode{
						{
							ID:       "child1",
							ParentID: "root1",
							Children: []MessageIdNode{},
						},
					},
				},
				{
					ID:       "root2",
					ParentID: "",
					Children: []MessageIdNode{
						{
							ID:       "child2",
							ParentID: "root2",
							Children: []MessageIdNode{
								{
									ID:       "grandchild1",
									ParentID: "child2",
									Children: []MessageIdNode{},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Balanced binary tree",
			setupFunc: func(ctx context.Context, storage *DialogStorage) {
				// First create all messages in order
				rootMsg := createTextMessage(gai.User, "Root message")
				child1Msg := createTextMessage(gai.Assistant, "Child 1 message")
				child2Msg := createTextMessage(gai.Assistant, "Child 2 message")
				gc1Msg := createTextMessage(gai.User, "Grandchild 1.1 message")
				gc2Msg := createTextMessage(gai.User, "Grandchild 1.2 message")
				gc3Msg := createTextMessage(gai.User, "Grandchild 2.1 message")
				gc4Msg := createTextMessage(gai.User, "Grandchild 2.2 message")

				// Then set up exact IDs we want
				ids := []string{"root1", "child1", "child2", "grandchild1", "grandchild2", "grandchild3", "grandchild4"}
				storage.idGenerator = func() string {
					if len(ids) == 0 {
						return "error"
					}
					id := ids[0]
					ids = ids[1:]
					return id
				}

				// Now save them in the correct order
				rootID, _ := storage.SaveMessage(ctx, rootMsg, "", "Binary Tree")
				child1ID, _ := storage.SaveMessage(ctx, child1Msg, rootID, "")
				child2ID, _ := storage.SaveMessage(ctx, child2Msg, rootID, "")
				storage.SaveMessage(ctx, gc1Msg, child1ID, "")
				storage.SaveMessage(ctx, gc2Msg, child1ID, "")
				storage.SaveMessage(ctx, gc3Msg, child2ID, "")
				storage.SaveMessage(ctx, gc4Msg, child2ID, "")
			},
			expectedTree: []MessageIdNode{
				{
					ID:       "root1",
					ParentID: "",
					Children: []MessageIdNode{
						{
							ID:       "child1",
							ParentID: "root1",
							Children: []MessageIdNode{
								{
									ID:       "grandchild1",
									ParentID: "child1",
									Children: []MessageIdNode{},
								},
								{
									ID:       "grandchild2",
									ParentID: "child1",
									Children: []MessageIdNode{},
								},
							},
						},
						{
							ID:       "child2",
							ParentID: "root1",
							Children: []MessageIdNode{
								{
									ID:       "grandchild3",
									ParentID: "child2",
									Children: []MessageIdNode{},
								},
								{
									ID:       "grandchild4",
									ParentID: "child2",
									Children: []MessageIdNode{},
								},
							},
						},
					},
				},
			},
		},
	}

	// (Other cases omitted for brevity)

	// Update matching in test to ignore CreatedAt field for now
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Setup fresh database for each test
			db, storage := setupTestDB(t)
			defer db.Close()

			ctx := context.Background()

			// Setup the tree structure with predefined IDs
			tc.setupFunc(ctx, storage)

			// Get the actual tree
			actual, err := storage.ListMessages(ctx)
			require.NoError(t, err, "Failed to list messages")

			// Sort roots for consistent ordering
			actual = sortNodesByID(actual)

			cmpOpts := []cmp.Option{
				cmpopts.IgnoreFields(MessageIdNode{}, "CreatedAt", "Content", "Role"),
				cmpopts.EquateEmpty(),
			}
			if diff := cmp.Diff(tc.expectedTree, actual, cmpOpts...); diff != "" {
				t.Errorf("Tree structures don't match (-expected +actual):\n%s", diff)
			}
		})
	}
}

// sortNodesByID sorts nodes by ID for deterministic comparison
func sortNodesByID(nodes []MessageIdNode) []MessageIdNode {
	if len(nodes) == 0 {
		return nodes
	}

	result := make([]MessageIdNode, len(nodes))
	copy(result, nodes)

	// Sort by ID
	for i := range result {
		for j := i + 1; j < len(result); j++ {
			if result[i].ID > result[j].ID {
				result[i], result[j] = result[j], result[i]
			}
		}

		// Recursively sort children
		if len(result[i].Children) > 0 {
			result[i].Children = sortNodesByID(result[i].Children)
		}
	}

	return result
}
