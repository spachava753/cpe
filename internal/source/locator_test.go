package source_test

import (
	"testing"

	"github.com/spachava753/cpe/internal/source"
)

func TestParseGitLocator(t *testing.T) {
	for _, test := range []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "GitHub shorthand resolves to a Git URL",
			input: "owner/repo",
			want:  "https://github.com/owner/repo",
		},
		{
			name:  "SSH transport remains explicit",
			input: "ssh://git@example.com/team/repo.git",
			want:  "ssh://git@example.com/team/repo.git",
		},
		{
			name:  "SCP syntax is unsupported",
			input: "git@example.com:team/repo.git",
		},
		{
			name:  "relative repository paths are unsupported",
			input: "../repo",
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				got, err := source.ParseGitLocator(test.input)
				if test.want == "" {
					if err == nil || got.String() != "" {
						t.Fatalf(
							"ParseGitLocator = %q, %v",
							got.String(),
							err,
						)
					}
					return
				}
				if err != nil || got.String() != test.want {
					t.Fatalf(
						"ParseGitLocator = %q, %v; want %q",
						got.String(),
						err,
						test.want,
					)
				}
			},
		)
	}
}
