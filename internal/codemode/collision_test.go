package codemode

import (
	"strings"
	"testing"
)

func TestCheckReservedNameCollision(t *testing.T) {
	tests := []struct {
		name      string
		toolNames []string
		wantErr   bool
		errContains string
	}{
		{
			name:      "no collision with empty list",
			toolNames: []string{},
			wantErr:   false,
		},
		{
			name:      "no collision with single tool",
			toolNames: []string{"get_weather"},
			wantErr:   false,
		},
		{
			name:      "no collision with multiple tools",
			toolNames: []string{"get_weather", "get_city", "read_file"},
			wantErr:   false,
		},
		{
			name:      "collision with execute_go_code",
			toolNames: []string{"get_weather", "execute_go_code", "get_city"},
			wantErr:   true,
			errContains: "execute_go_code",
		},
		{
			name:      "collision when execute_go_code is first",
			toolNames: []string{"execute_go_code", "other_tool"},
			wantErr:   true,
			errContains: "reserved code mode tool name",
		},
		{
			name:      "collision when execute_go_code is only tool",
			toolNames: []string{"execute_go_code"},
			wantErr:   true,
			errContains: "excludedTools",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckReservedNameCollision(tt.toolNames)
			if (err != nil) != tt.wantErr {
				t.Errorf("CheckReservedNameCollision() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("CheckReservedNameCollision() error = %v, want error containing %q", err, tt.errContains)
				}
			}
		})
	}
}

func TestCheckPascalCaseCollision(t *testing.T) {
	tests := []struct {
		name        string
		toolNames   []string
		wantErr     bool
		errContains string
	}{
		{
			name:      "no collision with empty list",
			toolNames: []string{},
			wantErr:   false,
		},
		{
			name:      "no collision with single tool",
			toolNames: []string{"get_weather"},
			wantErr:   false,
		},
		{
			name:      "no collision with different tools",
			toolNames: []string{"get_weather", "get_city", "read_file"},
			wantErr:   false,
		},
		{
			name:      "collision with underscore vs camelCase",
			toolNames: []string{"get_weather", "getWeather"},
			wantErr:   true,
			errContains: "GetWeather",
		},
		{
			name:      "collision with different case in underscore names",
			toolNames: []string{"get_weather", "get_Weather"},
			wantErr:   true,
			errContains: "GetWeather",
		},
		{
			name:      "collision with mixed separators",
			toolNames: []string{"get-weather", "get_weather"},
			wantErr:   true,
			errContains: "GetWeather",
		},
		{
			name:      "no collision with similar but different names",
			toolNames: []string{"get_weather", "get_weather_data"},
			wantErr:   false,
		},
		{
			name:        "error message contains both tool names",
			toolNames:   []string{"foo_bar", "fooBar"},
			wantErr:     true,
			errContains: "foo_bar",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckPascalCaseCollision(tt.toolNames)
			if (err != nil) != tt.wantErr {
				t.Errorf("CheckPascalCaseCollision() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("CheckPascalCaseCollision() error = %v, want error containing %q", err, tt.errContains)
				}
			}
		})
	}
}

func TestCheckToolNameCollisions(t *testing.T) {
	tests := []struct {
		name        string
		toolNames   []string
		wantErr     bool
		errContains string
	}{
		{
			name:      "no collisions",
			toolNames: []string{"get_weather", "get_city", "read_file"},
			wantErr:   false,
		},
		{
			name:        "reserved name collision is caught first",
			toolNames:   []string{"execute_go_code", "getWeather", "get_weather"},
			wantErr:     true,
			errContains: "reserved code mode tool name",
		},
		{
			name:        "pascal case collision when no reserved collision",
			toolNames:   []string{"get_weather", "getWeather"},
			wantErr:     true,
			errContains: "GetWeather",
		},
		{
			name:      "empty list passes",
			toolNames: []string{},
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckToolNameCollisions(tt.toolNames)
			if (err != nil) != tt.wantErr {
				t.Errorf("CheckToolNameCollisions() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("CheckToolNameCollisions() error = %v, want error containing %q", err, tt.errContains)
				}
			}
		})
	}
}
