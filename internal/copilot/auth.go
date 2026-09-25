package copilot

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/gofrs/flock"

	"github.com/spachava753/cpe/internal/jsonconfig"
)

type credential struct {
	Version     int    `json:"version"`
	GitHubToken string `json:"github_token"`
	Token       string `json:"token"`
	ExpiresAt   int64  `json:"expires_at"`
	APIURL      string `json:"api_url"`
}

type credentialStore struct {
	dir    string
	client *http.Client
}

// HasCredential checks local credentials without contacting GitHub.
func HasCredential(dir string) bool {
	_, err := (&credentialStore{dir: dir}).read()
	return err == nil
}

func (s *credentialStore) read() (credential, error) {
	path := filepath.Join(s.dir, credentialFile)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return credential{}, errors.New("missing or invalid CPE Copilot credentials; use /login copilot")
	}
	data, err := os.ReadFile(path)
	var saved credential
	if err != nil || jsonconfig.Decode(data, &saved) != nil || saved.Version != 1 || !validToken(saved.GitHubToken) || !validToken(saved.Token) || saved.ExpiresAt <= 0 {
		return credential{}, errors.New("invalid CPE Copilot credentials; use /login copilot")
	}
	saved.APIURL, err = parseAPIURL(saved.APIURL)
	if err != nil {
		return credential{}, err
	}
	return saved, nil
}

func (s *credentialStore) lock(ctx context.Context) (*flock.Flock, error) {
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return nil, err
	}
	l := flock.New(filepath.Join(s.dir, credentialFile+".lock"), flock.SetPermissions(0600))
	held, err := l.TryLockContext(ctx, 50*time.Millisecond)
	if err != nil || !held {
		_ = l.Close()
		if err == nil {
			err = ctx.Err()
		}
		return nil, err
	}
	return l, nil
}

func (s *credentialStore) current(ctx context.Context) (credential, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	l, err := s.lock(ctx)
	if err != nil {
		return credential{}, err
	}
	defer l.Close()
	saved, err := s.read()
	if err != nil {
		return credential{}, err
	}
	if time.Now().Add(time.Minute).Unix() < saved.ExpiresAt {
		return saved, nil
	}
	next, err := exchange(ctx, s.client, saved.GitHubToken)
	if err != nil {
		return credential{}, err
	}
	if err := s.write(ctx, next); err != nil {
		return credential{}, err
	}
	return next, nil
}

func (s *credentialStore) save(ctx context.Context, saved credential) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	l, err := s.lock(ctx)
	if err != nil {
		return err
	}
	defer l.Close()
	return s.write(ctx, saved)
}

// write is called only under the stable credential lock, with parsed credentials.
func (s *credentialStore) write(ctx context.Context, saved credential) error {
	path := filepath.Join(s.dir, credentialFile)
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return errors.New("copilot credential file must be regular, not a symlink")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	data, err := json.Marshal(saved)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(s.dir, ".cpe-copilot-*")
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
	dir, err := os.Open(s.dir)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func exchange(ctx context.Context, client *http.Client, githubToken string) (credential, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tokenURL, nil)
	if err != nil {
		return credential{}, err
	}
	clientHeaders(req)
	req.Header.Set("Authorization", "token "+githubToken)
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return credential{}, err
	}
	var result struct {
		Token     string `json:"token"`
		ExpiresAt int64  `json:"expires_at"`
		Endpoints struct {
			API string `json:"api"`
		} `json:"endpoints"`
	}
	if err := readJSON(resp, &result); err != nil {
		return credential{}, err
	}
	if !validToken(result.Token) || result.ExpiresAt <= time.Now().Unix() || result.ExpiresAt > time.Now().Add(7*24*time.Hour).Unix() {
		return credential{}, errors.New("invalid GitHub Copilot token response")
	}
	endpoint := result.Endpoints.API
	if endpoint == "" {
		endpoint = apiURL
	}
	endpoint, err = parseAPIURL(endpoint)
	if err != nil {
		return credential{}, err
	}
	return credential{Version: 1, GitHubToken: githubToken, Token: result.Token, ExpiresAt: result.ExpiresAt, APIURL: endpoint}, nil
}
