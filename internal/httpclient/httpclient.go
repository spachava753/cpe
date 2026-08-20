package httpclient

import (
	"context"
	"crypto/x509"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/failsafe-go/failsafe-go"
	"github.com/failsafe-go/failsafe-go/failsafehttp"
	"github.com/failsafe-go/failsafe-go/retrypolicy"
)

type options struct {
	baseClient     *http.Client
	baseTransport  http.RoundTripper
	timeout        *time.Duration
	defaultTimeout *time.Duration
	maxRetries     int
	backoffDelay   time.Duration
	backoffMax     time.Duration
	jitterFactor   float64
	retryStatuses  bool
}

// option configures a reliable HTTP client or transport.
type option func(*options)

// withBaseClient clones client settings before installing a failsafe transport.
// If client.Transport is nil, http.DefaultTransport is used.
func withBaseClient(client *http.Client) option {
	return func(o *options) {
		o.baseClient = client
	}
}

// WithBaseTransport sets the transport wrapped by failsafe. It takes precedence
// over a transport from withBaseClient. If transport is nil, http.DefaultTransport
// is used.
func WithBaseTransport(transport http.RoundTripper) option {
	return func(o *options) {
		o.baseTransport = transport
	}
}

// WithTimeout sets http.Client.Timeout on the returned client.
func WithTimeout(timeout time.Duration) option {
	return func(o *options) {
		o.timeout = &timeout
	}
}

// withDefaultTimeout sets http.Client.Timeout only when the base client does not
// already define one.
func withDefaultTimeout(timeout time.Duration) option {
	return func(o *options) {
		o.defaultTimeout = &timeout
	}
}

// WithMaxRetries configures the maximum number of retry attempts after the
// initial request attempt. Values below zero are treated as zero.
func WithMaxRetries(maxRetries int) option {
	return func(o *options) {
		o.maxRetries = max(maxRetries, 0)
	}
}

// WithBackoff configures exponential retry backoff.
func WithBackoff(delay, maxDelay time.Duration) option {
	return func(o *options) {
		o.backoffDelay = delay
		o.backoffMax = maxDelay
	}
}

// WithJitterFactor configures proportional retry jitter.
func WithJitterFactor(jitterFactor float64) option {
	return func(o *options) {
		o.jitterFactor = jitterFactor
	}
}

// WithRetryStatuses configures whether retryable HTTP responses such as 429 and
// most 5xx statuses are retried. When false, only retryable transport errors are
// retried and any HTTP response is returned directly to the caller.
func WithRetryStatuses(retryStatuses bool) option {
	return func(o *options) {
		o.retryStatuses = retryStatuses
	}
}

// New returns an HTTP client whose transport is wrapped with failsafe retry
// behavior. If withBaseClient is provided, the returned client is a shallow copy
// that preserves fields such as CheckRedirect, Jar, and Timeout unless timeout
// options override them.
func New(opts ...option) *http.Client {
	cfg := applyOptions(opts...)
	client := http.Client{}
	if cfg.baseClient != nil {
		client = *cfg.baseClient
	}
	client.Transport = Transport(opts...)
	if cfg.timeout != nil {
		client.Timeout = *cfg.timeout
	} else if cfg.defaultTimeout != nil && client.Timeout == 0 {
		client.Timeout = *cfg.defaultTimeout
	}
	return &client
}

// Transport returns a failsafe-backed RoundTripper using the configured retry
// policy. The returned transport closes discarded retry response bodies before
// scheduling another attempt.
//
//nolint:bodyclose // retry status responses are closed by closeRetryResponseBody.
func Transport(opts ...option) http.RoundTripper {
	cfg := applyOptions(opts...)
	return failsafehttp.NewRoundTripper(baseTransport(cfg), retryPolicy(cfg))
}

//nolint:bodyclose // retry status responses are closed by closeRetryResponseBody.
func retryPolicy(cfg options) retrypolicy.RetryPolicy[*http.Response] {
	builder := retryPolicyBuilder(cfg).
		WithBackoff(cfg.backoffDelay, cfg.backoffMax).
		WithJitterFactor(cfg.jitterFactor).
		WithMaxRetries(cfg.maxRetries).
		OnRetryScheduled(closeRetryResponseBody).
		ReturnLastFailure()
	return builder.Build()
}

//nolint:bodyclose // retry status responses are closed by closeRetryResponseBody.
func retryPolicyBuilder(cfg options) retrypolicy.Builder[*http.Response] {
	if cfg.retryStatuses {
		return failsafehttp.NewRetryPolicyBuilder()
	}
	return retrypolicy.NewBuilder[*http.Response]().HandleIf(func(_ *http.Response, err error) bool {
		return shouldRetryTransportError(err)
	}).AbortOnErrors(context.Canceled)
}

func applyOptions(opts ...option) options {
	cfg := options{
		maxRetries:    2,
		backoffDelay:  200 * time.Millisecond,
		backoffMax:    3 * time.Second,
		jitterFactor:  0.2,
		retryStatuses: true,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

func baseTransport(cfg options) http.RoundTripper {
	if cfg.baseTransport != nil {
		return cfg.baseTransport
	}
	if cfg.baseClient != nil && cfg.baseClient.Transport != nil {
		return cfg.baseClient.Transport
	}
	return http.DefaultTransport
}

func closeRetryResponseBody(event failsafe.ExecutionScheduledEvent[*http.Response]) {
	if resp := event.LastResult(); resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
}

func shouldRetryTransportError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	message := err.Error()
	if strings.Contains(message, "unsupported protocol scheme") ||
		strings.Contains(message, "certificate is not trusted") ||
		(strings.Contains(message, "stopped after") && strings.Contains(message, "redirects")) {
		return false
	}
	var unknownAuthority x509.UnknownAuthorityError
	return !errors.As(err, &unknownAuthority)
}
