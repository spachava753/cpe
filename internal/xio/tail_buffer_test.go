package xio_test

import (
	"testing"
	"unicode/utf8"

	"github.com/spachava753/cpe/internal/xio"
)

func TestTailBufferWrite(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name      string
		limit     int
		writes    []string
		want      string
		truncated bool
	}{
		{name: "unlimited", writes: []string{"abc", "def"}, want: "abcdef"},
		{name: "negative limit", limit: -1, writes: []string{"abc", "def"}, want: "abcdef"},
		{name: "empty", limit: 3, writes: []string{""}},
		{name: "exact limit", limit: 3, writes: []string{"abc"}, want: "abc"},
		{name: "exact limit across writes", limit: 3, writes: []string{"a", "bc", ""}, want: "abc"},
		{name: "replace existing bytes", limit: 3, writes: []string{"a", "bcd"}, want: "bcd", truncated: true},
		{name: "tail across writes", limit: 10, writes: []string{"abc", "def", "ghijklmnopqrstuvwxyz"}, want: "qrstuvwxyz", truncated: true},
		{name: "UTF-8 boundary", limit: 5, writes: []string{"ééé"}, want: "éé", truncated: true},
		{name: "split rune fits", limit: 3, writes: []string{"\xe2\x82", "\xac"}, want: "€"},
		{name: "split rune discarded", limit: 2, writes: []string{"\xe2\x82", "\xac", "a"}, want: "a", truncated: true},
		{name: "continuations after empty tail", limit: 1, writes: []string{"\xe2\x82", "\xac", ""}, want: "", truncated: true},
		{name: "rune completed in combined tail", limit: 4, writes: []string{"xx\xe2\x82", "\xac", "a"}, want: "€a", truncated: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			output := xio.NewTailBuffer(test.limit)
			for _, chunk := range test.writes {
				if n, err := output.Write([]byte(chunk)); n != len(chunk) || err != nil {
					t.Fatalf("Write(%q) = %d, %v, want %d, nil", chunk, n, err, len(chunk))
				}
			}
			if got := output.String(); got != test.want || !utf8.ValidString(got) {
				t.Fatalf("String() = %q, want valid UTF-8 %q", got, test.want)
			}
			if got := output.Truncated(); got != test.truncated {
				t.Fatalf("Truncated() = %t, want %t", got, test.truncated)
			}
		})
	}
}
