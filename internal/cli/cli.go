package cli

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/spachava753/gai"
	"golang.org/x/term"

	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/codex"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/repl"
	"github.com/spachava753/cpe/internal/session"
	"github.com/spachava753/cpe/internal/tui"
	"github.com/spachava753/cpe/internal/version"
)

// Run executes a CLI invocation. The caller supplies process context and output.
func Run(ctx context.Context, args []string, out, errOut io.Writer) error {
	flags := flag.NewFlagSet("cpe", flag.ContinueOnError)
	flags.SetOutput(errOut)
	initConfig := flags.Bool("init", false, "create ~/.cpe/config.json and system.md")
	modelName := flags.String("model", "", "model profile from config.json")
	resume := flags.String("resume", "", "resume a session ID or JSONL path")
	latest := flags.Bool("continue", false, "continue the newest session in this directory")
	list := flags.Bool("sessions", false, "list sessions in this directory")
	branch := flags.String("branch", "", "branch a resumed session at a checkpoint ID")
	prompt := flags.String("prompt", "", "run one prompt without the TUI")
	showVersion := flags.Bool("version", false, "print version")
	flags.Usage = func() {
		fmt.Fprint(errOut, "Usage: cpe [flags]\n\nInteractive Starlark agent. Configuration: ~/.cpe/config.json and system.md\n\n")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments; use --prompt for non-interactive input")
	}
	if *showVersion {
		fmt.Fprintln(out, version.Get())
		return nil
	}
	if *initConfig {
		dir, err := config.Init()
		if err == nil {
			fmt.Fprintf(out, "Configuration ready in %s. Start CPE and use /login for Codex, or configure API-key credentials.\n", dir)
		}
		return err
	}
	if *resume != "" && *latest {
		return errors.New("use either --resume or --continue")
	}
	if *branch != "" && *resume == "" && !*latest {
		return errors.New("--branch requires --resume or --continue")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	dir, err := config.Directory()
	if err != nil {
		return err
	}
	if *list || *latest {
		sessions, err := listSessions(filepath.Join(dir, "sessions"), cwd)
		if err != nil {
			return err
		}
		if *list {
			for _, s := range sessions {
				fmt.Fprintln(out, s)
			}
			return nil
		}
		if len(sessions) == 0 {
			return errors.New("no sessions in this directory")
		}
		*resume = sessions[0]
	}
	c, err := config.Load()
	if err != nil {
		return err
	}
	if *modelName == "" {
		*modelName = c.DefaultModel
	}
	model, ok := c.Models[*modelName]
	if !ok {
		return fmt.Errorf("unknown model profile %q", *modelName)
	}
	generator, err := agent.Provider(ctx, model)
	if err != nil {
		return err
	}
	if *prompt == "" && (!term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd()))) {
		return errors.New("interactive mode requires a terminal; use --prompt")
	}
	path := *resume
	if path == "" {
		path = filepath.Join(c.Dir, "sessions", time.Now().UTC().Format("20060102T150405")+"_"+rand.Text()+".jsonl")
	} else {
		if filepath.Base(path) == path {
			path = filepath.Join(c.Dir, "sessions", path)
			if !strings.HasSuffix(path, ".jsonl") {
				path += ".jsonl"
			}
		}
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("resume: %w", err)
		}
	}
	store, err := session.Open(path, cwd, repl.Runtime)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	if *branch != "" {
		if err := store.Branch(*branch); err != nil {
			return err
		}
	}
	a, err := agent.Open(ctx, agent.Options{Config: c, Model: model, Generator: generator, Store: store, CWD: cwd})
	if err != nil {
		return err
	}
	defer func() { _ = a.Close() }()
	if *prompt != "" {
		fmt.Fprintf(errOut, "Session: %s\n", path)
		return a.Prompt(ctx, *prompt, func(e agent.Event) {
			if e.Kind == agent.EventMessage && e.Message != nil && e.Message.Role == gai.Assistant {
				for _, b := range e.Message.Blocks {
					if b.BlockType == gai.Content && b.Content != nil {
						fmt.Fprintln(out, b.Content.String())
					}
				}
			}
		})
	}
	authFile := filepath.Join(c.Dir, "auth.json")
	options := tui.Options{
		Models: c.Models,
		LoginRequired: func(profile config.Model) bool {
			_, err := os.Stat(authFile)
			return profile.Provider == "codex" && errors.Is(err, os.ErrNotExist)
		},
		Login: func(ctx context.Context, method string, notify func(string)) error {
			return codex.Login(ctx, authFile, method, notify)
		},
	}
	return tui.Run(ctx, a, *modelName, options)
}
func listSessions(dir, cwd string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	type item struct {
		path string
		time time.Time
	}
	var items []item
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		var e session.Entry
		err = json.NewDecoder(io.LimitReader(f, 1<<20)).Decode(&e)
		_ = f.Close()
		if err != nil || e.Type != "session" {
			continue
		}
		var h session.Header
		if json.Unmarshal(e.Data, &h) != nil || h.CWD != cwd {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		items = append(items, item{path, info.ModTime()})
	}
	slices.SortFunc(items, func(a, b item) int { return b.time.Compare(a.time) })
	var paths []string
	for _, i := range items {
		paths = append(paths, i.path)
	}
	return paths, nil
}
