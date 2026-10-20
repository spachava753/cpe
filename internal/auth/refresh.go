package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/afero"
)

const refreshAttemptTimeout = 30 * time.Second

// RefreshOpenAI renews the stored snapshot previous and saves the new tokens.
// Exchange retries stop at expiry; saving rotated tokens retries until saved or ctx ends.
// Concurrent login/logout wins; already-expired tokens get one bounded exchange attempt.
func (store *Store) RefreshOpenAI(ctx context.Context, filename string, previous OAuthCredentials, client *http.Client) (*OAuthCredentials, error) {
	if err := store.configured(filename); err != nil {
		return nil, err
	}
	if client == nil || store.refreshGate == nil {
		return nil, errors.New("credential refresh requires an HTTP client and configured store")
	}
	if err := (Credentials{
		OpenAI: &previous,
	}).check(); err != nil {
		return nil, err
	}
	retry := previous.ExpiresAt.After(time.Now())
	deadline := previous.ExpiresAt
	if !retry {
		deadline = time.Now().Add(refreshAttemptTimeout)
	}
	lifetime := ctx
	ctx, cancel := context.WithDeadline(
		ctx,
		deadline,
	)
	defer cancel()
	select {
	case store.refreshGate <- struct{}{}:
		defer func() { <-store.refreshGate }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	// A separate lock serializes rotating-token exchanges across processes without
	// blocking credential reads or login/logout during a slow refresh request.
	var lock afero.File
	defer func() {
		if lock != nil {
			closeCredentialLock(lock)
		}
	}()
	httpClient := *client
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	oauth := openAIClient{
		client: &httpClient,
		issuer: "https://auth.openai.com",
	}
	var refreshed *OAuthCredentials
	delay := time.Second
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var err error
		if lock == nil {
			err = checkCredentialDirectory(
				ctx,
				store.filesystem,
				filename,
				false,
			)
			if err == nil {
				lock, err = lockCredentials(
					ctx,
					store.filesystem,
					filename+".refresh",
				)
			}
		}
		var current Credentials
		if err == nil {
			current, err = store.Retrieve(
				ctx,
				filename,
			)
		}
		if err == nil && (current.OpenAI == nil || *current.OpenAI != previous) {
			return current.OpenAI, nil
		}
		if err == nil && refreshed == nil {
			attempt, cancelAttempt := context.WithTimeout(
				ctx,
				refreshAttemptTimeout,
			)
			refreshed, err = oauth.refresh(
				attempt,
				previous,
			)
			cancelAttempt()
			if err == nil {
				cancel()
				// Rotation has succeeded. The old access-token deadline must not
				// discard its replacement while persistence is still pending.
				ctx = lifetime
			}
		}
		if err == nil {
			var saved *OAuthCredentials
			saved, err = store.saveRefreshed(
				ctx,
				filename,
				previous,
				*refreshed,
			)
			if err == nil {
				return saved, nil
			}
		}
		if !retry && refreshed == nil {
			return nil, err
		}
		// Retain rotated tokens across failed saves instead of exchanging the old
		// refresh token again. Only durable credentials are returned to callers.
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
		delay = min(
			2*delay,
			30*time.Second,
		)
	}
}

func (store *Store) saveRefreshed(ctx context.Context, filename string, previous, refreshed OAuthCredentials) (*OAuthCredentials, error) {
	if err := store.enter(ctx); err != nil {
		return nil, err
	}
	defer store.leave()
	if err := checkCredentialDirectory(
		ctx,
		store.filesystem,
		filename,
		false,
	); err != nil {
		return nil, err
	}
	lock, err := lockCredentials(
		ctx,
		store.filesystem,
		filename,
	)
	if err != nil {
		return nil, err
	}
	defer closeCredentialLock(lock)
	document, err := readCredentials(
		store.filesystem,
		filename,
	)
	if err != nil {
		return nil, err
	}
	if document.OpenAI == nil || *document.OpenAI != previous {
		return document.OpenAI, nil
	}
	document.OpenAI = &refreshed
	if err := writeCredentials(
		ctx,
		store.filesystem,
		filename,
		document,
	); err != nil {
		return nil, err
	}
	return document.OpenAI, nil
}

func (client openAIClient) refresh(ctx context.Context, previous OAuthCredentials) (*OAuthCredentials, error) {
	values := url.Values{
		"grant_type": {
			"refresh_token",
		},
		clientIDParameter: {
			openAIClientID,
		},
		"refresh_token": {
			previous.RefreshToken,
		},
	}
	response, err := client.post(
		ctx,
		"/oauth/token",
		"application/x-www-form-urlencoded",
		strings.NewReader(values.Encode()),
	)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"OpenAI token refresh failed (HTTP %d)",
			response.StatusCode,
		)
	}
	var tokens tokenResponse
	if err := decodeAuthResponse(
		response.Body,
		&tokens,
	); err != nil {
		return nil, err
	}
	if tokens.AccessToken == "" {
		return nil, errors.New("OpenAI refresh returned no access token")
	}
	refreshed := previous
	refreshed.AccessToken = tokens.AccessToken
	if tokens.RefreshToken != "" {
		refreshed.RefreshToken = tokens.RefreshToken
	}
	if tokens.IDToken != "" {
		refreshed.IDToken = tokens.IDToken
	}
	account := accountID(tokens.IDToken)
	if account == "" {
		account = accountID(tokens.AccessToken)
	}
	if account != "" && account != previous.AccountID {
		return nil, errors.New("OpenAI refresh changed the ChatGPT account")
	}
	expires := int64(3600)
	if tokens.ExpiresIn != nil {
		expires = *tokens.ExpiresIn
	}
	if expires <= 0 || expires > int64((365*24*time.Hour)/time.Second) {
		return nil, errors.New("OpenAI returned an invalid token lifetime")
	}
	refreshed.ExpiresAt = time.Now().UTC().Add(time.Duration(expires) * time.Second)
	if err := (Credentials{
		OpenAI: &refreshed,
	}).check(); err != nil {
		return nil, err
	}
	return &refreshed, nil
}
