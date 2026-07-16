package logging

import (
	"context"
	"log/slog"
	"os"
	"slices"
)

type contextAttrsKey struct{}

// WithAttrs returns a child context carrying attrs for a ContextHandler.
// Values are resolved when attached. Existing attributes are preserved; a new
// attribute replaces an existing one with the same key.
func WithAttrs(ctx context.Context, attrs ...slog.Attr) context.Context {
	if len(attrs) == 0 {
		return ctx
	}
	attrs = resolveAttrs(attrs)
	existing, _ := ctx.Value(contextAttrsKey{}).([]slog.Attr)
	combined := slices.Clone(existing)
	for _, attr := range attrs {
		idx := slices.IndexFunc(combined, func(existing slog.Attr) bool {
			return existing.Key == attr.Key
		})
		if idx == -1 {
			combined = append(combined, attr)
			continue
		}
		combined[idx] = attr
	}
	return context.WithValue(ctx, contextAttrsKey{}, combined)
}

// NewProcessHandler adds the process ID and decorates next with context-carried
// attributes.
func NewProcessHandler(next slog.Handler) slog.Handler {
	return NewContextHandler(next).WithAttrs([]slog.Attr{slog.Int("pid", os.Getpid())})
}

// NewContextHandler decorates next so records logged with a context include
// attributes previously attached by WithAttrs. Context attributes remain at the
// root even when the logger uses WithGroup.
func NewContextHandler(next slog.Handler) slog.Handler {
	return contextHandler{
		base:    next,
		current: next,
	}
}

type handlerOperation struct {
	attrs   []slog.Attr
	group   string
	isGroup bool
}

type contextHandler struct {
	base       slog.Handler
	current    slog.Handler
	operations []handlerOperation
}

func (h contextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handlerFor(ctx).Enabled(ctx, level)
}

func (h contextHandler) Handle(ctx context.Context, record slog.Record) error {
	return h.handlerFor(ctx).Handle(ctx, record)
}

func (h contextHandler) handlerFor(ctx context.Context) slog.Handler {
	attrs, _ := ctx.Value(contextAttrsKey{}).([]slog.Attr)
	if len(attrs) == 0 {
		return h.current
	}

	// Bind correlation fields before replaying logger groups so they stay at root.
	handler := h.base.WithAttrs(cloneAttrs(attrs))
	for _, operation := range h.operations {
		if operation.isGroup {
			handler = handler.WithGroup(operation.group)
			continue
		}
		handler = handler.WithAttrs(cloneAttrs(operation.attrs))
	}
	return handler
}

func (h contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	attrs = resolveAttrs(attrs)
	operationAttrs := cloneAttrs(attrs)
	return contextHandler{
		base:       h.base,
		current:    h.current.WithAttrs(cloneAttrs(attrs)),
		operations: append(slices.Clone(h.operations), handlerOperation{attrs: operationAttrs}),
	}
}

func (h contextHandler) WithGroup(name string) slog.Handler {
	return contextHandler{
		base:       h.base,
		current:    h.current.WithGroup(name),
		operations: append(slices.Clone(h.operations), handlerOperation{group: name, isGroup: true}),
	}
}

func resolveAttrs(attrs []slog.Attr) []slog.Attr {
	resolved := make([]slog.Attr, len(attrs))
	for i, attr := range attrs {
		attr.Value = attr.Value.Resolve()
		if attr.Value.Kind() == slog.KindGroup {
			attr.Value = slog.GroupValue(resolveAttrs(attr.Value.Group())...)
		}
		resolved[i] = attr
	}
	return resolved
}

func cloneAttrs(attrs []slog.Attr) []slog.Attr {
	cloned := make([]slog.Attr, len(attrs))
	for i, attr := range attrs {
		if attr.Value.Kind() == slog.KindGroup {
			attr.Value = slog.GroupValue(cloneAttrs(attr.Value.Group())...)
		}
		cloned[i] = attr
	}
	return cloned
}
