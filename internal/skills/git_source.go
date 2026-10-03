package skills

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// GitSourceClient acquires an isolated Git checkout using the installed git CLI.
// It accepts Git URLs and GitHub owner/repository shorthand.
type GitSourceClient struct{}

func (GitSourceClient) Fetch(ctx context.Context, source string) (Checkout, error) {
	if err := ctx.Err(); err != nil {
		return Checkout{}, err
	}
	locator, err := gitLocator(source)
	if err != nil {
		return Checkout{}, err
	}
	root, err := os.MkdirTemp("", "cpe-source-")
	if err != nil {
		return Checkout{}, fmt.Errorf("create source checkout: %w", err)
	}
	cleanup := func() error { return os.RemoveAll(root) }
	directory := filepath.Join(root, "repository")
	command := exec.CommandContext(ctx, "git", "clone", "--quiet", "--depth", "1", "--", locator, directory)
	// Do not hang waiting for interactive credentials; Git's configured credential
	// helpers and SSH authentication still work for explicit user invocations.
	command.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	output, err := command.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return Checkout{}, errors.Join(fmt.Errorf("clone source: %w: %s", err, terminalText(strings.TrimSpace(string(output)))), cleanup())
	}
	return Checkout{FS: os.DirFS(directory), Cleanup: cleanup}, nil
}

func gitLocator(source string) (string, error) {
	if !strings.Contains(source, "://") {
		parts := strings.Split(source, "/")
		if len(parts) != 2 || !validDirectoryName(parts[0]) || !validDirectoryName(parts[1]) || strings.ContainsAny(source, "@?# \t\r\n") {
			return "", errors.New("source must be a Git URL or GitHub owner/repository")
		}
		return "https://github.com/" + source, nil
	}
	locator, err := url.Parse(source)
	if err != nil {
		return "", errors.New("invalid Git source URL")
	}
	if locator.User != nil {
		if _, present := locator.User.Password(); present {
			return "", errors.New("source URL must not contain a password")
		}
	}
	if locator.Fragment != "" || locator.RawQuery != "" {
		return "", errors.New("source URL must not contain a query or fragment")
	}
	switch locator.Scheme {
	case "https", "http", "ssh", "git":
		if locator.Host == "" || locator.Path == "" || locator.Path == "/" {
			return "", errors.New("source URL requires a host and repository path")
		}
	case "file":
		if (locator.Host != "" && locator.Host != "localhost") || !filepath.IsAbs(locator.Path) {
			return "", errors.New("file source URL requires an absolute local repository path")
		}
	default:
		return "", errors.New("unsupported Git source URL scheme")
	}
	return locator.String(), nil
}
