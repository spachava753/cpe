package storage

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/bradleyjkemp/cupaloy/v2"
	_ "github.com/mattn/go-sqlite3"
	"github.com/spachava753/gai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestDB creates an in-memory SQLite database for testing
func setupTestDB(t *testing.T) (*sql.DB, *DialogStorage) {
	// Create an in-memory database
	db, err := sql.Open("sqlite3", ":memory:")
	//db, err := sql.Open("sqlite3", "./test_db")
	require.NoError(t, err, "Failed to open in-memory database")

	// Execute schema - enable foreign keys
	_, err = db.ExecContext(context.Background(), "PRAGMA foreign_keys = ON;")
	require.NoError(t, err, "Failed to enable foreign keys")

	// Execute schema - create tables
	_, err = db.ExecContext(context.Background(), schemaSQL)
	require.NoError(t, err, "Failed to create schema")

	// Create dialog storage
	storage := &DialogStorage{
		db:          db,
		q:           New(db),
		idGenerator: generateId,
	}

	return db, storage
}

// normalizeMessage returns a copy of the message with IDs cleared for snapshot comparison
func normalizeMessage(msg gai.Message) gai.Message {
	normalized := gai.Message{
		Role:            msg.Role,
		ToolResultError: msg.ToolResultError,
	}
	normalized.Blocks = make([]gai.Block, len(msg.Blocks))
	for i, block := range msg.Blocks {
		normalized.Blocks[i] = gai.Block{
			ID:           "", // Clear the ID
			BlockType:    block.BlockType,
			ModalityType: block.ModalityType,
			MimeType:     block.MimeType,
			Content:      block.Content,
		}
	}
	return normalized
}

// normalizeDialog returns a copy of the dialog with IDs cleared for snapshot comparison
func normalizeDialog(dialog gai.Dialog) gai.Dialog {
	result := make(gai.Dialog, len(dialog))
	for i, msg := range dialog {
		result[i] = normalizeMessage(msg)
	}
	return result
}

// createTextMessage is a helper function to create a simple text message
func createTextMessage(role gai.Role, content string) gai.Message {
	return gai.Message{
		Role: role,
		Blocks: []gai.Block{
			{
				BlockType:    gai.Content,
				ModalityType: gai.Text,
				MimeType:     "text/plain",
				Content:      gai.Str(content),
			},
		},
	}
}

// TestStoreAndRetrieveMessages tests basic message storage and retrieval
func TestStoreAndRetrieveMessages(t *testing.T) {
	db, storage := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()

	// Create a couple of messages
	userMsg := createTextMessage(gai.User, "Hello, AI!")
	assistantMsg := createTextMessage(gai.Assistant, "Hello, human! How can I help you today?")

	// Save messages
	userID, err := storage.SaveMessage(ctx, userMsg, "", "Test Conversation")
	require.NoError(t, err, "Failed to save user message")

	assistantID, err := storage.SaveMessage(ctx, assistantMsg, userID, "")
	require.NoError(t, err, "Failed to save assistant message")

	// Retrieve the individual messages directly
	retrievedUserMsg, _, err := storage.GetMessage(ctx, userID)
	require.NoError(t, err, "Failed to get user message")
	retrievedAssistantMsg, parentID, err := storage.GetMessage(ctx, assistantID)
	require.NoError(t, err, "Failed to get assistant message")

	// Check parent ID
	assert.Equal(t, userID, parentID, "Assistant message parent ID should match user ID")

	// Verify individual messages using snapshots
	cupaloy.SnapshotT(t, normalizeMessage(retrievedUserMsg), normalizeMessage(retrievedAssistantMsg))
}

// TestGetLatestMessage tests retrieving the most recent message
func TestGetLatestMessage(t *testing.T) {
	db, storage := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()

	// Create messages with a small delay between them
	msg1 := createTextMessage(gai.User, "First message")
	msg2 := createTextMessage(gai.Assistant, "Second message")
	msg3 := createTextMessage(gai.Assistant, "Third message")

	_, err := storage.SaveMessage(ctx, msg1, "", "Test Conversation")
	require.NoError(t, err, "Failed to save first message")

	_, err = storage.SaveMessage(ctx, msg2, "", "")
	require.NoError(t, err, "Failed to save second message")

	// Save the third message which should be the latest
	_, err = storage.SaveMessage(ctx, msg3, "", "")
	require.NoError(t, err, "Failed to save third message")

	// Get the latest message ID
	latestID, err := storage.GetMostRecentAssistantMessageId(ctx)
	require.NoError(t, err, "Failed to get latest message ID")

	// Get the message using the ID
	latestMsg, _, err := storage.GetMessage(ctx, latestID)
	require.NoError(t, err, "Failed to get message by ID")

	// Verify it's the third message using snapshot
	cupaloy.SnapshotT(t, normalizeMessage(latestMsg))
}

