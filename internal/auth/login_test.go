package auth

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/commandio"
)

func TestParseProvider(t *testing.T) {
	for _, test := range []struct {
		name  string
		input string
		want  Provider
	}{
		{
			name:  "openai",
			input: "openai",
			want:  OpenAI,
		},
		{
			name:  "OpenAI",
			input: "OpenAI",
			want:  OpenAI,
		},
		{
			name:  "OpenCode",
			input: "OpenCode",
			want:  OpenCode,
		},
		{
			name:  "empty",
			input: "",
		},
		{
			name:  "unknown",
			input: "other",
		},
		{
			name:  "path",
			input: "../openai",
		},
		{
			name:  "whitespace",
			input: " openai",
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				got, err := ParseProvider(test.input)
				if got != test.want || (err != nil) != (test.want == 0) {
					t.Fatalf(
						"got provider %v, error %v",
						got,
						err,
					)
				}
			},
		)
	}
}

func TestLogin(t *testing.T) {
	for _, test := range []struct {
		name          string
		key           string
		promptErr     error
		provider      Provider
		device        bool
		cancel        bool
		missingPath   bool
		missingPrompt bool
		corruptFile   bool
		wantErr       bool
		wantPrompt    bool
	}{
		{
			name:       "key",
			key:        "test-key",
			provider:   OpenCode,
			wantPrompt: true,
		},
		{
			name:        "missing path fails before prompt",
			provider:    OpenCode,
			missingPath: true,
			wantErr:     true,
		},
		{
			name:          "missing prompt fails before storage",
			provider:      OpenCode,
			missingPrompt: true,
			wantErr:       true,
		},
		{
			name:        "corrupt storage fails before prompt",
			provider:    OpenCode,
			corruptFile: true,
			wantErr:     true,
		},
		{
			name:     "missing HTTP client fails before authorization",
			provider: OpenAI,
			wantErr:  true,
		},
		{
			name:       "trim paste",
			key:        "  test-key\n",
			provider:   OpenCode,
			wantPrompt: true,
		},
		{
			name:       "empty key",
			provider:   OpenCode,
			wantErr:    true,
			wantPrompt: true,
		},
		{
			name:       "embedded whitespace",
			key:        "test key",
			provider:   OpenCode,
			wantErr:    true,
			wantPrompt: true,
		},
		{
			name:       "escape",
			key:        "test\x1bkey",
			provider:   OpenCode,
			wantErr:    true,
			wantPrompt: true,
		},
		{
			name:       "invalid UTF8",
			key:        "test\xffkey",
			provider:   OpenCode,
			wantErr:    true,
			wantPrompt: true,
		},
		{
			name: "too large",
			key: strings.Repeat(
				"x",
				16*1024+1,
			),
			provider:   OpenCode,
			wantErr:    true,
			wantPrompt: true,
		},
		{
			name:       "form aborted",
			provider:   OpenCode,
			promptErr:  errors.New("aborted"),
			wantErr:    true,
			wantPrompt: true,
		},
		{
			name:     "invalid provider",
			provider: 99,
			wantErr:  true,
		},
		{
			name:     "wrong device provider",
			provider: OpenCode,
			device:   true,
			wantErr:  true,
		},
		{
			name:     "canceled",
			provider: OpenCode,
			cancel:   true,
			wantErr:  true,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				filesystem := afero.NewMemMapFs()
				original := testAuthDocument
				if test.corruptFile {
					original = "private-not-json"
				}
				if err := afero.WriteFile(
					filesystem,
					credentialFile,
					[]byte(original),
					0o600,
				); err != nil {
					t.Fatal(err)
				}
				store := NewStore(filesystem)
				var output, diagnostics bytes.Buffer
				streams := commandio.IO{
					In:  strings.NewReader("input"),
					Out: &output,
					Err: &diagnostics,
				}
				prompted := false
				flow := loginFlow{
					promptKey: func(_ context.Context, got commandio.IO) (string, error) {
						prompted = true
						if got != streams {
							t.Error("streams not forwarded")
						}
						return test.key, test.promptErr
					},
				}
				if test.missingPrompt {
					flow.promptKey = nil
				}
				path := credentialFile
				if test.missingPath {
					path = ""
				}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if test.cancel {
					cancel()
				}
				err := login(
					ctx,
					store,
					LoginOptions{
						Provider: test.provider,
						Path:     path,
						Device:   test.device,
					},
					streams,
					flow,
				)
				if (err != nil) != test.wantErr {
					t.Fatalf(
						"unexpected error: %v",
						err,
					)
				}
				if prompted != test.wantPrompt {
					t.Errorf(
						"prompted: %v",
						prompted,
					)
				}
				data, err := afero.ReadFile(
					filesystem,
					credentialFile,
				)
				if err != nil {
					t.Fatal(err)
				}
				if test.wantErr {
					if string(data) != original || output.Len() != 0 {
						t.Error("failed login changed credentials or printed success")
					}
				} else {
					document, err := store.Retrieve(
						t.Context(),
						credentialFile,
					)
					if err != nil {
						t.Fatal(err)
					}
					if document.OpenCode == nil || document.OpenCode.APIKey != "test-key" || document.OpenAI == nil || document.OpenAI.AccessToken != "access" {
						t.Error("login did not replace only OpenCode")
					}
					if output.String() != "Saved opencode credentials.\n" {
						t.Error("missing confirmation")
					}
				}
				if diagnostics.Len() != 0 {
					t.Error("API-key login printed diagnostics")
				}
			},
		)
	}
}

func TestPromptAPIKey(t *testing.T) {
	for _, test := range []struct {
		name string
		ctx  context.Context
	}{
		{
			name: "nonterminal",
			ctx:  context.Background(),
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				var output bytes.Buffer
				key, err := promptAPIKey(
					test.ctx,
					commandio.IO{
						In:  strings.NewReader("must-not-echo"),
						Err: &output,
					},
				)
				if key != "" || err == nil || output.Len() != 0 {
					t.Error("nonterminal prompt accepted input or exposed data")
				}
			},
		)
	}
}
