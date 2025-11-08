package commands

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/spachava753/cpe/internal/config"
)

func TestConfigLint(t *testing.T) {
	tests := []struct {
		name               string
		config             config.Config
		wantErr            bool
		wantOutputContains []string
	}{
		{
			name: "valid config with models",
			config: config.Config{
				Models: []config.ModelConfig{
					{Model: config.Model{Ref: "model1"}},
					{Model: config.Model{Ref: "model2"}},
				},
			},
			wantErr: false,
			wantOutputContains: []string{
				"✓ Configuration is valid",
				"Models: 2",
			},
		},
		{
			name: "valid config with default model",
			config: config.Config{
				Models: []config.ModelConfig{
					{Model: config.Model{Ref: "default-model"}},
				},
				Defaults: config.Defaults{
					Model: "default-model",
				},
			},
			wantErr: false,
			wantOutputContains: []string{
				"✓ Configuration is valid",
				"Default Model: default-model",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			opts := ConfigLintOptions{
				Config: tt.config,
				Writer: &buf,
			}

			err := ConfigLint(context.Background(), opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("ConfigLint() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			output := buf.String()
			for _, want := range tt.wantOutputContains {
				if !strings.Contains(output, want) {
					t.Errorf("ConfigLint() output does not contain %q\nOutput: %s", want, output)
				}
			}
		})
	}
}