// TestDeleteMessageWithoutChildren tests deletion of a leaf message
func TestDeleteMessageWithoutChildren(t *testing.T) {
	db, storage := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()

	// Create a simple conversation
	userMsg := createTextMessage(gai.User, "Hello")
	userID, err := storage.SaveMessage(ctx, userMsg, "", "Test Conversation")
	require.NoError(t, err, "Failed to save user message")

	assistantMsg := createTextMessage(gai.Assistant, "Hi there!")
	assistantID, err := storage.SaveMessage(ctx, assistantMsg, userID, "")
	require.NoError(t, err, "Failed to save assistant message")

	// Verify both messages exist
	messageNodes, err := storage.ListMessages(ctx)
	require.NoError(t, err, "Failed to list messages")
	assert.Len(t, messageNodes, 1, "Should have 1 root message initially")
	assert.Len(t, messageNodes[0].Children, 1, "Root should have 1 child")

	// Delete the leaf message (assistant's reply)
	err = storage.DeleteMessage(ctx, assistantID)
	require.NoError(t, err, "Failed to delete leaf message")

	// Verify only one message remains
	messageNodes, err = storage.ListMessages(ctx)
	require.NoError(t, err, "Failed to list messages after deletion")
	assert.Len(t, messageNodes, 1, "Should have 1 root message after deletion")
	assert.Len(t, messageNodes[0].Children, 0, "Root should have no children after deletion")
	assert.Equal(t, userID, messageNodes[0].ID, "Remaining message should be the user message")

	// Verify trying to get the deleted message returns an error
	_, _, err = storage.GetMessage(ctx, assistantID)
	assert.Error(t, err, "Getting deleted message should return an error")
}

// TestDeleteMessageWithChildren tests that we can't delete a message with children
func TestDeleteMessageWithChildren(t *testing.T) {
	db, storage := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()

	// Create a simple conversation
	userMsg := createTextMessage(gai.User, "Hello")
	userID, err := storage.SaveMessage(ctx, userMsg, "", "Test Conversation")
	require.NoError(t, err, "Failed to save user message")

	assistantMsg := createTextMessage(gai.Assistant, "Hi there!")
	_, err = storage.SaveMessage(ctx, assistantMsg, userID, "")
	require.NoError(t, err, "Failed to save assistant message")

	// Try to delete the parent message (user's message)
	err = storage.DeleteMessage(ctx, userID)
	assert.Error(t, err, "Deleting a message with children should return an error")
	assert.Contains(t, err.Error(), "has children", "Error should mention the message has children")

	// Verify both messages still exist
	messageNodes, err := storage.ListMessages(ctx)
	require.NoError(t, err, "Failed to list messages")
	assert.Len(t, messageNodes, 1, "Should have 1 root message")
	assert.Len(t, messageNodes[0].Children, 1, "Root should have 1 child")
}

// TestDeleteMessageRecursive tests recursive deletion of a message and its children
func TestDeleteMessageRecursive(t *testing.T) {
	db, storage := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()

	// Create a branching conversation
	rootMsg := createTextMessage(gai.User, "Root message")
	rootID, err := storage.SaveMessage(ctx, rootMsg, "", "Test Conversation")
	require.NoError(t, err, "Failed to save root message")

	// Add two child messages
	child1Msg := createTextMessage(gai.Assistant, "Child 1 message")
	child1ID, err := storage.SaveMessage(ctx, child1Msg, rootID, "")
	require.NoError(t, err, "Failed to save child 1 message")

	child2Msg := createTextMessage(gai.Assistant, "Child 2 message")
	child2ID, err := storage.SaveMessage(ctx, child2Msg, rootID, "")
	require.NoError(t, err, "Failed to save child 2 message")

	// Add a grandchild message
	grandchildMsg := createTextMessage(gai.User, "Grandchild message")
	grandchildID, err := storage.SaveMessage(ctx, grandchildMsg, child2ID, "")
	require.NoError(t, err, "Failed to save grandchild message")

	// Verify we have the correct tree structure
	messageNodes, err := storage.ListMessages(ctx)
	require.NoError(t, err, "Failed to list messages")
	assert.Len(t, messageNodes, 1, "Should have 1 root node")
	assert.Len(t, messageNodes[0].Children, 2, "Root should have 2 children")

	// Find child2 (the one with a grandchild)
	var child2Node MessageIdNode
	for _, child := range messageNodes[0].Children {
		if child.ID == child2ID {
			child2Node = child
			break
		}
	}
	assert.Len(t, child2Node.Children, 1, "Child2 should have 1 grandchild")

	// Delete child2 and its descendants recursively
	err = storage.DeleteMessageRecursive(ctx, child2ID)
	require.NoError(t, err, "Failed to recursively delete message")

	// Verify updated tree structure
	messageNodes, err = storage.ListMessages(ctx)
	require.NoError(t, err, "Failed to list messages after deletion")
	assert.Len(t, messageNodes, 1, "Should have 1 root node")
	assert.Len(t, messageNodes[0].Children, 1, "Root should have 1 child after deletion")
	assert.Equal(t, child1ID, messageNodes[0].Children[0].ID, "Remaining child should be child1")

	// Verify we can still access root and child1
	_, _, err = storage.GetMessage(ctx, rootID)
	assert.NoError(t, err, "Root message should still exist")

	_, _, err = storage.GetMessage(ctx, child1ID)
	assert.NoError(t, err, "Child 1 message should still exist")

	// Verify child2 and grandchild are gone
	_, _, err = storage.GetMessage(ctx, child2ID)
	assert.Error(t, err, "Child 2 message should be deleted")

	_, _, err = storage.GetMessage(ctx, grandchildID)
	assert.Error(t, err, "Grandchild message should be deleted")
}

