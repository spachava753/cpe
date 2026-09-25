package copilot

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"time"

	"github.com/spachava753/cpe/internal/authstore"
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
	entry := authstore.Entry{Path: filepath.Join(s.dir, credentialFile), Service: credentialName}
	raw, err := entry.Read()
	if err != nil {
		return credential{}, err
	}
	return parseCredential(raw)
}

func (s *credentialStore) current(ctx context.Context) (credential, error) {

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	entry := authstore.Entry{Path: filepath.Join(s.dir, credentialFile), Service: credentialName}
	var saved credential
	err := entry.Update(ctx, func(raw json.RawMessage) (json.RawMessage, error) {
		var err error
		saved, err = parseCredential(raw)
		if err != nil {
			return nil, err
		}
		if time.Now().Add(time.Minute).Unix() < saved.ExpiresAt {
			return nil, nil
		}
		saved, err = exchange(ctx, s.client, saved.GitHubToken)
		if err != nil {
			return nil, err
		}
		return json.Marshal(saved)
	})
	if err != nil {
		return credential{}, err
	}
	return saved, nil
}

func parseCredential(raw json.RawMessage) (credential, error) {
	var saved credential
	if jsonconfig.Decode(raw, &saved) != nil || saved.Version != 1 || !validToken(saved.GitHubToken) || !validToken(saved.Token) || saved.ExpiresAt <= 0 {
		return credential{}, errors.New("invalid CPE Copilot credentials; use /login copilot")
	}
	var err error
	saved.APIURL, err = parseAPIURL(saved.APIURL)
	if err != nil {
		return credential{}, err
	}
	return saved, nil
}

func (s *credentialStore) save(ctx context.Context, saved credential) error {
	entry := authstore.Entry{Path: filepath.Join(s.dir, credentialFile), Service: credentialName}
	return entry.Update(ctx, func(json.RawMessage) (json.RawMessage, error) {
		return json.Marshal(saved)
	})
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
