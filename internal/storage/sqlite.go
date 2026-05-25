package storage

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"iter"
	"math"
	"strings"
	"time"

	acp "github.com/coder/acp-go-sdk"
	gonanoid "github.com/matoous/go-nanoid/v2"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/storage/sqlcgen"
)

// DB is the database contract required by NewSqlite.
//
// It is intentionally narrow so callers can pass either *sql.DB or a test
// double. SaveDialog and DeleteMessages rely on BeginTx providing real SQL
// transaction semantics (commit or rollback boundaries).
type DB interface {
	sqlcgen.DBTX
	// ExecContext executes a statement that does not return rows. NewSqlite uses
	// it for PRAGMA setup and schema initialization/migrations.
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	// BeginTx starts a transaction used for atomic write operations.
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

const idCharset = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func generateId() string {
	return gonanoid.MustGenerate(idCharset, 6)
}

//go:embed schema.sql
var schemaSQL string

// SqliteOption configures a SQLite-backed message store.
type SqliteOption func(*Sqlite)

// WithIDGenerator overrides the message ID generator used for newly persisted
// messages.
//
// This is primarily useful for deterministic tests. The generator must return
// unique IDs often enough for SaveDialog's collision retry limit; duplicate IDs
// are retried and eventually fail the save. A nil generator leaves the default
// random generator in place.
func WithIDGenerator(idGenerator func() string) SqliteOption {
	return func(s *Sqlite) {
		if idGenerator != nil {
			s.idGenerator = idGenerator
		}
	}
}

// Sqlite is the SQLite-backed MessageDB implementation.
//
// It stores messages as a parent-linked tree and reconstructs gai.Message
// values (including metadata keys in ExtraFields) on reads.
type Sqlite struct {
	db          DB
	q           *sqlcgen.Queries
	idGenerator func() string
}

// NewSqlite initializes a SQLite-backed message store.
//
// It enables foreign-key enforcement, applies lightweight compatibility
// migrations, and executes the embedded schema SQL before returning.
//
// The caller owns the lifecycle of db (open/close).
func NewSqlite(ctx context.Context, db DB, opts ...SqliteOption) (*Sqlite, error) {
	if err := enableForeignKeys(ctx, db); err != nil {
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}
	if err := migrateDeleteLegacySubagentMessages(ctx, db); err != nil {
		return nil, fmt.Errorf("failed to migrate schema: %w", err)
	}
	if err := migrateMessagesCompactionParentColumn(ctx, db); err != nil {
		return nil, fmt.Errorf("failed to migrate schema: %w", err)
	}
	if err := migrateMessagesMetadataColumns(ctx, db); err != nil {
		return nil, fmt.Errorf("failed to migrate schema: %w", err)
	}

	// Initialize schema from embedded SQL file
	_, err := db.ExecContext(ctx, schemaSQL)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	store := &Sqlite{
		db:          db,
		q:           sqlcgen.New(db),
		idGenerator: generateId,
	}
	for _, opt := range opts {
		opt(store)
	}
	return store, nil
}

func enableForeignKeys(ctx context.Context, db DB) error {
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON;"); err != nil {
		return fmt.Errorf("failed to set PRAGMA foreign_keys = ON: %w", err)
	}

	var enabled int
	if err := db.QueryRowContext(ctx, "PRAGMA foreign_keys;").Scan(&enabled); err != nil {
		return fmt.Errorf("failed to verify PRAGMA foreign_keys: %w", err)
	}
	if enabled != 1 {
		return fmt.Errorf("PRAGMA foreign_keys is OFF")
	}
	return nil
}

