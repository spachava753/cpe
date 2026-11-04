package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/models"
)

// ID identifies a saved conversation and its connection-owned live state.
type ID string

// Info describes saved history, not a live execution.
type Info struct {
	UpdatedAt time.Time
	ParentID  *ID
	ID        ID
	Cwd       string
	Title     string
}

// Record checkpoints conversation and configuration, never live resources.
type Record struct {
	Info       Info
	Usage      TokenUsage
	Compaction *Compaction
	Options    models.Selection
	Dialog     gai.Dialog
}

// Query filters saved conversations, most recently updated first. Cursor is
// an opaque NextCursor from a previous page.
type Query struct {
	Cwd    string
	Cursor string
	Limit  uint
}

// Page contains saved sessions and an optional continuation cursor.
type Page struct {
	NextCursor string
	Sessions   []Info
}

// Store persists whole checkpoints atomically; returned records are owned.
// Save borrows its argument until return. This is not a VM replay journal or ATIF trace.
type Store interface {
	Save(context.Context, Record) error
	Load(context.Context, ID) (Record, error)
	List(context.Context, Query) (Page, error)
	Delete(context.Context, ID) error
}

// JSON stores the string payload of each gai block explicitly because fmt.Stringer
// cannot be decoded by encoding/json. Provider replay metadata is retained.
type storedBlock struct {
	gai.Block
	Content string `json:"content"`
}

type storedMessage struct {
	gai.Message
	Blocks []storedBlock `json:"blocks"`
}

func encodeMessage(message gai.Message) ([]byte, error) {
	item, err := storeMessage(message)
	if err != nil {
		return nil, err
	}
	return json.Marshal(item)
}

func decodeMessage(data []byte) (gai.Message, error) {
	var item storedMessage
	if err := json.Unmarshal(
		data,
		&item,
	); err != nil {
		return gai.Message{}, err
	}
	return restoreMessage(item), nil
}

func storeMessage(message gai.Message) (storedMessage, error) {
	item := storedMessage{
		Message: message,
	}
	for _, block := range message.Blocks {
		if block.Content == nil {
			return storedMessage{}, errors.New("history contains an empty block")
		}
		item.Blocks = append(
			item.Blocks,
			storedBlock{
				Block:   block,
				Content: block.Content.String(),
			},
		)
	}
	return item, nil
}

func restoreMessage(item storedMessage) gai.Message {
	message := item.Message
	message.Blocks = nil
	for _, block := range item.Blocks {
		block.Block.Content = gai.Str(block.Content)
		message.Blocks = append(
			message.Blocks,
			block.Block,
		)
	}
	return message
}

// Clone returns a deep copy, preserving provider metadata and block contents.
// Token counters are immutable and may be shared.
func (record Record) Clone() (Record, error) {
	if err := record.Validate(); err != nil {
		return Record{}, err
	}
	clone := record
	if record.Info.ParentID != nil {
		clone.Info.ParentID = new(*record.Info.ParentID)
	}
	if record.Compaction != nil {
		clone.Compaction = new(*record.Compaction)
	}
	clone.Dialog = nil
	for _, message := range record.Dialog {
		data, err := encodeMessage(message)
		if err != nil {
			return Record{}, err
		}
		copied, err := decodeMessage(data)
		if err != nil {
			return Record{}, err
		}
		clone.Dialog = append(
			clone.Dialog,
			copied,
		)
	}
	return clone, nil
}

// Validate checks checkpoint invariants without accessing the filesystem.
func (record Record) Validate() error {
	if err := record.checkCompaction(); err != nil {
		return err
	}
	return record.Usage.check()
}
