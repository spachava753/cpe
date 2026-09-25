package copilot

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/spachava753/cpe/internal/config"
)

// Login displays a GitHub device code, imports available Copilot profiles, and
// saves credentials. notify is UI-only. It must not append conversation messages.
func Login(ctx context.Context, dir string, notify func(string)) (map[string]config.Model, error) {
	return (&loginClient{client: newClient(), wait: wait}).login(ctx, dir, notify)
}

type loginClient struct {
	client *http.Client
	wait   func(context.Context, time.Duration) error
}

func wait(ctx context.Context, interval time.Duration) error {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (l *loginClient) login(ctx context.Context, dir string, notify func(string)) (map[string]config.Model, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	githubToken, err := l.authorize(ctx, notify)
	if err != nil {
		return nil, err
	}
	saved, err := exchange(ctx, l.client, githubToken)
	if err != nil {
		return nil, err
	}
	models, err := discover(ctx, l.client, saved)
	if err != nil {
		return nil, err
	}
	profiles, err := config.AddModels(ctx, dir, models)
	if err != nil {
		return nil, fmt.Errorf("import Copilot profiles: %w", err)
	}
	store := credentialStore{dir: dir, client: l.client}
	if err := store.save(ctx, saved); err != nil {
		return nil, fmt.Errorf("save Copilot credentials (profiles imported; retry /login copilot): %w", err)
	}
	return profiles, nil
}

func (l *loginClient) authorize(ctx context.Context, notify func(string)) (string, error) {
	var device struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		ExpiresIn       int64  `json:"expires_in"`
		Interval        int64  `json:"interval"`
	}
	if err := postForm(ctx, l.client, deviceURL, url.Values{"client_id": {clientID}, "scope": {"read:user"}}, &device); err != nil {
		return "", err
	}
	if !validToken(device.DeviceCode) || !validToken(device.UserCode) || len(device.UserCode) > 64 || device.VerificationURI != verificationURL || device.ExpiresIn <= 0 || device.ExpiresIn > 3600 || device.Interval < 0 || device.Interval > 60 {
		return "", errors.New("invalid GitHub device authorization response")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(device.ExpiresIn)*time.Second)
	defer cancel()
	interval := time.Duration(device.Interval) * time.Second
	if interval == 0 {
		interval = 5 * time.Second
	}
	if notify != nil {
		notify("Sign in to GitHub Copilot\n\nOpen " + verificationURL + "\n\nEnter code: " + device.UserCode + "\n\nAuthorize GitHub Copilot, then return here. Waiting for approval. Esc cancels.")
	}
	for {
		if err := l.wait(ctx, interval); err != nil {
			return "", err
		}
		var result struct {
			AccessToken string `json:"access_token"`
			TokenType   string `json:"token_type"`
			Error       string `json:"error"`
			Interval    int64  `json:"interval"`
		}
		if err := postForm(ctx, l.client, pollURL, url.Values{"client_id": {clientID}, "device_code": {device.DeviceCode}, "grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}}, &result); err != nil {
			return "", err
		}
		if result.Error == "" && validToken(result.AccessToken) && strings.EqualFold(result.TokenType, "bearer") {
			return result.AccessToken, nil
		}
		switch result.Error {
		case "authorization_pending":
		case "slow_down":
			interval += 5 * time.Second
			if result.Interval > 0 && result.Interval <= 3600 {
				interval = max(interval, time.Duration(result.Interval)*time.Second)
			}
		case "access_denied":
			return "", errors.New("GitHub Copilot login was denied; use /login copilot to try again")
		case "expired_token":
			return "", errors.New("GitHub Copilot device code expired; use /login copilot to try again")
		default:
			return "", errors.New("GitHub Copilot authorization failed; use /login copilot to try again")
		}
	}
}
