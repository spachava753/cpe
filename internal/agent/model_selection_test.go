package agent

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spachava753/acp-sdk/acp"

	"github.com/spachava753/cpe/internal/conversation"
	"github.com/spachava753/cpe/internal/models"
)

func TestLastUsedModel(t *testing.T) {
	for _, test := range []struct {
		name     string
		want     models.Selection
		wantLast models.ID
	}{
		{
			name: "new session",
			want: models.Selection{
				Model: "two",
			},
			wantLast: "two",
		},
		{
			name: "restart",
			want: models.Selection{
				Model: "two",
			},
			wantLast: "two",
		},
		{
			name: "load",
			want: models.Selection{
				Model:     "one",
				Reasoning: "high",
			},
			wantLast: "two",
		},
		{
			name: "resume after restart",
			want: models.Selection{
				Model:     "one",
				Reasoning: "high",
			},
			wantLast: "two",
		},
		{
			name: "fork",
			want: models.Selection{
				Model:     "one",
				Reasoning: "high",
			},
			wantLast: "two",
		},
		{
			name: "continue existing session",
			want: models.Selection{
				Model:     "one",
				Reasoning: "high",
			},
			wantLast: "one",
		},
		{
			name: "invalid selection",
			want: models.Selection{
				Model: "two",
			},
			wantLast: "two",
		},
		{
			name: "remembered model removed from catalog",
			want: models.Selection{
				Model: "one",
			},
			wantLast: "removed",
		},
		{
			name: "last session deleted",
			want: models.Selection{
				Model: "two",
			},
			wantLast: "two",
		},
		{
			name: "initial reasoning applies to remembered model",
			want: models.Selection{
				Model:     "two",
				Reasoning: "low",
			},
			wantLast: "two",
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				filename := filepath.Join(
					t.TempDir(),
					"last-model.json",
				)
				preference, preferenceErr := models.NewLastUsed(filename)
				if preferenceErr != nil {
					t.Fatal(preferenceErr)
				}
				fixture := serverFixture(
					t,
					scriptGenerator,
					func(_ *Config, deps *Dependencies, _ *acp.InitializeRequest) {
						deps.LastModel = preference
					},
				)
				original := openFixtureSession(
					t,
					fixture.client,
				)
				record, preferenceErr := fixture.store.Load(
					t.Context(),
					conversation.ID(original),
				)
				if preferenceErr != nil || record.Options.Model != "one" {
					t.Fatalf(
						"bootstrap model: %v, %v",
						record.Options,
						preferenceErr,
					)
				}
				if _, err := fixture.client.SetSessionConfigOption(
					t.Context(),
					&acp.SetSessionConfigOptionRequest{
						SessionID: original,
						ConfigID:  "reasoning",
						Value:     "high",
					},
				); err != nil {
					t.Fatal(err)
				}
				other := openFixtureSession(
					t,
					fixture.client,
				)
				if _, err := fixture.client.SetSessionConfigOption(
					t.Context(),
					&acp.SetSessionConfigOptionRequest{
						SessionID: other,
						ConfigID:  "model",
						Value:     "two",
					},
				); err != nil {
					t.Fatal(err)
				}
				if _, err := fixture.client.SetSessionConfigOption(
					t.Context(),
					&acp.SetSessionConfigOptionRequest{
						SessionID: other,
						ConfigID:  "reasoning",
						Value:     "low",
					},
				); err != nil {
					t.Fatal(err)
				}
				target := original
				switch test.name {
				case "restart", "resume after restart", "initial reasoning applies to remembered model":
					history := fixture.store
					if err := fixture.client.Close(); err != nil {
						t.Fatal(err)
					}
					// A new owner and server must read the file, not reuse a cached selection.
					reopened, err := models.NewLastUsed(filename)
					if err != nil {
						t.Fatal(err)
					}
					fixture = serverFixture(
						t,
						scriptGenerator,
						func(config *Config, deps *Dependencies, _ *acp.InitializeRequest) {
							deps.LastModel = reopened
							deps.History = history
							if test.name == "initial reasoning applies to remembered model" {
								config.Initial.Reasoning = "low"
							}
						},
					)
					if test.name == "resume after restart" {
						if _, err := fixture.client.ResumeSession(
							t.Context(),
							&acp.ResumeSessionRequest{
								SessionID: original,
								Cwd:       "/work",
							},
						); err != nil {
							t.Fatal(err)
						}
					} else {
						target = openFixtureSession(
							t,
							fixture.client,
						)
					}
				case "load":
					if _, err := fixture.client.CloseSession(
						t.Context(),
						&acp.CloseSessionRequest{
							SessionID: original,
						},
					); err != nil {
						t.Fatal(err)
					}
					if _, err := fixture.client.LoadSession(
						t.Context(),
						&acp.LoadSessionRequest{
							SessionID: original,
							Cwd:       "/work",
						},
					); err != nil {
						t.Fatal(err)
					}
				case "fork":
					forked, err := fixture.client.ForkSession(
						t.Context(),
						&acp.ForkSessionRequest{
							SessionID: original,
							Cwd:       "/work",
						},
					)
					if err != nil {
						t.Fatal(err)
					}
					target = forked.SessionID
				case "continue existing session":
					promptFixture(
						t,
						fixture.client,
						original,
						"print('continued')",
					)
				default:
					switch test.name {
					case "invalid selection":
						if _, err := fixture.client.SetSessionConfigOption(
							t.Context(),
							&acp.SetSessionConfigOptionRequest{
								SessionID: original,
								ConfigID:  "model",
								Value:     "invalid",
							},
						); err == nil {
							t.Fatal("invalid model selection succeeded")
						}
					case "remembered model removed from catalog":
						if err := preference.Save(
							t.Context(),
							"removed",
						); err != nil {
							t.Fatal(err)
						}
					case "last session deleted":
						if _, err := fixture.client.DeleteSession(
							t.Context(),
							&acp.DeleteSessionRequest{
								SessionID: other,
							},
						); err != nil {
							t.Fatal(err)
						}
					}
					target = openFixtureSession(
						t,
						fixture.client,
					)
				}
				record, preferenceErr = fixture.store.Load(
					t.Context(),
					conversation.ID(target),
				)
				if preferenceErr != nil || record.Options != test.want {
					t.Fatalf(
						"session selection: %v, %v; want %v",
						record.Options,
						preferenceErr,
						test.want,
					)
				}
				last, preferenceErr := preference.Load(t.Context())
				if preferenceErr != nil || last != test.wantLast {
					t.Fatalf(
						"last used: %q, %v; want %q",
						last,
						preferenceErr,
						test.wantLast,
					)
				}
				if test.name == "continue existing session" {
					next := openFixtureSession(
						t,
						fixture.client,
					)
					record, err := fixture.store.Load(
						t.Context(),
						conversation.ID(next),
					)
					if err != nil || record.Options.Model != "one" || record.Options.Reasoning != "" {
						t.Fatalf(
							"default after continued use: %v, %v",
							record.Options,
							err,
						)
					}
				}
			},
		)
	}
}

