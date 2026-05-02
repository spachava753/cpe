// Package httpclient constructs HTTP clients with consistent retry hygiene.
//
// Callers choose policy values such as retry counts, backoff, jitter, timeout,
// status retry behavior, and the base client or transport. The package owns the
// failsafehttp wiring and closes intermediate retry response bodies so retried
// status responses do not leak connections.
package httpclient
