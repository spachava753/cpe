package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/commandio"
)

func TestCommands(t *testing.T) {
	for _, test := range []struct {
		name        string
		logout      bool
		provider    Provider
		cancel      bool
		promptError bool
		outputError bool
		nilStore    bool
		wantError   bool
		wantChanged bool
		wantOutput  string
	}{
		{
			name:        "login confirmation",
			provider:    OpenCode,
			wantChanged: true,
			wantOutput:  "Saved opencode credentials.\n",
		},
		{
			name:        "logout confirmation",
			logout:      true,
			provider:    OpenAI,
			wantChanged: true,
			wantOutput:  "Removed saved openai credentials.\n",
		},
		{
			name:        "failed login",
			provider:    OpenCode,
			promptError: true,
			wantError:   true,
		},
		{
			name:      "canceled login",
			provider:  OpenCode,
			cancel:    true,
			wantError: true,
		},
		{
			name:      "canceled logout",
			logout:    true,
			provider:  OpenAI,
			cancel:    true,
			wantError: true,
		},
		{
			name:      "invalid login provider",
			wantError: true,
		},
		{
			name:      "invalid logout provider",
			logout:    true,
			wantError: true,
		},
		{
			name:        "login output error follows saved credentials",
			provider:    OpenCode,
			outputError: true,
			wantError:   true,
			wantChanged: true,
		},
		{
			name:        "logout output error follows removed credentials",
			logout:      true,
			provider:    OpenAI,
			outputError: true,
			wantError:   true,
			wantChanged: true,
		},
		{
			name:      "missing login store",
			provider:  OpenCode,
			nilStore:  true,
			wantError: true,
		},
		{
			name:      "missing logout store",
			logout:    true,
			provider:  OpenAI,
			nilStore:  true,
			wantError: true,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				filesystem := afero.NewMemMapFs()
				path := credentialFile
				if err := afero.WriteFile(
					filesystem,
					path,
					[]byte(testAuthDocument),
					0o600,
				); err != nil {
					t.Fatal(err)
				}
				store := NewStore(filesystem)
				promptFailure := errors.New("API-key form aborted")
				flow := loginFlow{
					promptKey: func(context.Context, commandio.IO) (string, error) {
						if test.promptError {
							return "", promptFailure
						}
						return "new-key", nil
					},
				}
				if test.nilStore {
					store = nil
				}
				var output, diagnostics bytes.Buffer
				var writer io.Writer = &output
				outputFailure := errors.New("result stream closed")
				if test.outputError {
					writer = authFailingWriter{
						err: outputFailure,
					}
				}
				streams := commandio.IO{
					Out: writer,
					Err: &diagnostics,
				}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if test.cancel {
					cancel()
				}
				var err error
				if test.logout {
					err = Logout(
						ctx,
						store,
						LogoutOptions{
							Provider: test.provider,
							Path:     path,
						},
						streams,
					)
				} else {
					err = login(
						ctx,
						store,
						LoginOptions{
							Provider: test.provider,
							Path:     path,
						},
						streams,
						flow,
					)
				}
				if (err != nil) != test.wantError {
					t.Fatalf(
						"unexpected error: %v",
						err,
					)
				}
				if test.outputError && !errors.Is(
					err,
					outputFailure,
				) {
					t.Error("output error was lost")
				}
				if test.promptError && !errors.Is(
					err,
					promptFailure,
				) {
					t.Error("login error was lost")
				}
				if test.cancel && !errors.Is(
					err,
					context.Canceled,
				) {
					t.Error("cancellation was lost")
				}
				if output.String() != test.wantOutput || diagnostics.Len() != 0 {
					t.Errorf(
						"stdout = %q, stderr = %q",
						&output,
						&diagnostics,
					)
				}
				data, err := afero.ReadFile(
					filesystem,
					path,
				)
				if err != nil {
					t.Fatal(err)
				}
				if !test.wantChanged {
					if string(data) != testAuthDocument {
						t.Error("failed operation changed credentials")
					}
					return
				}
				var document Credentials
				if err := json.Unmarshal(
					data,
					&document,
				); err != nil {
					t.Fatal(err)
				}
				if test.logout {
					if document.OpenAI != nil || document.OpenCode == nil || document.OpenCode.APIKey != "old" {
						t.Error("logout did not remove only OpenAI")
					}
				} else if document.OpenAI == nil || document.OpenAI.AccessToken != "access" || document.OpenCode == nil || document.OpenCode.APIKey != "new-key" {
					t.Error("login did not replace only OpenCode")
				}
			},
		)
	}
}

type authFailingWriter struct {
	err error
}

func (writer authFailingWriter) Write([]byte) (int, error) {
	return 0, writer.err
}
