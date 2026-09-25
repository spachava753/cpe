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
	"strings"
	"time"
	"unicode"

	"github.com/spachava753/cpe/internal/authstore"
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
	var cred credential
	entry := authstore.Entry{Path: s.path, Service: providerKey}
	err := entry.Refresh(ctx, func(raw json.RawMessage) (json.RawMessage, error) {
		var fields map[string]json.RawMessage
		var err error
		cred, err = parseCredential(raw)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil, errors.New("invalid CPE Codex credential")
		}
		if !refresh || time.Now().Add(time.Minute).UnixMilli() < cred.expires {
			return nil, nil
		}
		cred, err = s.refresh(ctx, cred)
		if err != nil {
			return nil, err
		}
		for key, value := range map[string]any{"access": cred.access, "refresh": cred.refresh, "expires": cred.expires, "accountId": cred.accountID} {
			fields[key], err = json.Marshal(value)
			if err != nil {
				return nil, err
			}
		}
		return json.Marshal(fields)
	})
	if err != nil {
		return credential{}, err
	}
	if err := ctx.Err(); err != nil {
		return credential{}, err
	}
	return cred, nil
}

// HasCredential checks the OpenAI entry locally, without refresh or file writes.
func HasCredential(path string) bool {
	raw, err := (authstore.Entry{Path: path, Service: providerKey}).Read()
	if err != nil {
		return false
	}
	_, err = parseCredential(raw)
	return err == nil
}

func parseCredential(raw json.RawMessage) (credential, error) {
	var saved struct {
		Type      string `json:"type"`
		Access    string `json:"access"`
		Refresh   string `json:"refresh"`
		Expires   int64  `json:"expires"`
		AccountID string `json:"accountId"`
	}
	// Do not include decoding errors: malformed values may contain credentials.
	if json.Unmarshal(raw, &saved) != nil || saved.Type != "oauth" ||
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

func (s *credentialStore) save(ctx context.Context, cred credential) error {
	entry := authstore.Entry{Path: s.path, Service: providerKey}
	return entry.Update(ctx, func(raw json.RawMessage) (json.RawMessage, error) {
		fields := map[string]json.RawMessage{}
		if json.Unmarshal(raw, &fields) != nil || fields == nil {
			fields = map[string]json.RawMessage{}
		}
		for key, value := range map[string]any{"type": "oauth", "access": cred.access, "refresh": cred.refresh, "expires": cred.expires, "accountId": cred.accountID} {
			encoded, err := json.Marshal(value)
			if err != nil {
				return nil, err
			}
			fields[key] = encoded
		}
		return json.Marshal(fields)
	})
}
