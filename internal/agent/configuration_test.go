package agent

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/conversation"
	"github.com/spachava753/cpe/internal/models"
	"github.com/spachava753/cpe/internal/toolruntime"
)

// gatedStore blocks the first save accepted by hold until release is closed.
type gatedStore struct {
	conversation.Store
	hold    func(conversation.Record) bool
	held    chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *gatedStore) Save(ctx context.Context, record conversation.Record) error {
	gate := false
	if s.hold(record) {
		s.once.Do(func() {
			gate = true
		})
	}
	if gate {
		close(s.held)
		<-s.release
	}
	return s.Store.Save(
		ctx,
		record,
	)
}

func TestConfigurationDuringTurn(t *testing.T) {
	entered := make(chan struct{})
	proceed := make(chan struct{})
	var (
		mu         sync.Mutex
		selections []models.Selection
	)
	generator := func(_ context.Context, _ conversation.ID, model models.Model, effort models.ReasoningLevel) (gai.Generator, gai.GenerationOptions, error) {
		mu.Lock()
		selections = append(
			selections,
			models.Selection{
				Model:     models.ID(model.ID),
				Reasoning: effort,
			},
		)
		mu.Unlock()
		return generatorFunc(func(ctx context.Context, request gai.GenerationRequest) (gai.Response, error) {
			last := request.Dialog[len(request.Dialog)-1]
			if last.Role != gai.ToolResult && last.Blocks[0].Content.String() == "wait" {
				close(entered)
				select {
				case <-proceed:
				case <-ctx.Done():
					return gai.Response{}, ctx.Err()
				}
				block, err := gai.ToolCallBlock(
					"call",
					toolruntime.StarlarkToolName,
					map[string]any{
						"code": "print(1)",
					},
				)
				if err != nil {
					return gai.Response{}, err
				}
				return gai.Response{
					Candidates: []gai.Message{
						{
							Role: gai.Assistant,
							Blocks: []gai.Block{
								block,
							},
						},
					},
					FinishReason: gai.ToolUse,
				}, nil
			}
			return gai.Response{
				Candidates: []gai.Message{
					{
						Role: gai.Assistant,
						Blocks: []gai.Block{
							gai.TextBlock("done"),
						},
					},
				},
				FinishReason: gai.EndTurn,
			}, nil
		}), gai.GenerationOptions{}, nil
	}
	store := &gatedStore{
		// Hold the turn's checkpoint of the tool call, made before it runs.
		hold: func(record conversation.Record) bool {
			return slices.ContainsFunc(
				record.Dialog,
				func(message gai.Message) bool {
					return message.Role == gai.Assistant && message.Blocks[0].BlockType == gai.ToolCall
				},
			)
		},
		held:    make(chan struct{}),
		release: make(chan struct{}),
	}
	fixture := serverFixture(
		t,
		generator,
		func(_ *Config, deps *Dependencies, _ *acp.InitializeRequest) {
			store.Store = deps.History
			deps.History = store
		},
	)
	id := openFixtureSession(
		t,
		fixture.client,
	)
	finished := make(
		chan error,
		1,
	)
	go func() {
		_, err := fixture.client.Prompt(
			t.Context(),
			&acp.PromptRequest{
				SessionID: id,
				Prompt: []acp.ContentBlock{
					acp.TextContentBlock("wait"),
				},
			},
		)
		finished <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("generation did not start")
	}
	_, err := fixture.client.SetSessionConfigOption(
		t.Context(),
		&acp.SetSessionConfigOptionRequest{
			SessionID: id,
			ConfigID:  "model",
			Value:     "two",
		},
	)
	if err != nil {
		t.Fatalf(
			"model change during turn: %v",
			err,
		)
	}
	close(proceed)
	select {
	case <-store.held:
	case <-time.After(time.Second):
		t.Fatal("tool call checkpoint did not start")
	}
	// A change made while a checkpoint is being saved must survive it.
	configured := make(
		chan error,
		1,
	)
	go func() {
		_, configureErr := fixture.client.SetSessionConfigOption(
			t.Context(),
			&acp.SetSessionConfigOptionRequest{
				SessionID: id,
				ConfigID:  "reasoning",
				Value:     "low",
			},
		)
		configured <- configureErr
	}()
	select {
	case err = <-configured:
		configured <- err
	case <-time.After(100 * time.Millisecond):
	}
	close(store.release)
	if err = <-configured; err != nil {
		t.Fatalf(
			"reasoning change during turn: %v",
			err,
		)
	}
	select {
	case err = <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("prompt did not finish")
	}
	want := models.Selection{
		Model:     "two",
		Reasoning: "low",
	}
	record, err := fixture.store.Load(
		t.Context(),
		conversation.ID(id),
	)
	if err != nil {
		t.Fatal(err)
	}
	if record.Options != want {
		t.Fatalf(
			"saved options: got %#v, want %#v",
			record.Options,
			want,
		)
	}
	fixture.notifications.mu.Lock()
	var announced []acp.SessionConfigOption
	for _, notification := range fixture.notifications.updates {
		if notification.Update.SessionUpdate == acp.SessionUpdateTypeConfigOptionUpdate {
			announced = notification.Update.ConfigOptions
		}
	}
	fixture.notifications.mu.Unlock()
	if got := currentValues(announced); !maps.Equal(
		got,
		map[string]string{
			"model":     "two",
			"reasoning": "low",
		},
	) {
		t.Fatalf(
			"last config_option_update: %v",
			got,
		)
	}
	promptFixture(
		t,
		fixture.client,
		id,
		"hello",
	)
	mu.Lock()
	defer mu.Unlock()
	// The running turn keeps its selection; the next turn uses the change.
	wantSelections := []models.Selection{
		{
			Model: "one",
		},
		want,
	}
	if !slices.Equal(
		selections,
		wantSelections,
	) {
		t.Fatalf(
			"generator selections: got %#v, want %#v",
			selections,
			wantSelections,
		)
	}
}

