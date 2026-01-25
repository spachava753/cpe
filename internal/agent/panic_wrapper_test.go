package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/bradleyjkemp/cupaloy/v2"
	"github.com/spachava753/gai"
)

// mockPanicGenerator is a test generator that panics
type mockPanicGenerator struct {
	panicMsg string
}

func (m *mockPanicGenerator) Register(tool gai.Tool) error {
	//TODO implement me
	panic("implement me")
}

func (m *mockPanicGenerator) Generate(ctx context.Context, dialog gai.Dialog, options *gai.GenOpts) (gai.Response, error) {
	panic(m.panicMsg)
}

// mockNormalGenerator is a test generator that returns normally
type mockNormalGenerator struct {
	response gai.Response
	err      error
}

func (m *mockNormalGenerator) Register(tool gai.Tool) error {
	//TODO implement me
	panic("implement me")
}

func (m *mockNormalGenerator) Generate(ctx context.Context, dialog gai.Dialog, options *gai.GenOpts) (gai.Response, error) {
	return m.response, m.err
}

func TestPanicCatchingGenerator(t *testing.T) {
	tests := []struct {
		name         string
		generator    gai.ToolCapableGenerator
		wantErr      bool
		errCheck     func(error) bool // used for panic cases with non-deterministic stack traces
		snapshotErr  bool             // if true, snapshot the error message
		snapshotResp bool             // if true, snapshot the response
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
			wantErr:      false,
			snapshotResp: true,
		},
		{
			name: "normal error passes through",
			generator: &mockNormalGenerator{
				err: errors.New("normal error"),
			},
			wantErr:     true,
			snapshotErr: true,
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
				if tt.snapshotErr {
					cupaloy.SnapshotT(t, err.Error())
				} else if tt.errCheck != nil && !tt.errCheck(err) {
					t.Errorf("PanicCatchingGenerator.Generate() error = %v, failed error check", err)
				}
			} else {
				if err != nil {
					t.Errorf("PanicCatchingGenerator.Generate() unexpected error = %v", err)
					return
				}
				if tt.snapshotResp {
					cupaloy.SnapshotT(t, resp)
				}
			}
		})
	}
}
