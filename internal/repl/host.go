package repl

import (
	"context"
	"errors"
	"time"

	"github.com/spachava753/dyson"
)

type httpClient struct {
	j    *journal
	host dyson.HTTPClient
}

func (h httpClient) Do(ctx context.Context, r dyson.HTTPRequest) (dyson.HTTPResponse, error) {
	return boundary(h.j, "http.request", r, func() (dyson.HTTPResponse, error) {
		if h.host == nil {
			return dyson.HTTPResponse{}, errors.New("HTTP capability is unavailable")
		}
		return h.host.Do(ctx, r)
	})
}

type commandRunner struct {
	j    *journal
	host dyson.CommandRunner
	cwd  string
}

func (h commandRunner) RunCommand(ctx context.Context, c dyson.Command) (dyson.CommandResult, error) {
	if c.Dir == "" {
		c.Dir = h.cwd
	}
	// Agent commands cannot inherit the TUI's input or write over its renderer.
	if c.Stdin == dyson.StreamInherit {
		c.Stdin = dyson.StreamDiscard
	}
	if c.Stdout == dyson.StreamInherit {
		c.Stdout = dyson.StreamPipe
	}
	if c.Stderr == dyson.StreamInherit {
		c.Stderr = dyson.StreamPipe
	}
	return boundary(h.j, "command.run", c, func() (dyson.CommandResult, error) {
		if h.host == nil {
			return dyson.CommandResult{}, errors.New("command capability is unavailable")
		}
		return h.host.RunCommand(ctx, c)
	})
}

type clock struct {
	j    *journal
	host dyson.Clock
}

func (h clock) Now() time.Time {
	if !h.j.active {
		return h.j.store.Path()[0].Timestamp
	}
	v, err := boundary(h.j, "clock.now", nil, func() (time.Time, error) {
		if h.host == nil {
			return time.Time{}, errors.New("clock capability is unavailable")
		}
		return h.host.Now(), nil
	})
	if err != nil {
		// Dyson's Clock interface cannot return an error. Refuse to commit a
		// chunk that would otherwise consume an invented zero timestamp.
		h.j.fatal = err
	}
	return v
}
func (h clock) Sleep(ctx context.Context, d time.Duration) error {
	return effect(h.j, "clock.sleep", d, func() error {
		if h.host == nil {
			return errors.New("clock capability is unavailable")
		}
		return h.host.Sleep(ctx, d)
	})
}

type directory string

func (d directory) Getwd() (string, error) { return string(d), nil }
func (directory) Chdir(string) error {
	return errors.New("working directory is fixed for this session; use explicit paths or subprocess cwd")
}
