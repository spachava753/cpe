package codemode

import (
	"testing"

	"github.com/bradleyjkemp/cupaloy/v2"
)

func TestCheckReservedNameCollision(t *testing.T) {
	tests := []struct {
		name      string
		toolNames []string
		wantErr   bool
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
		},
		{
			name:      "collision when execute_go_code is first",
			toolNames: []string{"execute_go_code", "other_tool"},
			wantErr:   true,
		},
		{
			name:      "collision when execute_go_code is only tool",
			toolNames: []string{"execute_go_code"},
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckReservedNameCollision(tt.toolNames)
			if (err != nil) != tt.wantErr {
				t.Errorf("CheckReservedNameCollision() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				cupaloy.SnapshotT(t, err.Error())
			}
		})
	}
}

func TestCheckPascalCaseCollision(t *testing.T) {
	tests := []struct {
		name      string
		toolNames []string
		wantErr   bool
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
		},
		{
			name:      "collision with different case in underscore names",
			toolNames: []string{"get_weather", "get_Weather"},
			wantErr:   true,
		},
		{
			name:      "collision with mixed separators",
			toolNames: []string{"get-weather", "get_weather"},
			wantErr:   true,
		},
		{
			name:      "no collision with similar but different names",
			toolNames: []string{"get_weather", "get_weather_data"},
			wantErr:   false,
		},
		{
			name:      "error message contains both tool names",
			toolNames: []string{"foo_bar", "fooBar"},
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckPascalCaseCollision(tt.toolNames)
			if (err != nil) != tt.wantErr {
				t.Errorf("CheckPascalCaseCollision() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				cupaloy.SnapshotT(t, err.Error())
			}
		})
	}
}

func TestCheckToolNameCollisions(t *testing.T) {
	tests := []struct {
		name      string
		toolNames []string
		wantErr   bool
	}{
		{
			name:      "no collisions",
			toolNames: []string{"get_weather", "get_city", "read_file"},
			wantErr:   false,
		},
		{
			name:      "reserved name collision is caught first",
			toolNames: []string{"execute_go_code", "getWeather", "get_weather"},
			wantErr:   true,
		},
		{
			name:      "pascal case collision when no reserved collision",
			toolNames: []string{"get_weather", "getWeather"},
			wantErr:   true,
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
			if err != nil {
				cupaloy.SnapshotT(t, err.Error())
			}
		})
	}
}
