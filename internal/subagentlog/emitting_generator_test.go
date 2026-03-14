package subagentlog

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/spachava753/gai"
)

func TestFindToolNameByCallIDReturnsUnknownForEmptyDecodedName(t *testing.T) {
	t.Parallel()

	assistantMsg := gai.Message{
		Role: gai.Assistant,
		Blocks: []gai.Block{{
			ID:           "call_1",
			BlockType:    gai.ToolCall,
			ModalityType: gai.Text,
			MimeType:     "application/json",
			Content:      gai.Str(`{"parameters":{"value":1}}`),
		}},
	}

	got := findToolNameByCallID(assistantMsg, "call_1")
	want := "unknown"
	if got != want {
		t.Fatalf("findToolNameByCallID() = %q, want %q", got, want)
	}
}

func TestEmittingGeneratorFallbackUsesNearestPrecedingAssistantForToolResults(t *testing.T) {
	t.Parallel()

	collector := newEventCollectorServer(t)
	gen := NewEmittingGenerator(mockDialogGenerator{dialog: gai.Dialog{
		{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("run two turns")}},
		{Role: gai.Assistant, Blocks: []gai.Block{mustToolCallBlock(t, "call_1", "first_tool")}},
		gai.ToolResultMessage("call_1", gai.TextBlock("first result")),
		{Role: gai.Assistant, Blocks: []gai.Block{mustToolCallBlock(t, "call_1", "second_tool")}},
		gai.ToolResultMessage("call_1", gai.TextBlock("second result")),
	}}, NewClient(collector.server.URL), "reviewer", "run-1")

	result, err := gen.Generate(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if len(result) != 5 {
		t.Fatalf("Generate() dialog length = %d, want 5", len(result))
	}

	toolResultEvents := collector.eventsByType(EventTypeToolResult)
	if len(toolResultEvents) != 2 {
		t.Fatalf("tool result event count = %d, want 2", len(toolResultEvents))
	}

	if got, want := toolResultEvents[0].ToolName, "first_tool"; got != want {
		t.Fatalf("first tool result event tool name = %q, want %q", got, want)
	}
	if got, want := toolResultEvents[1].ToolName, "second_tool"; got != want {
		t.Fatalf("second tool result event tool name = %q, want %q", got, want)
	}
}

func TestEmittingGeneratorFallbackIncludesAllToolResultBlocksInPayload(t *testing.T) {
	t.Parallel()

	collector := newEventCollectorServer(t)
	gen := NewEmittingGenerator(mockDialogGenerator{dialog: gai.Dialog{
		{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("run tool")}},
		{Role: gai.Assistant, Blocks: []gai.Block{mustToolCallBlock(t, "call_1", "report_tool")}},
		gai.ToolResultMessage("call_1", gai.TextBlock("summary"), gai.ImageBlock([]byte{0x01}, "image/png")),
	}}, NewClient(collector.server.URL), "reviewer", "run-1")

	_, err := gen.Generate(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	toolResultEvents := collector.eventsByType(EventTypeToolResult)
	if len(toolResultEvents) != 1 {
		t.Fatalf("tool result event count = %d, want 1", len(toolResultEvents))
	}
	if got, want := toolResultEvents[0].Payload, "summary\n\n[image/png content]"; got != want {
		t.Fatalf("tool result event payload = %q, want %q", got, want)
	}
}

func TestNewEmittingGeneratorDoesNotStackMiddlewareWhenReused(t *testing.T) {
	t.Parallel()

	toolGen := &gai.ToolGenerator{G: staticToolCapableGenerator{}}
	firstCollector := newEventCollectorServer(t)
	secondCollector := newEventCollectorServer(t)

	NewEmittingGenerator(toolGen, NewClient(firstCollector.server.URL), "first", "run-1")
	wrapped, ok := toolGen.G.(*emittingMiddleware)
	if !ok {
		t.Fatalf("toolGen.G type = %T, want *emittingMiddleware", toolGen.G)
	}
	if _, ok := wrapped.base.(staticToolCapableGenerator); !ok {
		t.Fatalf("wrapped.base type = %T, want staticToolCapableGenerator", wrapped.base)
	}

	NewEmittingGenerator(toolGen, NewClient(secondCollector.server.URL), "second", "run-2")
	wrapped, ok = toolGen.G.(*emittingMiddleware)
	if !ok {
		t.Fatalf("toolGen.G type after second wrap = %T, want *emittingMiddleware", toolGen.G)
	}
	if _, ok := wrapped.base.(staticToolCapableGenerator); !ok {
		t.Fatalf("wrapped.base type after second wrap = %T, want staticToolCapableGenerator", wrapped.base)
	}
	if got, want := wrapped.subagentName, "second"; got != want {
		t.Fatalf("wrapped.subagentName = %q, want %q", got, want)
	}
	if got, want := wrapped.runID, "run-2"; got != want {
		t.Fatalf("wrapped.runID = %q, want %q", got, want)
	}
}

type mockDialogGenerator struct {
	dialog gai.Dialog
}

func (m mockDialogGenerator) Generate(context.Context, gai.Dialog, gai.GenOptsGenerator) (gai.Dialog, error) {
	return m.dialog, nil
}

type staticToolCapableGenerator struct{}

func (staticToolCapableGenerator) Generate(context.Context, gai.Dialog, *gai.GenOpts) (gai.Response, error) {
	return gai.Response{}, nil
}

func (staticToolCapableGenerator) Register(gai.Tool) error {
	return nil
}

type eventCollectorServer struct {
	server *httptest.Server

	mu     sync.Mutex
	events []Event
}

func newEventCollectorServer(t *testing.T) *eventCollectorServer {
	t.Helper()

	collector := &eventCollectorServer{}
	collector.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/subagent-events" {
			http.NotFound(w, r)
			return
		}
		defer r.Body.Close()

		var event Event
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			t.Errorf("decode event: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		collector.mu.Lock()
		collector.events = append(collector.events, event)
		collector.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(collector.server.Close)
	return collector
}

func (c *eventCollectorServer) eventsByType(eventType string) []Event {
	c.mu.Lock()
	defer c.mu.Unlock()

	var filtered []Event
	for _, event := range c.events {
		if event.Type == eventType {
			filtered = append(filtered, event)
		}
	}
	return filtered
}

func mustToolCallBlock(t *testing.T, id, name string) gai.Block {
	t.Helper()

	block, err := gai.ToolCallBlock(id, name, map[string]any{"value": 1})
	if err != nil {
		t.Fatalf("ToolCallBlock() error = %v", err)
	}
	return block
}
