package models

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"

	"github.com/spf13/afero"
)

// LastUsed stores the model to prefer for new sessions, separately from session selections.
// Reads and writes are safe across concurrent sessions; writes replace the file atomically.
type LastUsed struct {
	filesystem afero.Fs
	filename   string
	mu         sync.Mutex
}

type lastUsedModel struct {
	Model ID `json:"model"`
}

// NewLastUsed opens a preference store at an absolute path without reading or writing it.
func NewLastUsed(filename string) (*LastUsed, error) {
	if !filepath.IsAbs(filename) {
		return nil, errors.New("last-used model path must be absolute")
	}
	return &LastUsed{
		filesystem: afero.NewOsFs(),
		filename:   filename,
	}, nil
}

// Load returns the remembered model, or an empty ID if no preference has been saved.
func (s *LastUsed) Load(ctx context.Context) (ID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	data, err := afero.ReadFile(
		s.filesystem,
		s.filename,
	)
	if errors.Is(
		err,
		fs.ErrNotExist,
	) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf(
			"read last-used model: %w",
			err,
		)
	}
	var saved lastUsedModel
	if err := json.Unmarshal(
		data,
		&saved,
	); err != nil {
		return "", fmt.Errorf(
			"decode last-used model: %w",
			err,
		)
	}
	if strings.TrimSpace(string(saved.Model)) == "" {
		return "", errors.New("last-used model ID is empty")
	}
	return saved.Model, nil
}

// Save remembers a model for future sessions without changing any existing session.
func (s *LastUsed) Save(ctx context.Context, model ID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(string(model)) == "" {
		return errors.New("last-used model ID is empty")
	}
	data, err := json.Marshal(lastUsedModel{
		Model: model,
	})
	if err != nil {
		return err
	}
	directory := filepath.Dir(s.filename)
	if mkdirErr := s.filesystem.MkdirAll(
		directory,
		0o700,
	); mkdirErr != nil {
		return mkdirErr
	}
	file, err := afero.TempFile(
		s.filesystem,
		directory,
		".last-model-",
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
		s.filename,
	)
}
