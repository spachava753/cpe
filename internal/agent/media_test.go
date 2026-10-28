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

func TestRejectedMediaIsSavedAndReplaced(t *testing.T) {
	rejection := &gai.ApiErr{
		Provider:   gai.ProviderAnthropic,
		Kind:       gai.APIErrorKindInvalidRequest,
		StatusCode: 400,
		Message:    "image exceeds 5 MB maximum",
	}
	const png = "\x89PNG\r\n\x1a\nfixture"
	for _, test := range []struct {
		retryErr error
		name     string
	}{
		{
			name: "retry without media succeeds",
		},
		{
			name: "retry fails for another reason",
			retryErr: &gai.ApiErr{
				Provider:   gai.ProviderAnthropic,
				Kind:       gai.APIErrorKindInvalidRequest,
				StatusCode: 400,
				Message:    "unrelated problem",
			},
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				var notice string
				view := compactionCall(
					t,
					"view",
					toolruntime.StarlarkToolName,
					map[string]any{
						"code": `print('looking')
view(b"\x89PNG\r\n\x1a\nfixture", 'image/png')`,
					},
				)
				hasImage := func(request gai.GenerationRequest) bool {
					for _, block := range request.Dialog[len(request.Dialog)-1].Blocks {
						if block.ModalityType == gai.Image {
							return true
						}
					}
					return false
				}
				steps := []agenttest.GenerateStep{
					{
						Response: compactionResponse(view),
					},
					{
						Check: func(request gai.GenerationRequest) error {
							if !hasImage(request) {
								return errors.New("the first request did not carry the media")
							}
							return nil
						},
						Err: rejection,
					},
				}
				if test.retryErr != nil {
					steps = append(
						steps,
						agenttest.GenerateStep{
							Err: test.retryErr,
						},
					)
				} else {
					steps = append(
						steps,
						agenttest.GenerateStep{
							Check: func(request gai.GenerationRequest) error {
								last := request.Dialog[len(request.Dialog)-1]
								if hasImage(request) || len(last.Blocks) != 2 || last.Blocks[0].Content.String() != "looking" {
									return errors.New("the retry still carried the media")
								}
								notice = last.Blocks[1].Content.String()
								return nil
							},
							Response: compactionResponse(gai.TextBlock("resized")),
						},
					)
				}
				generator := agenttest.NewScriptedGenerator(steps...)
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
							acp.TextContentBlock("look at the screenshot"),
						},
					},
				)
				record, loadErr := fixture.store.Load(
					t.Context(),
					conversation.ID(id),
				)
				if loadErr != nil {
					t.Fatal(loadErr)
				}
				result := record.Dialog[2]
				if test.retryErr != nil {
					if err == nil || !strings.Contains(
						err.Error(),
						"unrelated problem",
					) {
						t.Fatalf(
							"prompt error = %v",
							err,
						)
					}
					if result.Blocks[1].ModalityType != gai.Image {
						t.Fatal("media was dropped from history although it was not the cause")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				match := regexp.MustCompile(`saved to (\S+);`).FindStringSubmatch(notice)
				if match == nil || !strings.Contains(
					notice,
					"image exceeds 5 MB maximum",
				) || !strings.Contains(
					notice,
					"image/png",
				) || !strings.HasSuffix(
					match[1],
					".png",
				) {
					t.Fatalf(
						"notice = %q",
						notice,
					)
				}
				saved, err := os.ReadFile(match[1])
				if err != nil || string(saved) != png {
					t.Fatalf(
						"saved media = %q, %v",
						saved,
						err,
					)
				}
				if result.Role != gai.ToolResult || result.Blocks[1].ModalityType != gai.Text || result.Blocks[1].Content.String() != notice {
					t.Fatalf(
						"saved history still holds the rejected media: %#v",
						result.Blocks,
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
