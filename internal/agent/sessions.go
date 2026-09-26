package agent

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/repl"
	"github.com/spachava753/cpe/internal/session"
	"github.com/spachava753/cpe/internal/skills"
)

// SessionOptions supplies process-scoped dependencies shared across conversations.
// LoadConfig reads current defaults; NewGenerator binds providers to the target
// journal's root ID. Neither callback may generate model output.
type SessionOptions struct {
	Directory    string
	CWD          string
	Tools        []repl.Tool
	Skills       skills.Catalog
	LoadConfig   func(context.Context) (config.Config, error)
	NewGenerator func(context.Context, config.Model, string) (gai.Generator, error)
}

// Conversation is the active agent and its startup settings. Its lifetime belongs
// to Sessions. Callers must join agent work before opening another conversation.
type Conversation struct {
	Agent  *Agent
	Config config.Config
	Name   string
}

// SessionStart selects a fresh conversation (empty Path) or a saved journal.
// Checkpoint selects an earlier branch before startup replay; Model overrides
// the saved default for this opening only.
type SessionStart struct {
	Path       string
	Model      string
	Checkpoint string
}

// Sessions owns one active conversation and its journal. All methods, including
// Generator, are restricted to operation boundaries except during ordinary agent
// generation, when Generator may construct a queued model selection.
type Sessions struct {
	opts    SessionOptions
	current *Conversation
}

func NewSessions(opts SessionOptions) *Sessions { return &Sessions{opts: opts} }

// List returns current-directory sessions without modifying or opening them.
func (s *Sessions) List(ctx context.Context) ([]session.Info, error) {
	return session.List(ctx, s.opts.Directory, s.opts.CWD)
}

// Open creates a fresh session when Path is empty, or resumes an existing file.
// Empty Model chooses the saved default. Restoration is staged before releasing
// the old conversation. A nonnil result means the switch committed, even if
// closing old resources reported an error. Cancellation before commit preserves
// the old agent. Opening the current file is a no-op.
func (s *Sessions) Open(ctx context.Context, start SessionStart) (_ *Conversation, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, name := start.Path, start.Model
	if start.Checkpoint != "" && (path == "" || s.current != nil) {
		return nil, errors.New("checkpoint selection requires startup resume; use /branch in an active conversation")
	}
	fresh := path == ""
	if !fresh {
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("resume: %w", err)
		}
		if !info.Mode().IsRegular() {
			return nil, errors.New("resume requires a regular session file")
		}
		if s.current != nil {
			old, err := os.Stat(s.current.Agent.SessionFile())
			if err == nil && os.SameFile(info, old) {
				return s.current, nil
			}
		}
	}
	cfg, err := s.opts.LoadConfig(ctx)
	if err != nil {
		return nil, err
	}
	if name == "" {
		name = cfg.DefaultModel
	}
	profile, ok := cfg.Models[name]
	if !ok {
		return nil, fmt.Errorf("unknown model profile %q", name)
	}
	if fresh {
		path = filepath.Join(s.opts.Directory, time.Now().UTC().Format("20060102T150405")+"_"+rand.Text()+".jsonl")
	}
	open := session.Resume
	if fresh {
		open = session.Open
	}
	store, err := open(path, s.opts.CWD, repl.Runtime)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = store.Close()
			if fresh {
				_ = os.Remove(path)
			}
		}
	}()
	if start.Checkpoint != "" {
		if err := store.Branch(start.Checkpoint); err != nil {
			return nil, err
		}
	}
	generator, err := s.opts.NewGenerator(ctx, profile, store.Entries()[0].ID)
	if err != nil {
		return nil, err
	}
	rootID := store.Entries()[0].ID
	a, err := Open(ctx, Options{NewTitleGenerator: func(ctx context.Context, model config.Model) (gai.Generator, error) {
		return s.opts.NewGenerator(ctx, model, rootID)
	}, Config: cfg, Model: profile, Generator: generator, Store: store, CWD: s.opts.CWD, Tools: s.opts.Tools, Skills: s.opts.Skills})
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		_ = a.Close()
		return nil, err
	}
	next := &Conversation{Agent: a, Config: cfg, Name: name}
	previous := s.current
	s.current, committed = next, true
	if previous != nil {
		err = errors.Join(previous.Agent.Close(), previous.Agent.opts.Store.Close())
	}
	return next, err
}

// Generator constructs a provider using the active journal's identity.
func (s *Sessions) Generator(ctx context.Context, profile config.Model) (gai.Generator, error) {
	return s.opts.NewGenerator(ctx, profile, s.current.Agent.opts.Store.Entries()[0].ID)
}

// Close releases the active interpreter and journal after all workers join.
func (s *Sessions) Close() error {
	if s.current == nil {
		return nil
	}
	err := errors.Join(s.current.Agent.Close(), s.current.Agent.opts.Store.Close())
	s.current = nil
	return err
}
