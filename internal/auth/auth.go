package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Credential represents stored authentication credentials
type Credential struct {
	Type         string `json:"type"`     // "oauth"
	Provider     string `json:"provider"` // "anthropic"
	AccessToken  string `json:"access"`
	RefreshToken string `json:"refresh"`
	ExpiresAt    int64  `json:"expires"`
}

// IsExpired returns true if the credential has expired
func (c *Credential) IsExpired() bool {
	return c.ExpiresAt > 0 && time.Now().Unix() >= c.ExpiresAt
}

// Store manages credential persistence
type Store struct {
	path string
	mu   sync.RWMutex
}

// NewStore creates a new credential store at the default location (~/.config/cpe/auth.json)
func NewStore() (*Store, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("getting user config dir: %w", err)
	}

	cpeDir := filepath.Join(configDir, "cpe")
	if err := os.MkdirAll(cpeDir, 0700); err != nil {
		return nil, fmt.Errorf("creating config dir: %w", err)
	}

	return &Store{
		path: filepath.Join(cpeDir, "auth.json"),
	}, nil
}

// loadAll reads all credentials from disk
func (s *Store) loadAll() (map[string]*Credential, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return make(map[string]*Credential), nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading auth file: %w", err)
	}

	var creds map[string]*Credential
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("parsing auth file: %w", err)
	}

	if creds == nil {
		creds = make(map[string]*Credential)
	}
	return creds, nil
}

// saveAll writes all credentials to disk
func (s *Store) saveAll(creds map[string]*Credential) error {
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling credentials: %w", err)
	}

	if err := os.WriteFile(s.path, data, 0600); err != nil {
		return fmt.Errorf("writing auth file: %w", err)
	}

	return nil
}

// GetCredential retrieves a credential for the given provider
func (s *Store) GetCredential(provider string) (*Credential, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	creds, err := s.loadAll()
	if err != nil {
		return nil, err
	}

	cred, ok := creds[provider]
	if !ok {
		return nil, fmt.Errorf("no credential found for provider %q", provider)
	}

	return cred, nil
}

// SaveCredential stores a credential for the given provider
func (s *Store) SaveCredential(cred *Credential) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	creds, err := s.loadAll()
	if err != nil {
		return err
	}

	creds[cred.Provider] = cred
	return s.saveAll(creds)
}

// DeleteCredential removes a credential for the given provider
func (s *Store) DeleteCredential(provider string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	creds, err := s.loadAll()
	if err != nil {
		return err
	}

	if _, ok := creds[provider]; !ok {
		return fmt.Errorf("no credential found for provider %q", provider)
	}

	delete(creds, provider)
	return s.saveAll(creds)
}

// ListCredentials returns all stored providers
func (s *Store) ListCredentials() ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	creds, err := s.loadAll()
	if err != nil {
		return nil, err
	}

	var providers []string
	for p := range creds {
		providers = append(providers, p)
	}
	return providers, nil
}

// GetCredential is a convenience function that creates a store and retrieves a credential
func GetCredential(provider string) (*Credential, error) {
	store, err := NewStore()
	if err != nil {
		return nil, err
	}
	return store.GetCredential(provider)
}
