package storage

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
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

	// Read schema content
	schema := `
	CREATE TABLE messages
	(
		id                TEXT PRIMARY KEY,
		parent_id         TEXT,
		title             TEXT,
		role              TEXT     NOT NULL,
		tool_result_error BOOLEAN  NOT NULL DEFAULT 0,
		created_at        TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (parent_id) REFERENCES messages (id) ON DELETE RESTRICT
	);
	
	CREATE INDEX idx_messages_created_at ON messages(created_at);
	CREATE INDEX idx_messages_parent_id ON messages(parent_id);
	
	CREATE TABLE blocks
	(
		id             TEXT PRIMARY KEY,
		message_id     TEXT      NOT NULL,
		block_type     TEXT      NOT NULL,
		modality_type  INTEGER   NOT NULL,
		mime_type      TEXT      NOT NULL,
		content        TEXT      NOT NULL,
		sequence_order INTEGER   NOT NULL,
		created_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (message_id) REFERENCES messages (id) ON DELETE CASCADE
	);
	`

	// Execute schema - enable foreign keys
	_, err = db.Exec("PRAGMA foreign_keys = ON;")
	require.NoError(t, err, "Failed to enable foreign keys")

	// Execute schema - create tables
	_, err = db.Exec(schema)
	require.NoError(t, err, "Failed to create schema")

	// Create dialog storage
	storage, err := NewDialogStorage(db)
	require.NoError(t, err, "Failed to create DialogStorage")

	return db, storage
}

// Define a custom comparer for messages that ignores IDs
var messageComparer = cmp.Comparer(func(a, b gai.Message) bool {
	// Compare roles
	if a.Role != b.Role {
		return false
	}

	// Compare tool result error
	if a.ToolResultError != b.ToolResultError {
		return false
	}

	// Compare blocks (ignoring IDs)
	if len(a.Blocks) != len(b.Blocks) {
		return false
	}

	for i := range a.Blocks {
		blockA := a.Blocks[i]
		blockB := b.Blocks[i]

		// Compare all fields except ID
		if blockA.BlockType != blockB.BlockType ||
			blockA.ModalityType != blockB.ModalityType ||
			blockA.MimeType != blockB.MimeType ||
			blockA.Content.String() != blockB.Content.String() {
			return false
		}
	}

	return true
})

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
	retrievedUserMsg, err := storage.GetMessage(ctx, userID)
	require.NoError(t, err, "Failed to get user message")
	retrievedAssistantMsg, err := storage.GetMessage(ctx, assistantID)
	require.NoError(t, err, "Failed to get assistant message")

	// Verify individual messages using go-cmp with custom comparer
	if diff := cmp.Diff(userMsg, retrievedUserMsg, messageComparer); diff != "" {
		t.Errorf("User message mismatch (-want +got):\n%s", diff)
	}

	if diff := cmp.Diff(assistantMsg, retrievedAssistantMsg, messageComparer); diff != "" {
		t.Errorf("Assistant message mismatch (-want +got):\n%s", diff)
	}
}

