package agent

import (
	"context"
	"errors"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/gai"
	"github.com/spachava753/gai/agent/agenttest"

	"github.com/spachava753/cpe/internal/conversation"
	"github.com/spachava753/cpe/internal/models"
	"github.com/spachava753/cpe/internal/toolruntime"
)

func TestProviderMediaErrorFailsTheTurn(t *testing.T) {
	for _, test := range []struct {
		err  *gai.ApiErr
		name string
	}{
		{
			name: "provider rejects the media",
			err: &gai.ApiErr{
				Provider:   gai.ProviderAnthropic,
				Kind:       gai.APIErrorKindInvalidRequest,
				StatusCode: 400,
				Message:    "messages.2.content.0.tool_result.content.1.image.source.base64.media_type: Input should be 'image/jpeg', 'image/png', 'image/gif' or 'image/webp'",
			},
		},
		{
			name: "invalid request unrelated to the media",
			err: &gai.ApiErr{
				Provider:   gai.ProviderAnthropic,
				Kind:       gai.APIErrorKindInvalidRequest,
				StatusCode: 400,
				Message:    "messages.1.content.0: Invalid `signature` in `thinking` block",
			},
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				generator := agenttest.NewScriptedGenerator(
					agenttest.GenerateStep{
						Response: compactionResponse(compactionCall(
							t,
							"view",
							toolruntime.StarlarkToolName,
							map[string]any{
								"code": `print('screenshot attached')
view(b"\x89PNG\r\n\x1a\nfixture", 'image/png')`,
							},
						)),
					},
					agenttest.GenerateStep{
						Err: test.err,
					},
					agenttest.GenerateStep{
						Err: errors.New("the request was retried"),
					},
				)
				fixture := serverFixture(
					t,
					func(context.Context, conversation.ID, models.Model, models.ReasoningLevel) (gai.Generator, gai.GenerationOptions, error) {
						return generator, gai.GenerationOptions{}, nil
					},
				)
				id := openFixtureSession(
					t,
					fixture.client,
				)
				_, err := fixture.client.Prompt(
					t.Context(),
					&acp.PromptRequest{
						SessionID: id,
						Prompt: []acp.ContentBlock{
							acp.TextContentBlock("take a screenshot"),
						},
					},
				)
				if err == nil || !strings.Contains(
					err.Error(),
					test.err.Message,
				) {
					t.Fatalf(
						"prompt error = %v",
						err,
					)
				}
				if requests := len(generator.Requests()); requests != 2 {
					t.Fatalf(
						"provider requests = %d, want 2",
						requests,
					)
				}
				record, err := fixture.store.Load(
					t.Context(),
					conversation.ID(id),
				)
				if err != nil {
					t.Fatal(err)
				}
				if blocks := record.Dialog[2].Blocks; blocks[len(blocks)-1].ModalityType != gai.Image {
					t.Fatalf(
						"saved history lost the media: %#v",
						blocks,
					)
				}
			},
		)
	}
}

func TestUnsupportedToolMediaIsSavedForTheModel(t *testing.T) {
	var notice string
	generator := agenttest.NewScriptedGenerator(
		agenttest.GenerateStep{
			Response: compactionResponse(compactionCall(
				t,
				"view",
				toolruntime.StarlarkToolName,
				map[string]any{
					"code": `view(b"\x89PNG\r\n\x1a\nfixture", "image/png")`,
				},
			)),
		},
		agenttest.GenerateStep{
			Check: func(request gai.GenerationRequest) error {
				last := request.Dialog[len(request.Dialog)-1]
				for _, block := range last.Blocks {
					if block.ModalityType != gai.Text {
						return errors.New("a text-only model received media")
					}
				}
				notice = last.Blocks[len(last.Blocks)-1].Content.String()
				return nil
			},
			Response: compactionResponse(gai.TextBlock("noted")),
		},
	)
	fixture := serverFixture(
		t,
		func(context.Context, conversation.ID, models.Model, models.ReasoningLevel) (gai.Generator, gai.GenerationOptions, error) {
			return generator, gai.GenerationOptions{}, nil
		},
	)
	id := openFixtureSession(
		t,
		fixture.client,
	)
	if _, err := fixture.client.SetSessionConfigOption(
		t.Context(),
		&acp.SetSessionConfigOptionRequest{
			SessionID: id,
			ConfigID:  "model",
			Value:     "two",
		},
	); err != nil {
		t.Fatal(err)
	}
	promptFixture(
		t,
		fixture.client,
		id,
		"look",
	)
	match := regexp.MustCompile(`saved to (\S+);`).FindStringSubmatch(notice)
	if match == nil || !strings.Contains(
		notice,
		"does not accept image input",
	) {
		t.Fatalf(
			"notice = %q",
			notice,
		)
	}
	if saved, err := os.ReadFile(match[1]); err != nil || string(saved) != "\x89PNG\r\n\x1a\nfixture" {
		t.Fatalf(
			"saved media = %q, %v",
			saved,
			err,
		)
	}
	record, err := fixture.store.Load(
		t.Context(),
		conversation.ID(id),
	)
	if err != nil {
		t.Fatal(err)
	}
	if blocks := record.Dialog[2].Blocks; blocks[len(blocks)-1].ModalityType != gai.Image {
		t.Fatal("history dropped media that another model could still use")
	}
}
