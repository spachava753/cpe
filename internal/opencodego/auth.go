package opencodego

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/spachava753/cpe/internal/authstore"
	"github.com/spachava753/cpe/internal/jsonconfig"
)

const credentialFile = "auth.json"

type credential struct {
	Key string `json:"key"`
}

func validKey(key string) bool {
	return key != "" && !strings.ContainsFunc(key, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
}

// HasKey reports whether CPE has a locally well-formed OpenCode Go API key.
// It does not contact the service or check subscription status.
func HasKey(dir string) bool {
	entry := authstore.Entry{Path: filepath.Join(dir, credentialFile), Service: credentialName}
	raw, err := entry.Read()
	if err != nil {
		return false
	}
	_, err = parseKey(raw)
	return err == nil
}

func readKey(ctx context.Context, dir string) (string, error) {
	entry := authstore.Entry{Path: filepath.Join(dir, credentialFile), Service: credentialName}
	var key string
	err := entry.Update(ctx, func(raw json.RawMessage) (json.RawMessage, error) {
		var err error
		key, err = parseKey(raw)
		return nil, err
	})
	if err != nil {
		return "", err
	}
	return key, nil
}

func parseKey(raw json.RawMessage) (string, error) {
	var saved credential
	if jsonconfig.Decode(raw, &saved) != nil || !validKey(saved.Key) {
		// JSON errors may contain the key. Never return them to the UI or model.
		return "", errors.New("cannot read OpenCode Go credential; use /login opencode-go")
	}
	return saved.Key, nil
}

func saveKey(ctx context.Context, dir, key string) error {
	if !validKey(key) {
		return errors.New("enter an OpenCode Go API key without whitespace")
	}
	entry := authstore.Entry{Path: filepath.Join(dir, credentialFile), Service: credentialName}
	return entry.Update(ctx, func(json.RawMessage) (json.RawMessage, error) {
		return json.Marshal(credential{Key: key})
	})
}