func migrateDeleteLegacySubagentMessages(ctx context.Context, db DB) error {
	hasMessages, err := hasMessagesTable(ctx, db)
	if err != nil {
		return err
	}
	if !hasMessages {
		return nil
	}

	hasSubagent, err := hasMessagesColumn(ctx, db, "is_subagent")
	if err != nil {
		return err
	}
	hasTitle, err := hasMessagesColumn(ctx, db, "title")
	if err != nil {
		return err
	}
	if !hasSubagent && !hasTitle {
		return nil
	}

	predicates := make([]string, 0, 2)
	if hasSubagent {
		predicates = append(predicates, "is_subagent = 1")
	}
	if hasTitle {
		predicates = append(predicates, "title LIKE 'subagent:%'")
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin legacy subagent cleanup transaction: %w", err)
	}
	defer tx.Rollback()

	query := fmt.Sprintf(`
		WITH RECURSIVE subagent_subtree(id, depth) AS (
			SELECT id, 0 FROM messages WHERE %s
			UNION ALL
			SELECT messages.id, subagent_subtree.depth + 1
			FROM messages
			JOIN subagent_subtree ON messages.parent_id = subagent_subtree.id
		)
		SELECT id
		FROM subagent_subtree
		GROUP BY id
		ORDER BY max(depth) DESC
	`, strings.Join(predicates, " OR "))
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to list legacy subagent messages: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("failed to scan legacy subagent message ID: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed to iterate legacy subagent message IDs: %w", err)
	}

	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, "DELETE FROM messages WHERE id = ?", id); err != nil {
			return fmt.Errorf("failed to delete legacy subagent message %s: %w", id, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit legacy subagent cleanup: %w", err)
	}
	return nil
}

func migrateMessagesCompactionParentColumn(ctx context.Context, db DB) error {
	hasMessages, err := hasMessagesTable(ctx, db)
	if err != nil {
		return err
	}
	if !hasMessages {
		return nil
	}

	hasCompactionParent, err := hasMessagesColumn(ctx, db, "compaction_parent_id")
	if err != nil {
		return err
	}
	if hasCompactionParent {
		return nil
	}

	if _, err := db.ExecContext(ctx, "ALTER TABLE messages ADD COLUMN compaction_parent_id TEXT"); err != nil {
		return fmt.Errorf("failed to add compaction_parent_id column: %w", err)
	}
	return nil
}

func migrateMessagesMetadataColumns(ctx context.Context, db DB) error {
	hasMessages, err := hasMessagesTable(ctx, db)
	if err != nil {
		return err
	}
	if !hasMessages {
		return nil
	}

	columns := []struct {
		name       string
		definition string
	}{
		{"message_extra_fields", "TEXT"},
		{"model_ref", "TEXT"},
		{"model_id", "TEXT"},
		{"model_type", "TEXT"},
		{"model_display_name", "TEXT"},
		{"input_tokens", "INTEGER"},
		{"output_tokens", "INTEGER"},
		{"cache_read_tokens", "INTEGER"},
		{"cache_write_tokens", "INTEGER"},
	}
	for _, column := range columns {
		exists, err := hasMessagesColumn(ctx, db, column.name)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		query := fmt.Sprintf("ALTER TABLE messages ADD COLUMN %s %s", column.name, column.definition)
		if _, err := db.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("failed to add %s column: %w", column.name, err)
		}
	}
	return nil
}

func hasMessagesTable(ctx context.Context, db DB) (bool, error) {
	var tableCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='messages'").Scan(&tableCount); err != nil {
		return false, fmt.Errorf("failed to inspect existing schema: %w", err)
	}
	return tableCount > 0, nil
}

