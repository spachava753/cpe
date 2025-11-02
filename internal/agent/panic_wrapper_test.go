package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/spachava753/gai"
)

// mockPanicGenerator is a test generator that panics
type mockPanicGenerator struct {
	panicMsg string
}

func (m *mockPanicGenerator) Generate(ctx context.Context, dialog gai.Dialog, options *gai.GenOpts) (gai.Response, error) {
	panic(m.panicMsg)
}

// mockNormalGenerator is a test generator that returns normally
type mockNormalGenerator struct {
	response gai.Response
	err      error
}

func (m *mockNormalGenerator) Generate(ctx context.Context, dialog gai.Dialog, options *gai.GenOpts) (gai.Response, error) {
	return m.response, m.err
}

func TestPanicCatchingGenerator(t *testing.T) {
	tests := []struct {
		name      string
		generator gai.Generator
		wantErr   bool
		errCheck  func(error) bool
	}{
		{
			name:      "catches string panic",
			generator: &mockPanicGenerator{panicMsg: "something went wrong"},
			wantErr:   true,
			errCheck: func(err error) bool {
				return strings.Contains(err.Error(), "generator panicked") &&
					strings.Contains(err.Error(), "something went wrong")
			},
		},
		{
			name:      "catches error panic",
			generator: &mockPanicGenerator{panicMsg: "critical failure"},
			wantErr:   true,
			errCheck: func(err error) bool {
				return strings.Contains(err.Error(), "generator panicked")
			},
		},
		{
			name: "normal response passes through",
			generator: &mockNormalGenerator{
				response: gai.Response{
					Candidates: []gai.Message{
						{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("test response")}},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "normal error passes through",
			generator: &mockNormalGenerator{
				err: errors.New("normal error"),
			},
			wantErr: true,
			errCheck: func(err error) bool {
				return err.Error() == "normal error"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wrapped := NewPanicCatchingGenerator(tt.generator)
			
			resp, err := wrapped.Generate(context.Background(), nil, nil)
			
			if tt.wantErr {
				if err == nil {
					t.Errorf("PanicCatchingGenerator.Generate() error = nil, wantErr %v", tt.wantErr)
					return
				}
				if tt.errCheck != nil && !tt.errCheck(err) {
					t.Errorf("PanicCatchingGenerator.Generate() error = %v, failed error check", err)
				}
			} else {
				if err != nil {
					t.Errorf("PanicCatchingGenerator.Generate() unexpected error = %v", err)
					return
				}
				if len(resp.Candidates) != 1 {
					t.Errorf("PanicCatchingGenerator.Generate() got %d candidates, want 1", len(resp.Candidates))
				}
			}
		})
	}
}

