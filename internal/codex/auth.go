package codex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode"
)

const providerKey = "openai-codex"
const clientIDKey = "client_id"
const windowsOS = "windows"

const clientID = "app_EMoamEEZ73f0CkXaXp7hrann"

type credential struct {
	access, refresh, accountID string
	expires                    int64 // Epoch milliseconds.
}

type credentialStore struct {
	path, tokenURL string
	client         *http.Client
}

func (s *credentialStore) credential(ctx context.Context, refresh bool) (credential, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	lock, err := acquireLock(ctx, s.path+".lock")
	if err != nil {
		return credential{}, err
	}
	defer lock.close()
	data, err := readCredentialFile(s.path)
	if err != nil {
		return credential{}, fmt.Errorf("read CPE OAuth file %s: %w; sign in with /login in CPE", s.path, err)
	}
	var document map[string]json.RawMessage
	var fields map[string]json.RawMessage
	var saved struct {
		Type      string `json:"type"`
		Access    string `json:"access"`
		Refresh   string `json:"refresh"`
		Expires   int64  `json:"expires"`
		AccountID string `json:"accountId"`
	}
	// Do not include decoding errors: malformed values may contain credentials.
	if json.Unmarshal(data, &document) != nil || json.Unmarshal(document[providerKey], &saved) != nil ||
		json.Unmarshal(document[providerKey], &fields) != nil || saved.Type != "oauth" ||
		!validHeaderValue(saved.Access) || !validHeaderValue(saved.Refresh) || saved.Expires <= 0 {
		return credential{}, errors.New("invalid or missing CPE Codex OAuth credential; sign in with /login in CPE")
	}
	cred := credential{access: saved.Access, refresh: saved.Refresh, expires: saved.Expires, accountID: saved.AccountID}
	if cred.accountID == "" {
		cred.accountID = accountFromToken(cred.access)
	}
	if !validHeaderValue(cred.accountID) {
		return credential{}, errors.New("saved CPE Codex credential has no valid account ID; sign in with /login in CPE")
	}
	if refresh && time.Now().Add(time.Minute).UnixMilli() >= cred.expires {
		cred, err = s.refresh(ctx, cred)
		if err != nil {
			return credential{}, err
		}
		for key, value := range map[string]any{"access": cred.access, "refresh": cred.refresh, "expires": cred.expires, "accountId": cred.accountID} {
			fields[key], err = json.Marshal(value)
			if err != nil {
				return credential{}, err
			}
		}
		document[providerKey], err = json.Marshal(fields)
		if err != nil {
			return credential{}, err
		}
		data, err = json.MarshalIndent(document, "", "  ")
		if err != nil {
			return credential{}, err
		}
		if err := saveCredentials(s.path, append(data, '\n')); err != nil {
			return credential{}, fmt.Errorf("save refreshed CPE OAuth credential: %w; sign in again if the saved token no longer works", err)
		}
	}
	return cred, nil
}

func validHeaderValue(value string) bool {
	return value != "" && !strings.ContainsFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
}

// Account extraction only selects a header; token verification belongs to OpenAI.
func accountFromToken(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return ""
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Auth struct {
			AccountID string `json:"chatgpt_account_id"`
		} `json:"https://api.openai.com/auth"`
	}
	if json.Unmarshal(data, &claims) != nil {
		return ""
	}
	return claims.Auth.AccountID
}

func (s *credentialStore) refresh(ctx context.Context, old credential) (credential, error) {
	form := url.Values{"grant_type": {"refresh_token"}, clientIDKey: {clientID}, "refresh_token": {old.refresh}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return credential{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.client.Do(req)
	if err != nil {
		return credential{}, fmt.Errorf("codex token refresh: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return credential{}, fmt.Errorf("codex token refresh failed (HTTP %d); sign in again with /login in CPE; refresh was not retried", resp.StatusCode)
	}
	var token struct {
		Access    string `json:"access_token"`
		Refresh   string `json:"refresh_token"`
		ExpiresIn int64  `json:"expires_in"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&token) != nil ||
		!validHeaderValue(token.Access) || token.ExpiresIn <= 0 || token.ExpiresIn > 366*24*3600 {
		return credential{}, errors.New("invalid Codex token refresh response; sign in again with /login in CPE")
	}
	if token.Refresh == "" {
		token.Refresh = old.refresh
	}
	if !validHeaderValue(token.Refresh) {
		return credential{}, errors.New("invalid Codex refresh token in response; sign in again with /login in CPE")
	}
	account := accountFromToken(token.Access)
	if account == "" {
		account = old.accountID
	}
	if !validHeaderValue(account) {
		return credential{}, errors.New("invalid account ID in refreshed Codex credential")
	}
	return credential{access: token.Access, refresh: token.Refresh, accountID: account, expires: time.Now().Add(time.Duration(token.ExpiresIn) * time.Second).UnixMilli()}, nil
}

func saveCredentials(path string, data []byte) error {
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return errors.New("OAuth credential file must be a regular file, not a symlink")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	target := path
	f, err := os.CreateTemp(filepath.Dir(target), ".cpe-oauth-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), target); err != nil {
		return err
	}
	if runtime.GOOS != windowsOS {
		dir, err := os.Open(filepath.Dir(target))
		if err != nil {
			return err
		}
		defer dir.Close()
		return dir.Sync()
	}
	return nil
}

func readCredentialFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("OAuth credential file must be a regular file, not a symlink")
	}
	return os.ReadFile(path)
}

func (s *credentialStore) save(ctx context.Context, cred credential) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	lock, err := acquireLock(ctx, s.path+".lock")
	if err != nil {
		return err
	}
	defer lock.close()
	document := map[string]json.RawMessage{}
	data, err := readCredentialFile(s.path)
	if err == nil {
		if json.Unmarshal(data, &document) != nil || document == nil {
			return errors.New("invalid auth.json; existing credentials were not overwritten")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	fields := map[string]json.RawMessage{}
	if raw := document[providerKey]; raw != nil {
		if json.Unmarshal(raw, &fields) != nil || fields == nil {
			fields = map[string]json.RawMessage{}
		}
	}
	for key, value := range map[string]any{"type": "oauth", "access": cred.access, "refresh": cred.refresh, "expires": cred.expires, "accountId": cred.accountID} {
		fields[key], err = json.Marshal(value)
		if err != nil {
			return err
		}
	}
	document[providerKey], err = json.Marshal(fields)
	if err != nil {
		return err
	}
	data, err = json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	return saveCredentials(s.path, append(data, '\n'))
}
