package lintanalysis

import (
	"strings"
	"testing"
)

func TestEditedSource(t *testing.T) {
	for _, test := range []struct {
		name      string
		want      string
		edits     []textEdit
		wantError bool
	}{
		{
			name: "unordered edits and duplicate test variant",
			edits: []textEdit{
				{
					start: 4,
					end:   6,
					text:  "last",
				},
				{
					start: 0,
					end:   2,
					text:  "first",
				},
				{
					start: 0,
					end:   2,
					text:  "first",
				},
			},
			want: "firstcdlast",
		},
		{
			name: "adjacent edits",
			edits: []textEdit{
				{
					start: 0,
					end:   2,
					text:  "A",
				},
				{
					start: 2,
					end:   4,
					text:  "B",
				},
			},
			want: "ABef",
		},
		{
			name: "insertion at replacement boundary",
			edits: []textEdit{
				{
					start: 0,
					end:   0,
					text:  "A",
				},
				{
					start: 0,
					end:   2,
				},
			},
			want: "Acdef",
		},
		{
			name: "insertion at EOF",
			edits: []textEdit{
				{
					start: 6,
					end:   6,
					text:  "!",
				},
			},
			want: "abcdef!",
		},
		{
			name: "overlapping replacements",
			edits: []textEdit{
				{
					start: 0,
					end:   3,
					text:  "A",
				},
				{
					start: 2,
					end:   4,
					text:  "B",
				},
			},
			wantError: true,
		},
		{
			name: "different insertions at same position",
			edits: []textEdit{
				{
					start: 0,
					end:   0,
					text:  "A",
				},
				{
					start: 0,
					end:   0,
					text:  "B",
				},
			},
			wantError: true,
		},
		{
			name: "invalid range",
			edits: []textEdit{
				{
					start: 1,
					end:   7,
				},
			},
			wantError: true,
		},
		{
			name: "Unicode insertion",
			edits: []textEdit{
				{
					start: 0,
					end:   0,
					text:  "模型",
				},
			},
			want: "模型abcdef",
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				source := []byte("abcdef")
				got, err := editedSource(
					source,
					test.edits,
				)
				if (err != nil) != test.wantError {
					t.Fatalf(
						"edit error: %v",
						err,
					)
				}
				if !test.wantError && string(got) != test.want {
					t.Fatalf(
						"edited source = %q, want %q",
						got,
						test.want,
					)
				}
				if string(source) != "abcdef" {
					t.Fatal("editing changed input buffer")
				}
				if test.wantError && !strings.Contains(
					err.Error(),
					"suggested edit",
				) {
					t.Fatal(err)
				}
			},
		)
	}
}

func TestEditedSourceByteOffsets(t *testing.T) {
	got, err := editedSource(
		[]byte("模型abc"),
		[]textEdit{
			{
				start: 6,
				end:   7,
				text:  "X",
			},
		},
	)
	if err != nil || string(got) != "模型Xbc" {
		t.Fatalf(
			"byte offset edit: %q, %v",
			got,
			err,
		)
	}
}
