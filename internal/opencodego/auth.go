package opencodego

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"

	"github.com/spachava753/cpe/internal/jsonconfig"
)

const credentialFile = "opencode-go.json"

type credential struct {
	Key string `json:"key"`
}

func validKey(key string) bool {
	return key != "" && !strings.ContainsFunc(key, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
}

// HasKey reports whether CPE has a locally well-formed OpenCode Go API key.
// It does not contact the service or check subscription status.
func HasKey(dir string) bool {
	_, err := readKey(dir)
	return err == nil
}

func readKey(dir string) (string, error) {
	path := filepath.Join(dir, credentialFile)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("missing or invalid OpenCode Go credential; use /login opencode-go")
	}
	data, err := os.ReadFile(path)
	var saved credential
	if err != nil || jsonconfig.Decode(data, &saved) != nil || !validKey(saved.Key) {
		// JSON errors may contain the key. Never return them to the UI or model.
		return "", errors.New("cannot read OpenCode Go credential; use /login opencode-go")
	}
	return saved.Key, nil
}

func saveKey(ctx context.Context, dir, key string) error {
	if !validKey(key) {
		return errors.New("enter an OpenCode Go API key without whitespace")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	path := filepath.Join(dir, credentialFile)
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return errors.New("OpenCode Go credential file must be a regular file, not a symlink")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	data, err := json.Marshal(credential{Key: key})
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".cpe-go-key-*") // Always 0600, including replacements.
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		return nil
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
