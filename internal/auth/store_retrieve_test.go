package auth

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
)

func TestStoreRetrieve(t *testing.T) {
	for _, test := range []struct {
		name         string
		original     string
		missingPath  bool
		missingFS    bool
		zeroStore    bool
		nilStore     bool
		cancel       bool
		wantErr      bool
		wantOpenAI   bool
		wantOpenCode bool
	}{
		{
			name:         "both providers",
			original:     testAuthDocument,
			wantOpenAI:   true,
			wantOpenCode: true,
		},
		{
			name:       "OpenAI only",
			original:   `{"openai":` + testOpenAIEntry + `}`,
			wantOpenAI: true,
		},
		{
			name:         "OpenCode only",
			original:     `{"opencode":{"api_key":"old"}}`,
			wantOpenCode: true,
		},
		{
			name: "absent file",
		},
		{
			name:     "empty document",
			original: `{}`,
		},
		{
			name:     "malformed document",
			original: "private-not-json",
			wantErr:  true,
		},
		{
			name:     "unknown fields",
			original: `{"other":{"api_key":"private-key"}}`,
			wantErr:  true,
		},
		{
			name:     "partial OpenAI entry",
			original: `{"openai":{"access_token":"private-token"}}`,
			wantErr:  true,
		},
		{
			name:     "invalid OpenCode entry",
			original: `{"opencode":{"api_key":" "}}`,
			wantErr:  true,
		},
		{
			name:     "null",
			original: `null`,
			wantErr:  true,
		},
		{
			name:     "array",
			original: `[]`,
			wantErr:  true,
		},
		{
			name:     "trailing data",
			original: testAuthDocument + ` {}`,
			wantErr:  true,
		},
		{
			name:     "invalid UTF8",
			original: "{\"opencode\":{\"api_key\":\"private\xff\"}}",
			wantErr:  true,
		},
		{
			name: "oversized document",
			original: strings.Repeat(
				"x",
				maxCredentialFile+1,
			),
			wantErr: true,
		},
		{
			name:        "missing path",
			missingPath: true,
			wantErr:     true,
		},
		{
			name:      "missing filesystem",
			missingFS: true,
			wantErr:   true,
		},
		{
			name:      "zero store",
			zeroStore: true,
			wantErr:   true,
		},
		{
			name:     "nil store",
			nilStore: true,
			wantErr:  true,
		},
		{
			name:    "canceled",
			cancel:  true,
			wantErr: true,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				t.Setenv(
					"HOME",
					"",
				)
				t.Setenv(
					"USERPROFILE",
					"",
				)
				filesystem := afero.NewMemMapFs()
				path := filepath.Join(
					"config",
					"credentials.data",
				)
				if test.original != "" {
					if err := filesystem.MkdirAll(
						"config",
						0o700,
					); err != nil {
						t.Fatal(err)
					}
					if err := afero.WriteFile(
						filesystem,
						path,
						[]byte(test.original),
						0o600,
					); err != nil {
						t.Fatal(err)
					}
				}
				store := NewStore(filesystem)
				if test.missingFS {
					store = NewStore(nil)
				}
				if test.zeroStore {
					store = &Store{}
				}
				if test.nilStore {
					store = nil
				}
				if test.missingPath {
					path = ""
				}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if test.cancel {
					cancel()
				}
				got, err := store.Retrieve(
					ctx,
					path,
				)
				if (err != nil) != test.wantErr {
					t.Fatalf(
						"unexpected error: %v",
						err,
					)
				}
				if (got.OpenAI != nil) != test.wantOpenAI || (got.OpenCode != nil) != test.wantOpenCode {
					t.Error("wrong retrieved providers or partial data on failure")
				}
				if got.OpenAI != nil && got.OpenAI.AccessToken != "access" {
					t.Error("retrieved OpenAI credentials changed")
				}
				if got.OpenCode != nil && got.OpenCode.APIKey != "old" {
					t.Error("retrieved API key changed")
				}
				if err != nil && strings.Contains(
					err.Error(),
					"private",
				) {
					t.Error("error exposed credential data")
				}
				if test.original != "" {
					data, err := afero.ReadFile(
						filesystem,
						path,
					)
					if err != nil || string(data) != test.original {
						t.Error("retrieve changed credentials")
					}
				} else if entries, err := afero.ReadDir(
					filesystem,
					".",
				); err != nil || len(entries) != 0 {
					t.Error("retrieve created state")
				}
			},
		)
	}
}
