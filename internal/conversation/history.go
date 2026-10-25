package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spachava753/gai"
	"github.com/spf13/afero"

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

// Record checkpoints conversation, configuration, and session KV data, never live resources.
type Record struct {
	Info       Info
	Usage      TokenUsage
	Compaction *Compaction
	// KV holds opaque runtime-encoded values by session-local key.
	KV      map[string][]byte
	Options models.Selection
	Dialog  gai.Dialog
}

// Query filters saved conversations. Cursor is the last returned session ID.
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

// NewStore stores JSON checkpoints beneath directory with private permissions.
// The store is safe for concurrent sessions in one process.
func NewStore(directory string) (Store, error) {
	if !filepath.IsAbs(directory) {
		return nil, errors.New("session directory must be absolute")
	}
	return &fileSessionStore{
		filesystem: afero.NewOsFs(),
		directory:  directory,
	}, nil
}

type fileSessionStore struct {
	filesystem afero.Fs
	directory  string
	mu         sync.Mutex
}

func (s *fileSessionStore) filename(id ID) (string, error) {
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

func (s *fileSessionStore) Save(ctx context.Context, record Record) error {
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
	if mkdirErr := s.filesystem.MkdirAll(
		s.directory,
		0o700,
	); mkdirErr != nil {
		return mkdirErr
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

func (s *fileSessionStore) Load(ctx context.Context, id ID) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	filename, err := s.filename(id)
	if err != nil {
		return Record{}, err
	}
	data, err := afero.ReadFile(
		s.filesystem,
		filename,
	)
	if err != nil {
		return Record{}, err
	}
	record, err := decodeRecord(data)
	if err == nil && record.Info.ID != id {
		return Record{}, errors.New("stored session ID mismatch")
	}
	return record, err
}

func (s *fileSessionStore) List(ctx context.Context, query Query) (Page, error) {
	if query.Limit == 0 {
		return Page{}, errors.New("page limit must be positive")
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
		return Page{}, nil
	}
	if err != nil {
		return Page{}, err
	}
	var infos []Info
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return Page{}, err
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
			return Page{}, err
		}
		record, err := decodeRecord(data)
		if err != nil {
			return Page{}, fmt.Errorf(
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
	page := Page{
		Sessions: infos,
	}
	if uint(len(infos)) > query.Limit {
		page.Sessions = infos[:query.Limit]
		page.NextCursor = string(page.Sessions[len(page.Sessions)-1].ID)
	}
	return page, nil
}

func (s *fileSessionStore) Delete(ctx context.Context, id ID) error {
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

// Byte keys preserve Starlark strings that are not valid UTF-8.
type storedKVEntry struct {
	Key   []byte
	Value []byte
}

type storedRecord struct {
	Info       Info
	Options    models.Selection
	Dialog     []storedMessage
	Compaction *Compaction
	Usage      TokenUsage
	KV         []storedKVEntry
}

func encodeRecord(record Record) ([]byte, error) {
	if err := record.Validate(); err != nil {
		return nil, err
	}
	stored := storedRecord{
		Info:       record.Info,
		Options:    record.Options,
		Compaction: record.Compaction,
		Usage:      record.Usage,
	}
	for _, key := range slices.Sorted(maps.Keys(record.KV)) {
		stored.KV = append(
			stored.KV,
			storedKVEntry{
				Key:   []byte(key),
				Value: record.KV[key],
			},
		)
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

func decodeRecord(data []byte) (Record, error) {
	var stored storedRecord
	if err := json.Unmarshal(
		data,
		&stored,
	); err != nil {
		return Record{}, err
	}
	record := Record{
		Info:       stored.Info,
		Options:    stored.Options,
		Compaction: stored.Compaction,
		Usage:      stored.Usage,
	}
	if len(stored.KV) != 0 {
		record.KV = make(map[string][]byte)
		for _, entry := range stored.KV {
			record.KV[string(entry.Key)] = entry.Value
		}
	}

	for _, item := range stored.Dialog {
		record.Dialog = append(
			record.Dialog,
			restoreMessage(item),
		)
	}
	if err := record.Validate(); err != nil {
		return Record{}, err
	}
	return record, nil
}

// Clone returns a deep copy, preserving provider metadata and block contents.
func (record Record) Clone() (Record, error) {
	data, err := encodeRecord(record)
	if err != nil {
		return Record{}, err
	}
	return decodeRecord(data)
}

// Validate checks checkpoint invariants without accessing the filesystem.
func (record Record) Validate() error {
	if err := record.checkCompaction(); err != nil {
		return err
	}
	return record.Usage.check()
}
