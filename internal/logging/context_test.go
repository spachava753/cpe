package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"
)

const (
	testCWD         = "/repo"
	testEnabledAttr = "enabled"
	testSessionID   = "session-1"
)

type attrFilteringHandler struct {
	enabled bool
	handled *int
}

func (h attrFilteringHandler) Enabled(context.Context, slog.Level) bool {
	return h.enabled
}

func (h attrFilteringHandler) Handle(context.Context, slog.Record) error {
	*h.handled = *h.handled + 1
	return nil
}

func (h attrFilteringHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	for _, attr := range attrs {
		if attr.Key == testEnabledAttr && attr.Value.Kind() == slog.KindBool {
			h.enabled = attr.Value.Bool()
		}
	}
	return h
}

func (h attrFilteringHandler) WithGroup(string) slog.Handler {
	return h
}

type incrementingLogValuer struct {
	calls *int
}

func (v incrementingLogValuer) LogValue() slog.Value {
	*v.calls = *v.calls + 1
	return slog.IntValue(*v.calls)
}

type mutatingAttrsHandler struct {
	slog.Handler
}

func (h mutatingAttrsHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := h.Handler.WithAttrs(attrs)
	for i := range attrs {
		attrs[i] = slog.String("mutated", "true")
	}
	return mutatingAttrsHandler{Handler: next}
}

func (h mutatingAttrsHandler) WithGroup(name string) slog.Handler {
	return mutatingAttrsHandler{Handler: h.Handler.WithGroup(name)}
}

func TestContextHandlerEvaluatesEnabledOnScopedHandler(t *testing.T) {
	t.Run("context enables record", func(t *testing.T) {
		handled := 0
		logger := slog.New(NewContextHandler(attrFilteringHandler{handled: &handled}))
		ctx := WithAttrs(t.Context(), slog.Bool(testEnabledAttr, true))

		logger.InfoContext(ctx, "scoped")
		logger.Info("unscoped")

		if handled != 1 {
			t.Fatalf("handled records = %d, want 1 scoped record", handled)
		}
	})

	t.Run("context disables record", func(t *testing.T) {
		handled := 0
		logger := slog.New(NewContextHandler(attrFilteringHandler{enabled: true, handled: &handled}))
		ctx := WithAttrs(t.Context(), slog.Bool(testEnabledAttr, false))

		logger.InfoContext(ctx, "scoped")
		logger.Info("unscoped")

		if handled != 1 {
			t.Fatalf("handled records = %d, want 1 unscoped record", handled)
		}
	})
}

func TestContextHandlerAddsScopedAttributes(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(NewContextHandler(slog.NewJSONHandler(&output, nil))).With("component", "test")
	sessionCtx := WithAttrs(
		t.Context(),
		slog.String("session_id", testSessionID),
		slog.String("cwd", testCWD),
	)

	logger.InfoContext(sessionCtx, "scoped")
	logger.InfoContext(context.Background(), "unscoped")

	decoder := json.NewDecoder(&output)
	var scoped map[string]any
	if err := decoder.Decode(&scoped); err != nil {
		t.Fatalf("decode scoped log: %v", err)
	}
	if scoped["component"] != "test" || scoped["session_id"] != testSessionID || scoped["cwd"] != testCWD {
		t.Fatalf("scoped log attributes = %#v", scoped)
	}

	var unscoped map[string]any
	if err := decoder.Decode(&unscoped); err != nil {
		t.Fatalf("decode unscoped log: %v", err)
	}
	if unscoped["component"] != "test" {
		t.Fatalf("unscoped component = %#v, want test", unscoped["component"])
	}
	if _, ok := unscoped["session_id"]; ok {
		t.Fatalf("session_id leaked into unscoped log: %#v", unscoped)
	}
	if _, ok := unscoped["cwd"]; ok {
		t.Fatalf("cwd leaked into unscoped log: %#v", unscoped)
	}
}