func hasMessagesColumn(ctx context.Context, db DB, column string) (bool, error) {
	rows, err := db.QueryContext(ctx, "PRAGMA table_info(messages)")
	if err != nil {
		return false, fmt.Errorf("failed to inspect messages table columns: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &colType, &notNull, &defaultValue, &pk); err != nil {
			return false, fmt.Errorf("failed to read messages table metadata: %w", err)
		}
		if name == column {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("failed to iterate messages table metadata: %w", err)
	}
	return false, nil
}

// roleToString converts a gai.Role to its string representation
func roleToString(role gai.Role) string {
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

// stringToRole converts a string to its gai.Role representation
func stringToRole(s string) (gai.Role, error) {
	switch s {
	case "user":
		return gai.User, nil
	case "assistant":
		return gai.Assistant, nil
	case "tool_result":
		return gai.ToolResult, nil
	default:
		return 0, fmt.Errorf("invalid role: %s", s)
	}
}

// saveMessageInTx saves a single message and its blocks within a transaction.
func (s *Sqlite) saveMessageInTx(ctx context.Context, qtx *sqlcgen.Queries, message gai.Message, parentID, compactionParentID string) (string, error) {
	// Generate a unique message ID
	messageID, err := s.generateUniqueIDInTx(ctx, qtx)
	if err != nil {
		return "", fmt.Errorf("failed to generate message ID: %w", err)
	}

	// Prepare parent ID parameter
	var parentIDParam sql.NullString
	if parentID != "" {
		parentIDParam = sql.NullString{String: parentID, Valid: true}
	}

	// Prepare compaction parent ID parameter.
	var compactionParentIDParam sql.NullString
	if compactionParentID != "" {
		compactionParentIDParam = sql.NullString{String: compactionParentID, Valid: true}
	}

	messageExtraFields, err := encodeMessageExtraFields(message.ExtraFields)
	if err != nil {
		return "", err
	}
	modelRef, err := messageMetadataString(message.ExtraFields, AgentMetadataModelRefKey)
	if err != nil {
		return "", err
	}
	modelID, err := messageMetadataString(message.ExtraFields, AgentMetadataModelIDKey)
	if err != nil {
		return "", err
	}
	modelType, err := messageMetadataString(message.ExtraFields, AgentMetadataModelTypeKey)
	if err != nil {
		return "", err
	}
	modelDisplayName, err := messageMetadataString(message.ExtraFields, AgentMetadataModelDisplayNameKey)
	if err != nil {
		return "", err
	}
	inputTokens, err := messageMetadataInt64(message.ExtraFields, AgentMetadataInputTokensKey)
	if err != nil {
		return "", err
	}
	outputTokens, err := messageMetadataInt64(message.ExtraFields, AgentMetadataOutputTokensKey)
	if err != nil {
		return "", err
	}
	cacheReadTokens, err := messageMetadataInt64(message.ExtraFields, AgentMetadataCacheReadTokensKey)
	if err != nil {
		return "", err
	}
	cacheWriteTokens, err := messageMetadataInt64(message.ExtraFields, AgentMetadataCacheWriteTokensKey)
	if err != nil {
		return "", err
	}

	// Create the message
	err = qtx.CreateMessage(ctx, sqlcgen.CreateMessageParams{
		ID:                 messageID,
		ParentID:           parentIDParam,
		CompactionParentID: compactionParentIDParam,
		Role:               roleToString(message.Role),
		ToolResultError:    message.ToolResultError,
		MessageExtraFields: messageExtraFields,
		ModelRef:           modelRef,
		ModelID:            modelID,
		ModelType:          modelType,
		ModelDisplayName:   modelDisplayName,
		InputTokens:        inputTokens,
		OutputTokens:       outputTokens,
		CacheReadTokens:    cacheReadTokens,
		CacheWriteTokens:   cacheWriteTokens,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create message: %w", err)
	}

	// Create all blocks for this message
	for j, block := range message.Blocks {
		// If the block already has an ID, use it; otherwise leave it as NULL
		var blockID sql.NullString
		if block.ID != "" {
			blockID = sql.NullString{
				String: block.ID,
				Valid:  true,
			}
		}

		// Encode ExtraFields as JSON if it exists
		var extraFieldsParam sql.NullString
		if block.ExtraFields != nil {
			extraFieldsJSON, err := json.Marshal(block.ExtraFields)
			if err != nil {
				return "", fmt.Errorf("failed to marshal ExtraFields to JSON: %w", err)
			}
			extraFieldsParam = sql.NullString{
				String: string(extraFieldsJSON),
				Valid:  true,
			}
		}

		err = qtx.CreateBlock(ctx, sqlcgen.CreateBlockParams{
			ID:            blockID,
			MessageID:     messageID,
			BlockType:     block.BlockType,
			ModalityType:  int64(block.ModalityType),
			MimeType:      block.MimeType,
			Content:       block.Content.String(),
			ExtraFields:   extraFieldsParam,
			SequenceOrder: int64(j),
		})
		if err != nil {
			return "", fmt.Errorf("failed to create block: %w", err)
		}
	}

	return messageID, nil
}

// getExtraFieldString safely extracts a string value from an ExtraFields map.
func getExtraFieldString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, _ := m[key].(string)
	return v
}

var messageColumnExtraFieldKeys = map[string]struct{}{
	MessageIDKey:                     {},
	MessageParentIDKey:               {},
	MessageCompactionParentIDKey:     {},
	MessageCreatedAtKey:              {},
	AgentMetadataModelRefKey:         {},
	AgentMetadataModelIDKey:          {},
	AgentMetadataModelTypeKey:        {},
	AgentMetadataModelDisplayNameKey: {},
	AgentMetadataInputTokensKey:      {},
	AgentMetadataOutputTokensKey:     {},
	AgentMetadataCacheReadTokensKey:  {},
	AgentMetadataCacheWriteTokensKey: {},
}

func encodeMessageExtraFields(extra map[string]any) (sql.NullString, error) {
	if len(extra) == 0 {
		return sql.NullString{}, nil
	}
	filtered := make(map[string]any, len(extra))
	for key, value := range extra {
		if _, ok := messageColumnExtraFieldKeys[key]; ok {
			continue
		}
		filtered[key] = value
	}
	if len(filtered) == 0 {
		return sql.NullString{}, nil
	}

	extraFieldsJSON, err := json.Marshal(filtered)
	if err != nil {
		return sql.NullString{}, fmt.Errorf("failed to marshal message ExtraFields to JSON: %w", err)
	}
	return sql.NullString{String: string(extraFieldsJSON), Valid: true}, nil
}

func messageMetadataString(extra map[string]any, key string) (sql.NullString, error) {
	if extra == nil {
		return sql.NullString{}, nil
	}
	value, ok := extra[key]
	if !ok || value == nil {
		return sql.NullString{}, nil
	}
	str, ok := value.(string)
	if !ok {
		return sql.NullString{}, fmt.Errorf("message ExtraFields[%q] must be a string, got %T", key, value)
	}
	return sql.NullString{String: str, Valid: true}, nil
}

func messageMetadataInt64(extra map[string]any, key string) (sql.NullInt64, error) {
	if extra == nil {
		return sql.NullInt64{}, nil
	}
	value, ok := extra[key]
	if !ok || value == nil {
		return sql.NullInt64{}, nil
	}
	intValue, err := extraFieldInt64(value)
	if err != nil {
		return sql.NullInt64{}, fmt.Errorf("message ExtraFields[%q] must be an integer: %w", key, err)
	}
	return sql.NullInt64{Int64: intValue, Valid: true}, nil
}

func extraFieldInt64(value any) (int64, error) {
	switch v := value.(type) {
	case int:
		return int64(v), nil
	case int8:
		return int64(v), nil
	case int16:
		return int64(v), nil
	case int32:
		return int64(v), nil
	case int64:
		return v, nil
	case uint:
		if uint64(v) > math.MaxInt64 {
			return 0, fmt.Errorf("%d overflows int64", v)
		}
		return int64(v), nil
	case uint8:
		return int64(v), nil
	case uint16:
		return int64(v), nil
	case uint32:
		return int64(v), nil
	case uint64:
		if v > math.MaxInt64 {
			return 0, fmt.Errorf("%d overflows int64", v)
		}
		return int64(v), nil
	case float64:
		if math.Trunc(v) != v || v < math.MinInt64 || v > math.MaxInt64 {
			return 0, fmt.Errorf("%v is not an int64", v)
		}
		return int64(v), nil
	default:
		return 0, fmt.Errorf("got %T", value)
	}
}

func decodeMessageExtraFields(encoded sql.NullString) (map[string]any, error) {
	if !encoded.Valid || encoded.String == "" {
		return map[string]any{}, nil
	}
	var extra map[string]any
	if err := json.Unmarshal([]byte(encoded.String), &extra); err != nil {
		return nil, fmt.Errorf("failed to unmarshal message ExtraFields: %w", err)
	}
	if extra == nil {
		return map[string]any{}, nil
	}
	return extra, nil
}

func putNullString(extra map[string]any, key string, value sql.NullString) {
	if value.Valid {
		extra[key] = value.String
	}
}

func putNullInt64(extra map[string]any, key string, value sql.NullInt64) {
	if value.Valid {
		extra[key] = value.Int64
	}
}

func acpSessionInfo(id, cwd, title string, updatedAt time.Time) acp.SessionInfo {
	updatedAtText := updatedAt.UTC().Format(time.RFC3339Nano)
	return acp.SessionInfo{
		Cwd:       cwd,
		SessionId: acp.SessionId(id),
		Title:     &title,
		UpdatedAt: &updatedAtText,
	}
}

func acpSessionTitle(session acp.SessionInfo) string {
	if session.Title == nil {
		return ""
	}
	return *session.Title
}

// generateUniqueIDInTx generates a unique ID checking for collisions within a transaction.
func (s *Sqlite) generateUniqueIDInTx(ctx context.Context, qtx *sqlcgen.Queries) (string, error) {
	maxAttempts := 10
	for range maxAttempts {
		id := s.idGenerator()

		exists, err := qtx.CheckMessageIDExists(ctx, id)
		if err != nil {
			return "", fmt.Errorf("failed to check ID collision: %w", err)
		}

		if exists == 0 {
			return id, nil
		}
	}

	return "", fmt.Errorf("failed to generate unique ID after %d attempts", maxAttempts)
}

// SaveDialog validates and persists a root-to-leaf chain in one SQL
// transaction.
//
// Existing messages (MessageIDKey already present) are verified against stored
// parent links. New messages are inserted with parent IDs pointing to the
// previously processed message.
//
// Commit/rollback boundary: when iteration starts, a transaction is opened.
// Any validation or insert failure rolls back writes from this call. If the
// consumer stops iteration early, the processed prefix is still committed when
// possible; remaining input is not read.
//
// See DialogSaver for cross-implementation contract details.
func (s *Sqlite) SaveDialog(ctx context.Context, msgs iter.Seq[gai.Message]) iter.Seq2[gai.Message, error] {
	return func(yield func(gai.Message, error) bool) {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			yield(gai.Message{}, fmt.Errorf("failed to begin transaction: %w", err))
			return
		}
		committed := false
		defer func() {
			if !committed {
				tx.Rollback()
			}
		}()

		qtx := s.q.WithTx(tx)

		var prevID string
		first := true
		consumerStopped := false

		for msg := range msgs {
			existingID := getExtraFieldString(msg.ExtraFields, MessageIDKey)

			if existingID != "" {
				// Message claims to be already persisted — verify it exists
				// and has the correct parent.
				dbMsg, dbErr := qtx.GetMessage(ctx, existingID)
				if dbErr != nil {
					yield(gai.Message{}, fmt.Errorf("failed to verify message %s exists: %w", existingID, dbErr))
					return
				}

				// Verify parent chain
				dbParent := ""
				if dbMsg.ParentID.Valid {
					dbParent = dbMsg.ParentID.String
				}
				if first {
					// First message must be a root (no parent in storage)
					if dbParent != "" {
						yield(gai.Message{}, fmt.Errorf("first message %s must be a root message but has parent %q in storage", existingID, dbParent))
						return
					}
				} else {
					if dbParent != prevID {
						yield(gai.Message{}, fmt.Errorf("message %s has parent %q in storage but expected %q", existingID, dbParent, prevID))
						return
					}
				}

				prevID = existingID
				first = false

				// Yield the message as-is (it's already persisted)
				if !yield(msg, nil) {
					consumerStopped = true
					break
				}
				continue
			}

			// Message needs to be saved
			parentID := prevID
			compactionParentID := ""
			if parentID == "" {
				compactionParentID = getExtraFieldString(msg.ExtraFields, MessageCompactionParentIDKey)
			}

			savedID, saveErr := s.saveMessageInTx(ctx, qtx, msg, parentID, compactionParentID)
			if saveErr != nil {
				yield(gai.Message{}, fmt.Errorf("failed to save message: %w", saveErr))
				return
			}

			// Set ExtraFields on the message
			if msg.ExtraFields == nil {
				msg.ExtraFields = make(map[string]any)
			}
			msg.ExtraFields[MessageIDKey] = savedID
			if parentID != "" {
				msg.ExtraFields[MessageParentIDKey] = parentID
				delete(msg.ExtraFields, MessageCompactionParentIDKey)
			} else if compactionParentID != "" {
				msg.ExtraFields[MessageCompactionParentIDKey] = compactionParentID
			}

			prevID = savedID
			first = false

			if !yield(msg, nil) {
				consumerStopped = true
				break
			}
		}

		if err := tx.Commit(); err != nil {
			if !consumerStopped {
				// Consumer is still active; propagate the error.
				yield(gai.Message{}, fmt.Errorf("failed to commit transaction: %w", err))
			}
			// If consumer stopped (break/return), we cannot call yield again.
			// The deferred Rollback will clean up.
			return
		}
		committed = true
	}
}

// getMessage loads one message and all of its blocks, then reconstructs a
// gai.Message with storage metadata in ExtraFields.
//
// It returns the reconstructed message, the parent ID ("" for roots), and an
// error. JSON-compatible message-level ExtraFields are restored from storage,
// with storage-owned and typed runtime metadata overlaid from dedicated columns.
func (s *Sqlite) getMessage(ctx context.Context, messageID string) (gai.Message, string, error) {
	msg, err := s.q.GetMessage(ctx, messageID)
	if err != nil {
		return gai.Message{}, "", fmt.Errorf("failed to get message: %w", err)
	}

	role, err := stringToRole(msg.Role)
	if err != nil {
		return gai.Message{}, "", fmt.Errorf("invalid role in database: %w", err)
	}

	blocks, err := s.q.GetBlocksByMessage(ctx, messageID)
	if err != nil {
		return gai.Message{}, "", fmt.Errorf("failed to get blocks: %w", err)
	}

	var gaiBlocks []gai.Block
	for _, block := range blocks {
		var blockID string
		if block.ID.Valid {
			blockID = block.ID.String
		}

		var extraFields map[string]any
		if block.ExtraFields.Valid && block.ExtraFields.String != "" {
			if err := json.Unmarshal([]byte(block.ExtraFields.String), &extraFields); err != nil {
				return gai.Message{}, "", fmt.Errorf("failed to unmarshal ExtraFields: %w", err)
			}
		}

		gaiBlocks = append(gaiBlocks, gai.Block{
			ID:           blockID,
			BlockType:    block.BlockType,
			ModalityType: gai.Modality(block.ModalityType),
			MimeType:     block.MimeType,
			Content:      gai.Str(block.Content),
			ExtraFields:  extraFields,
		})
	}

	// Extract parent IDs
	parentID := ""
	if msg.ParentID.Valid {
		parentID = msg.ParentID.String
	}
	compactionParentID := ""
	if msg.CompactionParentID.Valid {
		compactionParentID = msg.CompactionParentID.String
	}

	// Set storage-owned and typed runtime metadata in ExtraFields so consumers can
	// access a complete message snapshot without knowing the physical schema.
	msgExtraFields, err := decodeMessageExtraFields(msg.MessageExtraFields)
	if err != nil {
		return gai.Message{}, "", err
	}
	msgExtraFields[MessageIDKey] = messageID
	msgExtraFields[MessageCreatedAtKey] = msg.CreatedAt
	putNullString(msgExtraFields, AgentMetadataModelRefKey, msg.ModelRef)
	putNullString(msgExtraFields, AgentMetadataModelIDKey, msg.ModelID)
	putNullString(msgExtraFields, AgentMetadataModelTypeKey, msg.ModelType)
	putNullString(msgExtraFields, AgentMetadataModelDisplayNameKey, msg.ModelDisplayName)
	putNullInt64(msgExtraFields, AgentMetadataInputTokensKey, msg.InputTokens)
	putNullInt64(msgExtraFields, AgentMetadataOutputTokensKey, msg.OutputTokens)
	putNullInt64(msgExtraFields, AgentMetadataCacheReadTokensKey, msg.CacheReadTokens)
	putNullInt64(msgExtraFields, AgentMetadataCacheWriteTokensKey, msg.CacheWriteTokens)
	if parentID != "" {
		msgExtraFields[MessageParentIDKey] = parentID
	}
	if compactionParentID != "" {
		msgExtraFields[MessageCompactionParentIDKey] = compactionParentID
	}

	return gai.Message{
		Role:            role,
		Blocks:          gaiBlocks,
		ToolResultError: msg.ToolResultError,
		ExtraFields:     msgExtraFields,
	}, parentID, nil
}

// CreateACPSession persists ACP session metadata.
func (s *Sqlite) CreateACPSession(ctx context.Context, session acp.SessionInfo, lastMessageID string) error {
	err := s.q.CreateSession(ctx, sqlcgen.CreateSessionParams{
		ID:            string(session.SessionId),
		LastMessageID: lastMessageID,
		Cwd:           session.Cwd,
		Title:         acpSessionTitle(session),
	})
	if err != nil {
		return fmt.Errorf("failed to create ACP session %s: %w", session.SessionId, err)
	}
	return nil
}

// AddACPSessionMessage marks a persisted message as the latest message for an
// ACP session and returns the updated session.
//
// UpdatedAt is formatted as an ISO 8601 timestamp from MessageID's creation
// time.
func (s *Sqlite) AddACPSessionMessage(ctx context.Context, sessionID acp.SessionId, messageID string) (acp.SessionInfo, error) {
	rowsAffected, err := s.q.AddSessionMessage(ctx, sqlcgen.AddSessionMessageParams{
		LastMessageID: messageID,
		ID:            string(sessionID),
	})
	if err != nil {
		return acp.SessionInfo{}, fmt.Errorf("failed to add message %s to ACP session %s: %w", messageID, sessionID, err)
	}
	if rowsAffected == 0 {
		return acp.SessionInfo{}, fmt.Errorf("ACP session %s not found", sessionID)
	}
	resp, err := s.GetACPSession(ctx, sessionID)
	if err != nil {
		return acp.SessionInfo{}, err
	}
	return resp.Session, nil
}

// GetACPSession returns ACP session metadata and its latest persisted message
// ID.
//
// UpdatedAt is formatted as an ISO 8601 timestamp from the session's last
// message creation time.
func (s *Sqlite) GetACPSession(ctx context.Context, sessionID acp.SessionId) (GetACPSessionResponse, error) {
	row, err := s.q.GetSession(ctx, string(sessionID))
	if err != nil {
		return GetACPSessionResponse{}, fmt.Errorf("failed to get ACP session %s: %w", sessionID, err)
	}
	return GetACPSessionResponse{
		Session:       acpSessionInfo(row.ID, row.Cwd, row.Title, row.UpdatedAt),
		LastMessageID: row.LastMessageID,
	}, nil
}

// ListACPSessions returns ACP session metadata ordered by last activity, newest
// first.
//
// UpdatedAt is formatted as an ISO 8601 timestamp from each session's last
// message creation time.
func (s *Sqlite) ListACPSessions(ctx context.Context) ([]acp.SessionInfo, error) {
	rows, err := s.q.ListSessions(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list ACP sessions: %w", err)
	}
	sessions := make([]acp.SessionInfo, 0, len(rows))
	for _, row := range rows {
		sessions = append(sessions, acpSessionInfo(row.ID, row.Cwd, row.Title, row.UpdatedAt))
	}
	return sessions, nil
}

// GetMessages returns fully populated messages for the requested IDs.
//
// The method fails if any ID cannot be loaded. Returned messages include all
// blocks plus storage metadata keys in ExtraFields.
func (s *Sqlite) GetMessages(ctx context.Context, messageIDs []string) (iter.Seq[gai.Message], error) {
	messages := make([]gai.Message, 0, len(messageIDs))
	for _, id := range messageIDs {
		msg, _, err := s.getMessage(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("failed to get message %s: %w", id, err)
		}
		messages = append(messages, msg)
	}

	return func(yield func(gai.Message) bool) {
		for _, msg := range messages {
			if !yield(msg) {
				return
			}
		}
	}, nil
}

// ListMessages returns a materialized snapshot of messages ordered by
// created_at with optional ascending order and offset.
//
// Each yielded message includes all blocks and storage metadata keys in
// ExtraFields.
func (s *Sqlite) ListMessages(ctx context.Context, opts ListMessagesOptions) (iter.Seq[gai.Message], error) {
	var rows []sqlcgen.Message
	var err error
	if opts.AscendingOrder {
		rows, err = s.q.ListMessagesAscending(ctx, int64(opts.Offset))
	} else {
		rows, err = s.q.ListMessagesDescending(ctx, int64(opts.Offset))
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list messages: %w", err)
	}

	// For each row, load blocks and build gai.Message
	messages := make([]gai.Message, 0, len(rows))
	for _, row := range rows {
		msg, _, err := s.getMessage(ctx, row.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get message %s: %w", row.ID, err)
		}
		messages = append(messages, msg)
	}

	return func(yield func(gai.Message) bool) {
		for _, msg := range messages {
			if !yield(msg) {
				return
			}
		}
	}, nil
}

// DeleteMessages deletes the requested messages in a single transaction.
//
// With opts.Recursive=true, each message's descendant subtree is removed.
// With opts.Recursive=false, deletion fails if any target has children.
//
// The operation is atomic across all IDs in opts.MessageIDs.
func (s *Sqlite) DeleteMessages(ctx context.Context, opts DeleteMessagesOptions) error {
	// Begin transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	qtx := s.q.WithTx(tx)

	for _, id := range opts.MessageIDs {
		if opts.Recursive {
			if err := s.deleteMessageAndDescendantsInTx(ctx, qtx, id); err != nil {
				return err
			}
		} else {
			// Check if the message has children
			hasChildren, err := qtx.HasChildren(ctx, sql.NullString{String: id, Valid: true})
			if err != nil {
				return fmt.Errorf("failed to check for children: %w", err)
			}
			if hasChildren > 0 {
				return fmt.Errorf("cannot delete message with ID %s: message has children", id)
			}
			if err := qtx.DeleteMessage(ctx, id); err != nil {
				return fmt.Errorf("failed to delete message %s: %w", id, err)
			}
		}
	}

	// Commit the transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// deleteMessageAndDescendantsInTx performs depth-first subtree deletion inside
// the caller's transaction.
func (s *Sqlite) deleteMessageAndDescendantsInTx(ctx context.Context, qtx *sqlcgen.Queries, messageID string) error {
	// Get all children of this message
	children, err := qtx.ListMessagesByParent(ctx, sql.NullString{String: messageID, Valid: true})
	if err != nil {
		return fmt.Errorf("failed to list child messages: %w", err)
	}

	// Recursively delete all children first
	for _, child := range children {
		if err := s.deleteMessageAndDescendantsInTx(ctx, qtx, child.ID); err != nil {
			return err
		}
	}

	// Finally delete this message (which will cascade delete its blocks)
	if err := qtx.DeleteMessage(ctx, messageID); err != nil {
		return fmt.Errorf("failed to delete message %s: %w", messageID, err)
	}

	return nil
}
