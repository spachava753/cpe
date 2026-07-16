// Package logging adds request-scoped structured attributes to slog handlers.
//
// WithAttrs stores immutable, once-resolved attributes on a context.
// NewProcessHandler adds the process ID, and both handler constructors emit
// context attributes at the root of records logged with slog's context-aware
// methods, including when a logger uses WithGroup. This allows request metadata
// to cross package boundaries without global mutable state.
package logging