func TestContextHandlerKeepsScopedAttributesAtRootUnderGroups(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(NewContextHandler(slog.NewJSONHandler(&output, nil))).
		With("service", "cpe").
		WithGroup("component").
		With("name", "acp")
	ctx := WithAttrs(
		t.Context(),
		slog.String("session_id", testSessionID),
		slog.String("cwd", testCWD),
	)

	logger.InfoContext(ctx, "grouped", "event", "prompt")

	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("decode grouped log: %v", err)
	}
	if record["session_id"] != testSessionID || record["cwd"] != testCWD {
		t.Fatalf("root correlation attributes = %#v", record)
	}
	component, ok := record["component"].(map[string]any)
	if !ok {
		t.Fatalf("component group = %#v, want object", record["component"])
	}
	if component["name"] != "acp" || component["event"] != "prompt" {
		t.Fatalf("component group attributes = %#v", component)
	}
	if _, ok := component["session_id"]; ok {
		t.Fatalf("session_id nested under component: %#v", component)
	}
	if _, ok := component["cwd"]; ok {
		t.Fatalf("cwd nested under component: %#v", component)
	}
}

func TestContextHandlerDoesNotReuseDelegatedAttrSlices(t *testing.T) {
	var output bytes.Buffer
	wrapped := mutatingAttrsHandler{Handler: slog.NewJSONHandler(&output, nil)}
	logger := slog.New(NewContextHandler(wrapped)).With("component", "acp")
	ctx := WithAttrs(
		t.Context(),
		slog.String("session_id", testSessionID),
		slog.String("cwd", testCWD),
	)

	const recordCount = 8
	var writers sync.WaitGroup
	for range recordCount {
		writers.Go(func() {
			logger.InfoContext(ctx, "concurrent")
		})
	}
	writers.Wait()

	decoder := json.NewDecoder(&output)
	for i := range recordCount {
		var record map[string]any
		if err := decoder.Decode(&record); err != nil {
			t.Fatalf("decode log %d: %v", i, err)
		}
		if record["component"] != "acp" || record["session_id"] != testSessionID || record["cwd"] != testCWD {
			t.Fatalf("log %d attributes = %#v", i, record)
		}
		if _, ok := record["mutated"]; ok {
			t.Fatalf("wrapped handler mutation leaked into log %d: %#v", i, record)
		}
	}
}

func TestContextHandlerResolvesLoggerAttrsOnce(t *testing.T) {
	var output bytes.Buffer
	calls := 0
	logger := slog.New(NewContextHandler(slog.NewJSONHandler(&output, nil))).With(
		"sequence",
		incrementingLogValuer{calls: &calls},
	)
	ctx := WithAttrs(t.Context(), slog.String("session_id", testSessionID))

	logger.Info("unscoped")
	logger.InfoContext(ctx, "scoped first")
	logger.InfoContext(ctx, "scoped second")

	decoder := json.NewDecoder(&output)
	for i := range 3 {
		var record map[string]any
		if err := decoder.Decode(&record); err != nil {
			t.Fatalf("decode log %d: %v", i, err)
		}
		if record["sequence"] != float64(1) {
			t.Fatalf("log %d sequence = %#v, want 1", i, record["sequence"])
		}
	}
	if calls != 1 {
		t.Fatalf("LogValue() calls = %d, want 1", calls)
	}
}

func TestWithAttrsPreservesAndReplacesParentAttributes(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(NewContextHandler(slog.NewJSONHandler(&output, nil)))
	ctx := WithAttrs(t.Context(), slog.String("session_id", testSessionID))
	ctx = WithAttrs(ctx, slog.String("session_id", "session-2"), slog.String("cwd", testCWD))

	logger.InfoContext(ctx, "nested")

	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("decode log: %v", err)
	}
	if record["session_id"] != "session-2" || record["cwd"] != testCWD {
		t.Fatalf("nested context attributes = %#v", record)
	}
}
