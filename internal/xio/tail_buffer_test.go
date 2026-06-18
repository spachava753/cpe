package xio

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTailBuffer(t *testing.T) {
	t.Parallel()

	t.Run("without limit retains all output", func(t *testing.T) {
		t.Parallel()

		output := NewTailBuffer(0)
		if n, err := output.Write([]byte("abc")); err != nil || n != 3 {
			t.Fatalf("Write() = %d, %v, want 3, nil", n, err)
		}
		if n, err := output.Write([]byte("def")); err != nil || n != 3 {
			t.Fatalf("Write() = %d, %v, want 3, nil", n, err)
		}

		if got := output.String(); got != "abcdef" {
			t.Fatalf("String() = %q, want %q", got, "abcdef")
		}
		if output.Truncated() {
			t.Fatal("Truncated() = true, want false")
		}
	})

	t.Run("retains tail across writes", func(t *testing.T) {
		t.Parallel()

		output := NewTailBuffer(10)
		for _, chunk := range []string{"abc", "def", "ghijklmnopqrstuvwxyz"} {
			if _, err := output.Write([]byte(chunk)); err != nil {
				t.Fatalf("Write(%q) error = %v, want nil", chunk, err)
			}
		}

		if got := output.String(); got != "qrstuvwxyz" {
			t.Fatalf("String() = %q, want %q", got, "qrstuvwxyz")
		}
		if !output.Truncated() {
			t.Fatal("Truncated() = false, want true")
		}
		if strings.Contains(output.String(), "abcdef") {
			t.Fatalf("String() = %q, want beginning truncated", output.String())
		}
	})

	t.Run("truncates at UTF-8 boundary", func(t *testing.T) {
		t.Parallel()

		output := NewTailBuffer(5)
		if _, err := output.Write([]byte("ééé")); err != nil {
			t.Fatalf("Write() error = %v, want nil", err)
		}

		got := output.String()
		if !utf8.ValidString(got) {
			t.Fatalf("String() = %q, want valid UTF-8", got)
		}
		if got != "éé" {
			t.Fatalf("String() = %q, want %q", got, "éé")
		}
		if !output.Truncated() {
			t.Fatal("Truncated() = false, want true")
		}
	})
}