// TestBranchingDialogs tests creating and retrieving branching conversations
func TestBranchingDialogs(t *testing.T) {
	db, storage := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()

	// Create a conversation base
	rootMsg := createTextMessage(gai.User, "Hello, AI!")
	rootID, err := storage.SaveMessage(ctx, rootMsg, "", "Test Conversation")
	require.NoError(t, err, "Failed to save root message")

	baseReply := createTextMessage(gai.Assistant, "Hello! How can I help you today?")
	baseReplyID, err := storage.SaveMessage(ctx, baseReply, rootID, "")
	require.NoError(t, err, "Failed to save base reply")

	// Create branch 1
	branch1Msg := createTextMessage(gai.User, "Tell me about cats.")
	branch1ID, err := storage.SaveMessage(ctx, branch1Msg, baseReplyID, "Cats")
	require.NoError(t, err, "Failed to save branch 1 message")

	branch1Reply := createTextMessage(gai.Assistant, "Cats are wonderful pets...")
	_, err = storage.SaveMessage(ctx, branch1Reply, branch1ID, "")
	require.NoError(t, err, "Failed to save branch 1 reply")

	// Create branch 2 (from the same base reply)
	branch2Msg := createTextMessage(gai.User, "Tell me about dogs.")
	branch2ID, err := storage.SaveMessage(ctx, branch2Msg, baseReplyID, "Dogs")
	require.NoError(t, err, "Failed to save branch 2 message")

	branch2Reply := createTextMessage(gai.Assistant, "Dogs are loyal companions...")
	_, err = storage.SaveMessage(ctx, branch2Reply, branch2ID, "")
	require.NoError(t, err, "Failed to save branch 2 reply")

	// Get the dialog from branch 1 leaf
	dialog1, msgList1, err := storage.GetDialogForMessage(ctx, branch1ID)
	require.NoError(t, err, "Failed to get dialog 1")

	// Print for debugging
	fmt.Println("Dialog 1 (Cats branch):")
	for i, msg := range dialog1 {
		content := ""
		if len(msg.Blocks) > 0 {
			content = msg.Blocks[0].Content.String()
		}
		fmt.Printf("  %d. Role: %v, Content: %s\n", i, msg.Role, content)
	}

	// Get the dialog from branch 2 leaf
	dialog2, msgList2, err := storage.GetDialogForMessage(ctx, branch2ID)
	require.NoError(t, err, "Failed to get dialog 2")

	// Print for debugging
	fmt.Println("Dialog 2 (Dogs branch):")
	for i, msg := range dialog2 {
		content := ""
		if len(msg.Blocks) > 0 {
			content = msg.Blocks[0].Content.String()
		}
		fmt.Printf("  %d. Role: %v, Content: %s\n", i, msg.Role, content)
	}

	// Verify dialogs using snapshots
	cupaloy.SnapshotT(t, normalizeDialog(dialog1), normalizeDialog(dialog2))

	// Verify message list lengths match expected
	assert.Len(t, msgList1, 3, "Branch 1 should have 3 messages in path")
	assert.Len(t, msgList2, 3, "Branch 2 should have 3 messages in path")

	// Verify the message lists contain the expected IDs
	assert.Equal(t, rootID, msgList1[0], "First message in branch 1 should be root")
	assert.Equal(t, baseReplyID, msgList1[1], "Second message in branch 1 should be base reply")
	assert.Equal(t, branch1ID, msgList1[2], "Third message in branch 1 should be branch1")

	assert.Equal(t, rootID, msgList2[0], "First message in branch 2 should be root")
	assert.Equal(t, baseReplyID, msgList2[1], "Second message in branch 2 should be base reply")
	assert.Equal(t, branch2ID, msgList2[2], "Third message in branch 2 should be branch2")

	// Verify shared path and branches
	assert.Equal(t, dialog1[:2], dialog2[:2], "Parent messages should be identical")
	assert.NotEqual(t, dialog1[2].Blocks[0].Content.String(), dialog2[2].Blocks[0].Content.String(), "Branch messages should differ")
}
