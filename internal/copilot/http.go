package copilot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/spachava753/cpe/internal/version"
)

const (
	credentialName    = "github-copilot"
	credentialFile    = "copilot.json"
	apiURL            = "https://api.githubcopilot.com"
	tokenURL          = "https://api.github.com/copilot_internal/v2/token"
	deviceURL         = "https://github.com/login/device/code"
	pollURL           = "https://github.com/login/oauth/access_token"
	verificationURL   = "https://github.com/login/device"
	clientID          = "Iv1.b507a08c87ecfe98"
	chatProtocol      = "openai"
	responsesProtocol = "responses"
	messagesProtocol  = "anthropic"
)

func newClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func validToken(value string) bool {
	return value != "" && !strings.ContainsFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
}

func parseAPIURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") || u.RawPath != "" {
		return "", errors.New("unsupported GitHub Copilot API endpoint")
	}
	switch u.Host {
	case "api.githubcopilot.com", "api.individual.githubcopilot.com", "api.business.githubcopilot.com", "api.enterprise.githubcopilot.com":
		return "https://" + u.Host, nil
	default:
		return "", errors.New("unsupported GitHub Copilot API host")
	}
}

func clientHeaders(req *http.Request) {
	identity := "cpe/" + version.Get()
	req.Header.Set("User-Agent", identity)
	req.Header.Set("Editor-Version", identity)
	req.Header.Set("Editor-Plugin-Version", identity)
	req.Header.Set("Copilot-Integration-Id", "vscode-chat")
	req.Header.Set("X-GitHub-Api-Version", "2025-04-01")
}

// readJSON never includes upstream content in errors: OAuth bodies contain secrets.
func readJSON(resp *http.Response, target any) error {
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub Copilot request failed (HTTP %d); check account access or use /login copilot", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	if err != nil || len(data) > 4<<20 || json.Unmarshal(data, target) != nil {
		return errors.New("invalid GitHub Copilot response")
	}
	return nil
}

func postForm(ctx context.Context, client *http.Client, endpoint string, form url.Values, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	clientHeaders(req)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	return readJSON(resp, target)
}