func currentValues(options []acp.SessionConfigOption) map[string]string {
	values := make(map[string]string)
	for _, option := range options {
		values[string(option.ID)] = fmt.Sprint(option.CurrentValue)
	}
	return values
}

func TestConfigurationSaveBeforeDelete(t *testing.T) {
	store := &gatedStore{
		hold: func(record conversation.Record) bool {
			return record.Options.Model == "two"
		},
		held:    make(chan struct{}),
		release: make(chan struct{}),
	}
	fixture := serverFixture(
		t,
		scriptGenerator,
		func(_ *Config, deps *Dependencies, _ *acp.InitializeRequest) {
			store.Store = deps.History
			deps.History = store
		},
	)
	id := openFixtureSession(
		t,
		fixture.client,
	)
	configured := make(
		chan error,
		1,
	)
	go func() {
		_, err := fixture.client.SetSessionConfigOption(
			t.Context(),
			&acp.SetSessionConfigOptionRequest{
				SessionID: id,
				ConfigID:  "model",
				Value:     "two",
			},
		)
		configured <- err
	}()
	select {
	case <-store.held:
	case <-time.After(time.Second):
		t.Fatal("configuration save did not start")
	}
	deleted := make(
		chan error,
		1,
	)
	go func() {
		_, err := fixture.client.DeleteSession(
			t.Context(),
			&acp.DeleteSessionRequest{
				SessionID: id,
			},
		)
		deleted <- err
	}()
	// Deletion must wait for the accepted save rather than run before it.
	select {
	case err := <-deleted:
		deleted <- err
	case <-time.After(100 * time.Millisecond):
	}
	close(store.release)
	if err := <-configured; err != nil {
		t.Fatal(err)
	}
	if err := <-deleted; err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.Load(
		t.Context(),
		conversation.ID(id),
	); !errors.Is(
		err,
		fs.ErrNotExist,
	) {
		t.Fatalf(
			"deleted session load: %v",
			err,
		)
	}
}
