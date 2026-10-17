package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spachava753/gai"
	"github.com/spf13/afero"
)

// SessionInfo describes saved history, not a live execution.
type SessionInfo struct {
	ID        SessionID
	ParentID  *SessionID
	Cwd       string
	Title     string
	UpdatedAt time.Time
}

// SessionRecord checkpoints conversation and configuration, never live resources.
// Resuming or forking creates fresh Starlark globals and MCP connections.
type SessionRecord struct {
	Info             SessionInfo
	Options          SessionOptions
	Dialog           gai.Dialog
	AbortedResponses []AbortedResponse
	Compaction       *Compaction
}

// AbortedResponse records an incomplete model response excluded from conversation and replay.
type AbortedResponse struct {
	Model         ModelID
	At            time.Time
	AfterMessages int
	Reason        string
	Partial       gai.Message
}

// SessionQuery filters saved conversations. Cursor is the last returned session ID.
type SessionQuery struct {
	Cwd    string
	Cursor string
	Limit  uint
}

// SessionPage contains saved sessions and an optional continuation cursor.
type SessionPage struct {
	Sessions   []SessionInfo
	NextCursor string
}

// SessionStore persists whole checkpoints atomically; returned records are owned.
// Save borrows its argument until return. This is not a VM replay journal or ATIF trace.
type SessionStore interface {
	Save(context.Context, SessionRecord) error
	Load(context.Context, SessionID) (SessionRecord, error)
	List(context.Context, SessionQuery) (SessionPage, error)
	Delete(context.Context, SessionID) error
}

// NewSessionStore stores JSON checkpoints beneath directory with private permissions.
// The store is safe for concurrent sessions in one process.
func NewSessionStore(directory string) (SessionStore, error) {
	if !filepath.IsAbs(directory) {
		return nil, errors.New("session directory must be absolute")
	}
	return &fileSessionStore{
		filesystem: afero.NewOsFs(),
		directory:  directory,
	}, nil
}

type fileSessionStore struct {
	mu         sync.Mutex
	filesystem afero.Fs
	directory  string
}

func (s *fileSessionStore) filename(id SessionID) (string, error) {
	if id == "" || strings.IndexFunc(
		string(id),
		func(r rune) bool {
			valid := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_'
			return !valid
		},
	) >= 0 {
		return "", errors.New("invalid session ID")
	}
	return filepath.Join(
		s.directory,
		string(id)+".json",
	), nil
}

func (s *fileSessionStore) Save(ctx context.Context, record SessionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	filename, err := s.filename(record.Info.ID)
	if err != nil {
		return err
	}
	data, err := encodeRecord(record)
	if err != nil {
		return err
	}
	if err := s.filesystem.MkdirAll(
		s.directory,
		0o700,
	); err != nil {
		return err
	}
	file, err := afero.TempFile(
		s.filesystem,
		s.directory,
		".checkpoint-",
	)
	if err != nil {
		return err
	}
	defer s.filesystem.Remove(file.Name())
	if err := s.filesystem.Chmod(
		file.Name(),
		0o600,
	); err != nil {
		_ = file.Close()
		return err
	}
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(
		writeErr,
		syncErr,
		closeErr,
		ctx.Err(),
	); err != nil {
		return err
	}
	return s.filesystem.Rename(
		file.Name(),
		filename,
	)
}

func (s *fileSessionStore) Load(ctx context.Context, id SessionID) (SessionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return SessionRecord{}, err
	}
	filename, err := s.filename(id)
	if err != nil {
		return SessionRecord{}, err
	}
	data, err := afero.ReadFile(
		s.filesystem,
		filename,
	)
	if err != nil {
		return SessionRecord{}, err
	}
	record, err := decodeRecord(data)
	if err == nil && record.Info.ID != id {
		return SessionRecord{}, errors.New("stored session ID mismatch")
	}
	return record, err
}