// TestGetLatestMessage tests retrieving the most recent message
func TestGetLatestMessage(t *testing.T) {
	db, storage := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()

	// Create messages with a small delay between them
	msg1 := createTextMessage(gai.User, "First message")
	msg2 := createTextMessage(gai.Assistant, "Second message")
	msg3 := createTextMessage(gai.User, "Third message")

	_, err := storage.SaveMessage(ctx, msg1, "", "Test Conversation")
	require.NoError(t, err, "Failed to save first message")

	_, err = storage.SaveMessage(ctx, msg2, "", "")
	require.NoError(t, err, "Failed to save second message")

	// Save the third message which should be the latest
	_, err = storage.SaveMessage(ctx, msg3, "", "")
	require.NoError(t, err, "Failed to save third message")

	// Get the latest message ID
	latestID, err := storage.GetMostRecentUserMessageId(ctx)
	require.NoError(t, err, "Failed to get latest message ID")

	// Get the message using the ID
	latestMsg, err := storage.GetMessage(ctx, latestID)
	require.NoError(t, err, "Failed to get message by ID")

	// Verify it's the third message using go-cmp
	if diff := cmp.Diff(msg3, latestMsg, messageComparer); diff != "" {
		t.Errorf("Latest message mismatch (-want +got):\n%s", diff)
	}

	// Verify we can retrieve it by ID
	retrievedMsg, err := storage.GetMessage(ctx, latestID)
	require.NoError(t, err, "Failed to get message by ID")

	if diff := cmp.Diff(msg3, retrievedMsg, messageComparer); diff != "" {
		t.Errorf("Retrieved message mismatch (-want +got):\n%s", diff)
	}
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
	messages, err := storage.ListMessages(ctx)
	require.NoError(t, err, "Failed to list messages")
	assert.Len(t, messages, 2, "Should have 2 messages initially")

	// Delete the leaf message (assistant's reply)
	err = storage.DeleteMessage(ctx, assistantID)
	require.NoError(t, err, "Failed to delete leaf message")

	// Verify only one message remains
	messages, err = storage.ListMessages(ctx)
	require.NoError(t, err, "Failed to list messages after deletion")
	assert.Len(t, messages, 1, "Should have 1 message after deletion")
	assert.Equal(t, userID, messages[0].ID, "Remaining message should be the user message")

	// Verify trying to get the deleted message returns an error
	_, err = storage.GetMessage(ctx, assistantID)
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
	messages, err := storage.ListMessages(ctx)
	require.NoError(t, err, "Failed to list messages")
	assert.Len(t, messages, 2, "Should still have both messages")
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
	grandchildID, err := storage.SaveMessage(ctx, grandchildMsg, child1ID, "")
	require.NoError(t, err, "Failed to save grandchild message")

	// Verify we have 4 messages total
	messages, err := storage.ListMessages(ctx)
	require.NoError(t, err, "Failed to list messages")
	assert.Len(t, messages, 4, "Should have 4 messages initially")

	// Delete child1 and its descendants recursively
	err = storage.DeleteMessageRecursive(ctx, child1ID)
	require.NoError(t, err, "Failed to recursively delete message")

	// Verify only 2 messages remain (root and child2)
	messages, err = storage.ListMessages(ctx)
	require.NoError(t, err, "Failed to list messages after deletion")
	assert.Len(t, messages, 2, "Should have 2 messages after recursive deletion")

	// Verify we can still access root and child2
	_, err = storage.GetMessage(ctx, rootID)
	assert.NoError(t, err, "Root message should still exist")

	_, err = storage.GetMessage(ctx, child2ID)
	assert.NoError(t, err, "Child 2 message should still exist")

	// Verify child1 and grandchild are gone
	_, err = storage.GetMessage(ctx, child1ID)
	assert.Error(t, err, "Child 1 message should be deleted")

	_, err = storage.GetMessage(ctx, grandchildID)
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
	dialog1, err := storage.GetDialogForUserMessage(ctx, branch1ID)
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
	dialog2, err := storage.GetDialogForUserMessage(ctx, branch2ID)
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

	// Define expected dialog structures
	expectedDialog1 := gai.Dialog{
		{
			Role: gai.User,
			Blocks: []gai.Block{
				{
					BlockType:    gai.Content,
					ModalityType: gai.Text,
					MimeType:     "text/plain",
					Content:      gai.Str("Hello, AI!"),
				},
			},
		},
		{
			Role: gai.Assistant,
			Blocks: []gai.Block{
				{
					BlockType:    gai.Content,
					ModalityType: gai.Text,
					MimeType:     "text/plain",
					Content:      gai.Str("Hello! How can I help you today?"),
				},
			},
		},
		{
			Role: gai.User,
			Blocks: []gai.Block{
				{
					BlockType:    gai.Content,
					ModalityType: gai.Text,
					MimeType:     "text/plain",
					Content:      gai.Str("Tell me about cats."),
				},
			},
		},
		{
			Role: gai.Assistant,
			Blocks: []gai.Block{
				{
					BlockType:    gai.Content,
					ModalityType: gai.Text,
					MimeType:     "text/plain",
					Content:      gai.Str("Cats are wonderful pets..."),
				},
			},
		},
	}

	expectedDialog2 := gai.Dialog{
		{
			Role: gai.User,
			Blocks: []gai.Block{
				{
					BlockType:    gai.Content,
					ModalityType: gai.Text,
					MimeType:     "text/plain",
					Content:      gai.Str("Hello, AI!"),
				},
			},
		},
		{
			Role: gai.Assistant,
			Blocks: []gai.Block{
				{
					BlockType:    gai.Content,
					ModalityType: gai.Text,
					MimeType:     "text/plain",
					Content:      gai.Str("Hello! How can I help you today?"),
				},
			},
		},
		{
			Role: gai.User,
			Blocks: []gai.Block{
				{
					BlockType:    gai.Content,
					ModalityType: gai.Text,
					MimeType:     "text/plain",
					Content:      gai.Str("Tell me about dogs."),
				},
			},
		},
		{
			Role: gai.Assistant,
			Blocks: []gai.Block{
				{
					BlockType:    gai.Content,
					ModalityType: gai.Text,
					MimeType:     "text/plain",
					Content:      gai.Str("Dogs are loyal companions..."),
				},
			},
		},
	}

	// Define a custom comparer for dialogs that ignores IDs
	dialogComparer := cmp.Transformer("IgnoreIDs", func(in gai.Dialog) gai.Dialog {
		result := make(gai.Dialog, len(in))
		for i, msg := range in {
			// Copy the message
			newMsg := msg

			// Create new blocks without IDs
			newBlocks := make([]gai.Block, len(msg.Blocks))
			for j, block := range msg.Blocks {
				newBlock := block
				newBlock.ID = "" // Clear the ID
				newBlocks[j] = newBlock
			}

			newMsg.Blocks = newBlocks
			result[i] = newMsg
		}
		return result
	})

	// Compare dialogs using go-cmp with custom transformer
	if diff := cmp.Diff(expectedDialog1, dialog1, dialogComparer); diff != "" {
		t.Errorf("Dialog 1 mismatch (-want +got):\n%s", diff)
	}

	if diff := cmp.Diff(expectedDialog2, dialog2, dialogComparer); diff != "" {
		t.Errorf("Dialog 2 mismatch (-want +got):\n%s", diff)
	}

	// Verify shared path and branches
	assert.Equal(t, dialog1[:2], dialog2[:2], "Parent messages should be identical")
	assert.NotEqual(t, dialog1[2].Blocks[0].Content.String(), dialog2[2].Blocks[0].Content.String(), "Branch messages should differ")
	assert.NotEqual(t, dialog1[3].Blocks[0].Content.String(), dialog2[3].Blocks[0].Content.String(), "Branch replies should differ")
}
