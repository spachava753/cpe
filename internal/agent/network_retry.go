package agent

import (
	"context"
	"errors"
	"io"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/spachava753/gai"
	"golang.org/x/net/http2"
)

const (
	defaultNetworkMaxRetries = 3
	defaultNetworkRetryDelay = 5 * time.Second
)

type networkRetryGenerator struct {
	gai.GeneratorWrapper
	maxRetries int
	retryDelay time.Duration
}

func newNetworkRetryGenerator(inner gai.Generator) *networkRetryGenerator {
	return &networkRetryGenerator{
		GeneratorWrapper: gai.GeneratorWrapper{Inner: inner},
		maxRetries:       defaultNetworkMaxRetries,
		retryDelay:       defaultNetworkRetryDelay,
	}
}

func withNetworkRetry() gai.WrapperFunc {
	return func(inner gai.Generator) gai.Generator {
		return newNetworkRetryGenerator(inner)
	}
}

func (g *networkRetryGenerator) Generate(ctx context.Context, dialog gai.Dialog, opts *gai.GenOpts) (gai.Response, error) {
	maxRetries := max(g.maxRetries, 0)

	var resp gai.Response
	for retry := 0; ; retry++ {
		if cause := context.Cause(ctx); cause != nil {
			return resp, cause
		}

		var err error
		resp, err = g.GeneratorWrapper.Generate(ctx, dialog, opts)
		if err == nil {
			return resp, nil
		}
		if cause := context.Cause(ctx); cause != nil {
			return resp, cause
		}
		if !isRetryableNetworkError(err) || retry >= maxRetries {
			return resp, err
		}
		if g.retryDelay <= 0 {
			continue
		}

		select {
		case <-time.After(g.retryDelay):
		case <-ctx.Done():
			return resp, context.Cause(ctx)
		}
	}
}

func isRetryableNetworkError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) {
		return true
	}

	// A URL error also represents deterministic request mistakes, so inspect its
	// cause rather than treating every URL error as a retryable network failure.
	if urlErr, ok := errors.AsType[*url.Error](err); ok {
		return isRetryableNetworkError(urlErr.Err)
	}
	if _, ok := errors.AsType[net.Error](err); ok {
		return true
	}
	if _, ok := errors.AsType[http2.StreamError](err); ok {
		return true
	}

	// These net/http HTTP/2 disconnect errors are unexported and implement only error.
	message := err.Error()
	return strings.Contains(message, "http2: client connection lost") ||
		strings.Contains(message, "http2: server sent GOAWAY and closed the connection")
}

var _ gai.ToolCallingGenerator = (*networkRetryGenerator)(nil)
