package agent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/spachava753/cpe/internal/auth"
)

const credentialRefreshWindow = 5 * time.Minute

type credentialRefresh struct {
	previous auth.OAuthCredentials
	cancel   context.CancelFunc
	done     chan struct{}
	err      error
}

// Close stops and joins outstanding credential refresh work.
func (p *Providers) Close() {
	p.mu.Lock()
	p.closed = true
	p.stopRefresh()
	p.mu.Unlock()
	p.workers.Wait()
}

// stopRefresh requires mu. Workers never acquire mu.
func (p *Providers) stopRefresh() {
	if p.refresh != nil {
		p.refresh.cancel()
		<-p.refresh.done
		p.refresh = nil
	}
}

//nolint:contextcheck // Refresh belongs to the connection and is joined by Close/Logout, not the triggering turn.
func (p *Providers) openAICredentials(ctx context.Context) (*auth.OAuthCredentials, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, errors.New("provider connection is closed")
	}
	credentials, err := p.store.Retrieve(
		ctx,
		p.path,
	)
	if err != nil {
		p.mu.Unlock()
		return nil, err
	}
	tokens := credentials.OpenAI
	if p.refresh != nil && (tokens == nil || p.refresh.previous != *tokens) {
		p.stopRefresh()
	}
	if tokens == nil {
		p.mu.Unlock()
		return nil, errors.New("ChatGPT login missing; run cpe auth login openai")
	}
	valid := tokens.ExpiresAt.After(time.Now())
	if time.Until(tokens.ExpiresAt) > credentialRefreshWindow {
		p.mu.Unlock()
		return tokens, nil
	}
	if p.refresh != nil {
		select {
		case <-p.refresh.done:
			p.refresh.cancel()
			p.refresh = nil
		default:
		}
	}
	if p.refresh == nil {
		// The provider connection owns refresh retries, not the triggering turn.
		background, cancel := context.WithCancel(context.Background())
		job := &credentialRefresh{
			previous: *tokens,
			cancel:   cancel,
			done:     make(chan struct{}),
		}
		p.refresh = job
		p.workers.Go(func() {
			defer close(job.done)
			_, job.err = p.store.RefreshOpenAI(
				background,
				p.path,
				job.previous,
				p.client,
			)
		})
	}
	job := p.refresh
	p.mu.Unlock()
	if valid {
		return tokens, nil
	}
	// Expired credentials cannot be sent. An expired-on-startup refresh gets one
	// bounded attempt; an existing retry worker stops at the old token's expiry.
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-job.done:
	}
	if job.err != nil {
		return nil, fmt.Errorf(
			"refresh ChatGPT credentials: %w",
			job.err,
		)
	}
	credentials, err = p.store.Retrieve(
		ctx,
		p.path,
	)
	if err != nil {
		return nil, err
	}
	if credentials.OpenAI == nil || !credentials.OpenAI.ExpiresAt.After(time.Now()) {
		return nil, errors.New("ChatGPT login missing or expired; run cpe auth login openai")
	}
	return credentials.OpenAI, nil
}

// credentialError prevents model-response retries from extending credential retries past expiry.
type credentialError struct {
	cause error
}

func (e *credentialError) Error() string { return e.cause.Error() }
func (e *credentialError) Unwrap() error { return e.cause }

type subscriptionTransport struct {
	providers *Providers
	base      http.RoundTripper
}

func (t subscriptionTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Scheme != httpsScheme || request.URL.Host != "chatgpt.com" {
		return nil, &credentialError{
			cause: errors.New("ChatGPT subscription requests require the exact HTTPS provider host"),
		}
	}
	credentials, err := t.providers.openAICredentials(request.Context())
	if err != nil {
		return nil, &credentialError{
			cause: err,
		}
	}
	if !credentials.ExpiresAt.After(time.Now()) {
		return nil, &credentialError{
			cause: errors.New("ChatGPT access token expired before the request"),
		}
	}
	copy := request.Clone(request.Context())
	copy.Header.Set(
		"Authorization",
		"Bearer "+credentials.AccessToken,
	)
	copy.Header.Set(
		"ChatGPT-Account-Id",
		credentials.AccountID,
	)
	copy.Header.Set(
		"originator",
		"cpe",
	)
	return t.base.RoundTrip(copy)
}