func (s *fileSessionStore) List(ctx context.Context, query SessionQuery) (SessionPage, error) {
	if query.Limit == 0 {
		return SessionPage{}, errors.New("page limit must be positive")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := afero.ReadDir(
		s.filesystem,
		s.directory,
	)
	if errors.Is(
		err,
		fs.ErrNotExist,
	) {
		return SessionPage{}, nil
	}
	if err != nil {
		return SessionPage{}, err
	}
	var infos []SessionInfo
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return SessionPage{}, err
		}
		if entry.IsDir() || !strings.HasSuffix(
			entry.Name(),
			".json",
		) {
			continue
		}
		id := strings.TrimSuffix(
			entry.Name(),
			".json",
		)
		if id <= query.Cursor {
			continue
		}
		data, err := afero.ReadFile(
			s.filesystem,
			filepath.Join(
				s.directory,
				entry.Name(),
			),
		)
		if err != nil {
			return SessionPage{}, err
		}
		record, err := decodeRecord(data)
		if err != nil {
			return SessionPage{}, fmt.Errorf(
				"read session %q: %w",
				id,
				err,
			)
		}
		if query.Cwd == "" || query.Cwd == record.Info.Cwd {
			infos = append(
				infos,
				record.Info,
			)
		}
	}
	sort.Slice(
		infos,
		func(i, j int) bool { return infos[i].ID < infos[j].ID },
	)
	page := SessionPage{
		Sessions: infos,
	}
	if uint(len(infos)) > query.Limit {
		page.Sessions = infos[:query.Limit]
		page.NextCursor = string(page.Sessions[len(page.Sessions)-1].ID)
	}
	return page, nil
}

func (s *fileSessionStore) Delete(ctx context.Context, id SessionID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	filename, err := s.filename(id)
	if err != nil {
		return err
	}
	return s.filesystem.Remove(filename)
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

type storedAbortedResponse struct {
	AbortedResponse
	Partial storedMessage
}

type storedRecord struct {
	Info             SessionInfo
	Options          SessionOptions
	Dialog           []storedMessage
	AbortedResponses []storedAbortedResponse
	Compaction       *Compaction
}

func encodeRecord(record SessionRecord) ([]byte, error) {
	if err := record.checkCompaction(); err != nil {
		return nil, err
	}
	stored := storedRecord{
		Info:       record.Info,
		Options:    record.Options,
		Compaction: record.Compaction,
	}
	for _, message := range record.Dialog {
		item, err := storeMessage(message)
		if err != nil {
			return nil, err
		}
		stored.Dialog = append(
			stored.Dialog,
			item,
		)
	}
	for _, aborted := range record.AbortedResponses {
		partial, err := storeMessage(aborted.Partial)
		if err != nil {
			return nil, err
		}
		stored.AbortedResponses = append(
			stored.AbortedResponses,
			storedAbortedResponse{
				AbortedResponse: aborted,
				Partial:         partial,
			},
		)
	}
	return json.Marshal(stored)
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

func decodeRecord(data []byte) (SessionRecord, error) {
	var stored storedRecord
	if err := json.Unmarshal(
		data,
		&stored,
	); err != nil {
		return SessionRecord{}, err
	}
	record := SessionRecord{
		Info:       stored.Info,
		Options:    stored.Options,
		Compaction: stored.Compaction,
	}
	for _, item := range stored.Dialog {
		record.Dialog = append(
			record.Dialog,
			restoreMessage(item),
		)
	}
	for _, item := range stored.AbortedResponses {
		aborted := item.AbortedResponse
		aborted.Partial = restoreMessage(item.Partial)
		record.AbortedResponses = append(
			record.AbortedResponses,
			aborted,
		)
	}
	if err := record.checkCompaction(); err != nil {
		return SessionRecord{}, err
	}
	return record, nil
}

func cloneRecord(record SessionRecord) (SessionRecord, error) {
	data, err := encodeRecord(record)
	if err != nil {
		return SessionRecord{}, err
	}
	return decodeRecord(data)
}
