package toolruntime

import (
	"context"
	"slices"
	"testing"

	"github.com/spachava753/dyson"
)

func TestCompaction(t *testing.T) {
	for _, test := range []struct {
		name   string
		code   string
		active bool
		want   []string
		failed bool
	}{
		{
			name:   "summary handoff",
			code:   "compact('first')\ncompact(summary='second')",
			active: true,
			want: []string{
				"first",
				"second",
			},
		},
		{
			name:   "accepted summary survives script failure",
			code:   "compact('saved')\nfail('later failure')",
			active: true,
			want: []string{
				"saved",
			},
			failed: true,
		},
		{
			name:   "invalid summary does not replace accepted summary",
			code:   "compact('saved')\ncompact('  ')",
			active: true,
			want: []string{
				"saved",
			},
			failed: true,
		},
		{
			name:   "no active turn",
			code:   "compact('summary')",
			failed: true,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				host := hostFixture(
					t,
					emptyMCP{},
					runnerFunc(func(context.Context, dyson.Command) (dyson.CommandResult, error) {
						return dyson.CommandResult{}, nil
					}),
				)
				ctx := t.Context()
				var summaries []string
				if test.active {
					ctx = WithCompaction(
						ctx,
						func(summary string) {
							summaries = append(
								summaries,
								summary,
							)
						},
					)
				}
				result, err := host.execute(
					ctx,
					toolRequest(
						"starlark",
						"code",
						test.code,
					),
				)
				if err != nil {
					t.Fatal(err)
				}
				if result.ToolResultError != test.failed || !slices.Equal(
					summaries,
					test.want,
				) {
					t.Fatalf(
						"failed=%v summaries=%q; want failed=%v summaries=%q",
						result.ToolResultError,
						summaries,
						test.failed,
						test.want,
					)
				}
			},
		)
	}
}