func TestModelPreferenceFailure(t *testing.T) {
	for _, test := range []struct {
		name      string
		wantError string
		directory bool
	}{
		{
			name:      "corrupt preference does not block restoration",
			wantError: "decode last-used model",
		},
		{
			name:      "failed preference write does not undo session selection",
			directory: true,
			wantError: "read last-used model",
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				filename := filepath.Join(
					t.TempDir(),
					"last-model.json",
				)
				preference, preferenceErr := models.NewLastUsed(filename)
				if preferenceErr != nil {
					t.Fatal(preferenceErr)
				}
				var diagnostics bytes.Buffer
				fixture := serverFixture(
					t,
					scriptGenerator,
					func(_ *Config, deps *Dependencies, _ *acp.InitializeRequest) {
						deps.LastModel = preference
						deps.Stderr = &diagnostics
					},
				)
				id := openFixtureSession(
					t,
					fixture.client,
				)
				if _, err := fixture.client.CloseSession(
					t.Context(),
					&acp.CloseSessionRequest{
						SessionID: id,
					},
				); err != nil {
					t.Fatal(err)
				}
				if test.directory {
					if err := os.Mkdir(
						filename,
						0o700,
					); err != nil {
						t.Fatal(err)
					}
				} else if err := os.WriteFile(
					filename,
					[]byte("{broken"),
					0o600,
				); err != nil {
					t.Fatal(err)
				}
				if _, err := fixture.client.LoadSession(
					t.Context(),
					&acp.LoadSessionRequest{
						SessionID: id,
						Cwd:       "/work",
					},
				); err != nil {
					t.Fatal(err)
				}
				if _, err := fixture.client.NewSession(
					t.Context(),
					&acp.NewSessionRequest{
						Cwd: "/work",
					},
				); err == nil || !strings.Contains(
					err.Error(),
					test.wantError,
				) {
					t.Fatalf(
						"new session must report unreadable preference: %v",
						err,
					)
				}
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
					"print('continued')",
				)
				record, preferenceErr := fixture.store.Load(
					t.Context(),
					conversation.ID(id),
				)
				if preferenceErr != nil || record.Options.Model != "two" {
					t.Fatalf(
						"session selection was not retained: %v, %v",
						record.Options,
						preferenceErr,
					)
				}
				if test.directory && !strings.Contains(
					diagnostics.String(),
					"could not remember last-used model",
				) {
					t.Fatal("preference write failure was not reported")
				}
			},
		)
	}
}
