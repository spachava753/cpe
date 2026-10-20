package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/gai"
	gaiagent "github.com/spachava753/gai/agent"
	"github.com/spachava753/gai/agent/agenttest"

	"github.com/spachava753/cpe/internal/conversation"
	"github.com/spachava753/cpe/internal/models"
)

func TestStreamingTurn(t *testing.T) {
	fixture := serverFixture(
		t,
		func(context.Context, models.Model, models.ReasoningLevel) (gai.Generator, gai.GenerationOptions, error) {
			return agenttest.NewScriptedStreamingGenerator(agenttest.StreamStep{
				Chunks: []gai.StreamChunk{
					{
						Block: gai.TextBlock("hel"),
					},
					{
						Block: gai.TextBlock("lo"),
					},
				},
			}), gai.GenerationOptions{}, nil
		},
	)
	id := openFixtureSession(
		t,
		fixture.client,
	)
	promptFixture(
		t,
		fixture.client,
		id,
		"hello",
	)
	// Closing the SDK client drains already-received notification handlers.
	if err := fixture.client.Close(); err != nil {
		t.Fatal(err)
	}
	fixture.notifications.mu.Lock()
	defer fixture.notifications.mu.Unlock()
	var output strings.Builder
	chunks := 0
	for _, notification := range fixture.notifications.updates {
		if notification.Update.SessionUpdate == acp.SessionUpdateTypeAgentMessageChunk {
			content, ok := notification.Update.Content.(acp.ContentBlock)
			if !ok {
				t.Fatalf(
					"unexpected content type %T",
					notification.Update.Content,
				)
			}
			output.WriteString(content.Text)
			chunks++
		}
	}
	if chunks != 2 || output.String() != "hello" {
		t.Fatalf(
			"stream output: %d %q",
			chunks,
			output.String(),
		)
	}
	record, err := fixture.store.Load(
		t.Context(),
		conversation.ID(id),
	)
	if err != nil {
		t.Fatal(err)
	}
	if record.Dialog[len(record.Dialog)-1].Blocks[0].Content.String() != "hello" {
		t.Fatal("streamed response not checkpointed")
	}
}

func TestProviderHistory(t *testing.T) {
	for _, test := range []struct {
		name         string
		selected     models.ID
		wantMessages int
	}{
		{
			name:         "same model retains reasoning",
			selected:     "one",
			wantMessages: 3,
		},
		{
			name:         "switch drops empty foreign assistant",
			selected:     "two",
			wantMessages: 2,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				dialog := gai.Dialog{
					{
						Role: gai.User,
						Blocks: []gai.Block{
							gai.ImageBlock(
								[]byte("image"),
								"image/png",
							),
						},
					},
					{
						Role: gai.Assistant,
						Blocks: []gai.Block{
							{
								BlockType:    gai.Thinking,
								ModalityType: gai.Text,
								MimeType:     "text/plain",
								Content:      gai.Str("reasoning only"),
							},
						},
						ExtraFields: map[string]any{
							"cpe.model":       "one",
							"provider.replay": "retained",
						},
					},
					{
						Role: gai.User,
						Blocks: []gai.Block{
							gai.TextBlock("continue"),
						},
					},
				}
				hooks := &turnHooks{
					modelID: test.selected,
					model: models.Model{
						InputModalities: []gai.Modality{
							gai.Text,
						},
					},
					maxGenerations: 5,
				}
				decision, err := hooks.BeforeGeneration(
					t.Context(),
					gaiagent.BeforeGenerationRequest{
						Request: gai.GenerationRequest{
							Dialog: dialog,
						},
					},
				)
				if err != nil {
					t.Fatal(err)
				}
				if len(decision.Request.Dialog) != test.wantMessages {
					t.Fatalf(
						"adapted messages: %d",
						len(decision.Request.Dialog),
					)
				}
				for _, message := range decision.Request.Dialog {
					if len(message.Blocks) == 0 {
						t.Fatal("empty provider message")
					}
				}
				if decision.Request.Dialog[0].Blocks[0].ModalityType != gai.Text {
					t.Fatal("unsupported media sent to selected model")
				}
				if dialog[0].Blocks[0].ModalityType != gai.Image || len(dialog[1].Blocks) != 1 || dialog[1].ExtraFields["provider.replay"] != "retained" {
					t.Fatal("canonical history mutated")
				}
			},
		)
	}
}
