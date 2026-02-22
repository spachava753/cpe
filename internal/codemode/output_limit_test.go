package codemode

import "testing"

func TestResolveLargeOutputCharLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		configured    int
		contextWindow uint32
		want          int
	}{
		{
			name:          "uses configured limit when provided",
			configured:    12345,
			contextWindow: 200000,
			want:          12345,
		},
		{
			name:          "derives limit from context window",
			configured:    0,
			contextWindow: 200000,
			want:          160000,
		},
		{
			name:          "zero context window returns zero",
			configured:    0,
			contextWindow: 0,
			want:          0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveLargeOutputCharLimit(tt.configured, tt.contextWindow)
			if got != tt.want {
				t.Fatalf("result mismatch: got %d, want %d", got, tt.want)
			}
		})
	}
}
