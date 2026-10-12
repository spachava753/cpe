package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"

	"github.com/spachava753/cpe/internal/auth"
	"github.com/spachava753/cpe/internal/commandio"
)

func TestAuthCommands(t *testing.T) {
	for _, test := range []struct {
		name        string
		arguments   []string
		login       bool
		logout      bool
		provider    auth.Provider
		device      bool
		actionError bool
		wantErr     bool
	}{
		{
			name: "openai login",
			arguments: []string{
				"auth",
				"login",
				"OpenAI",
			},
			login:    true,
			provider: auth.OpenAI,
		},
		{
			name: "device login",
			arguments: []string{
				"auth",
				"login",
				"openai",
				"--device",
			},
			login:    true,
			provider: auth.OpenAI,
			device:   true,
		},
		{
			name: "opencode login",
			arguments: []string{
				"auth",
				"login",
				"opencode",
			},
			login:    true,
			provider: auth.OpenCode,
		},
		{
			name: "logout",
			arguments: []string{
				"auth",
				"logout",
				"opencode",
			},
			logout:   true,
			provider: auth.OpenCode,
		},
		{
			name: "login error",
			arguments: []string{
				"auth",
				"login",
				"openai",
			},
			login:       true,
			provider:    auth.OpenAI,
			actionError: true,
			wantErr:     true,
		},
		{
			name: "logout error",
			arguments: []string{
				"auth",
				"logout",
				"openai",
			},
			logout:      true,
			provider:    auth.OpenAI,
			actionError: true,
			wantErr:     true,
		},
		{
			name: "missing provider",
			arguments: []string{
				"auth",
				"login",
			},
			wantErr: true,
		},
		{
			name: "too many arguments",
			arguments: []string{
				"auth",
				"logout",
				"openai",
				"opencode",
			},
			wantErr: true,
		},
		{
			name: "unknown provider",
			arguments: []string{
				"auth",
				"login",
				"unknown",
			},
			wantErr: true,
		},
		{
			name: "unknown logout provider",
			arguments: []string{
				"auth",
				"logout",
				"unknown",
			},
			wantErr: true,
		},
		{
			name: "unknown flag",
			arguments: []string{
				"auth",
				"login",
				"openai",
				"--key=secret",
			},
			wantErr: true,
		},
		{
			name: "login help",
			arguments: []string{
				"auth",
				"login",
				"--help",
			},
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				ctx := context.WithValue(
					context.Background(),
					authContextKey{},
					"test",
				)
				var output, diagnostics bytes.Buffer
				input := strings.NewReader("input")
				loginCalled, logoutCalled := false, false
				actionError := errors.New("authentication failed")
				store := auth.NewStore(afero.NewMemMapFs())
				path := filepath.Join(
					t.TempDir(),
					"auth.json",
				)
				client := &http.Client{}
				configuration := auth.LoginOptions{
					Path:   path,
					Client: client,
				}
				actions := authActions{
					login: func(got context.Context, gotStore *auth.Store, options auth.LoginOptions, streams commandio.IO) error {
						loginCalled = true
						if got != ctx || gotStore != store || options.Provider != test.provider || options.Device != test.device || options.Path != path || options.Client != client {
							t.Error("incorrect login options or context")
						}
						if streams.In != input || streams.Out != &output || streams.Err != &diagnostics {
							t.Error("incorrect command streams")
						}
						if test.actionError {
							return actionError
						}
						return nil
					},
					logout: func(got context.Context, gotStore *auth.Store, options auth.LogoutOptions, streams commandio.IO) error {
						logoutCalled = true
						if got != ctx || gotStore != store || options.Provider != test.provider || options.Path != path {
							t.Error("incorrect logout options or context")
						}
						if streams.In != input || streams.Out != &output || streams.Err != &diagnostics {
							t.Error("incorrect command streams")
						}
						if test.actionError {
							return actionError
						}
						return nil
					},
				}
				root := &cobra.Command{
					Use:           "cpe",
					SilenceUsage:  true,
					SilenceErrors: true,
				}
				authCmdWithActions(
					root,
					store,
					configuration,
					actions,
				)
				root.SetIn(input)
				root.SetOut(&output)
				root.SetErr(&diagnostics)
				root.SetArgs(test.arguments)
				err := root.ExecuteContext(ctx)
				if (err != nil) != test.wantErr {
					t.Fatalf(
						"error: %v",
						err,
					)
				}
				if test.actionError && !errors.Is(
					err,
					actionError,
				) {
					t.Error("action error was lost")
				}
				if loginCalled != test.login || logoutCalled != test.logout {
					t.Error("incorrect action invoked")
				}
				if err != nil && output.Len() != 0 {
					t.Error("failed authentication reported success")
				}
			},
		)
	}
}

type authContextKey struct{}

func TestRootAuth(t *testing.T) {
	for _, test := range []struct {
		name      string
		arguments []string
		want      []string
	}{
		{
			name: "registered",
			arguments: []string{
				"auth",
				"--help",
			},
			want: []string{
				"login",
				"logout",
				"auth.json",
			},
		},
		{
			name: "login help",
			arguments: []string{
				"auth",
				"login",
				"--help",
			},
			want: []string{
				"--device",
				"127.0.0.1:1455",
				"cpe auth login openai",
				"cpe auth login opencode",
			},
		},
		{
			name: "logout help",
			arguments: []string{
				"auth",
				"logout",
				"--help",
			},
			want: []string{
				"auth.json",
				"Other providers are preserved",
				"cpe auth logout openai",
				"cpe auth logout opencode",
			},
		},
		{
			name: "completion",
			arguments: []string{
				"__complete",
				"auth",
				"login",
				"",
			},
			want: []string{
				"openai",
				"opencode",
			},
		},
		{
			name: "logout completion",
			arguments: []string{
				"__complete",
				"auth",
				"logout",
				"",
			},
			want: []string{
				"openai",
				"opencode",
			},
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				root := Root(
					"test",
					nil,
					nil,
					nil,
					auth.LoginOptions{},
				)
				root.SilenceUsage = true
				root.SilenceErrors = true
				var output bytes.Buffer
				root.SetOut(&output)
				root.SetErr(io.Discard)
				root.SetArgs(test.arguments)
				if err := root.Execute(); err != nil {
					t.Fatalf(
						"error: %v",
						err,
					)
				}
				for _, want := range test.want {
					if !strings.Contains(
						output.String(),
						want,
					) {
						t.Errorf(
							"output %q is missing %q",
							&output,
							want,
						)
					}
				}
			},
		)
	}
}
