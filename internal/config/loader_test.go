package config

import "testing"

func TestParseConfigDataRejectsJSON(t *testing.T) {
	_, err := parseConfigData([]byte(`{"models":[]}`), "cpe.json")
	if err == nil {
		t.Fatal("expected JSON config error")
	}
	want := "JSON config files are unsupported; use YAML (.yaml or .yml)"
	if err.Error() != want {
		t.Fatalf("unexpected error: got %q want %q", err.Error(), want)
	}
}

func TestParseConfigDataRejectsRemovedTopLevelFields(t *testing.T) {
	for _, field := range []string{"defaults", "mcpServers"} {
		t.Run(field, func(t *testing.T) {
			_, err := parseConfigData([]byte("version: \"1.0\"\nmodels: []\n"+field+": {}\n"), "cpe.yaml")
			if err == nil {
				t.Fatalf("expected unknown %s field error", field)
			}
		})
	}
}

func TestParseConfigDataSupportsDisableEditTool(t *testing.T) {
	cfg, err := parseConfigData([]byte(`models:
  - ref: test-model
    display_name: Test Model
    id: test-id
    type: openai
    api_key_env: TEST_API_KEY
    context_window: 200000
    max_output: 64000
    disable_edit_tool: true
`), "cpe.yaml")
	if err != nil {
		t.Fatalf("parseConfigData returned error: %v", err)
	}
	if !cfg.Models[0].DisableEditTool {
		t.Fatal("DisableEditTool = false, want true")
	}
}

func TestParseConfigDataSupportsAnthropicVertexConfig(t *testing.T) {
	cfg, err := parseConfigData([]byte(`models:
  - ref: vertex-sonnet
    display_name: Vertex Sonnet
    id: claude-sonnet-4-6
    type: anthropic_vertex
    context_window: 200000
    max_output: 64000
    vertex:
      project_id: test-project
      region: global
      scopes:
        - https://www.googleapis.com/auth/cloud-platform
`), "cpe.yaml")
	if err != nil {
		t.Fatalf("parseConfigData returned error: %v", err)
	}
	vertex := cfg.Models[0].Vertex
	if vertex == nil {
		t.Fatal("Vertex = nil, want config")
	}
	if vertex.ProjectID != "test-project" {
		t.Fatalf("ProjectID = %q, want test-project", vertex.ProjectID)
	}
	if vertex.Region != "global" {
		t.Fatalf("Region = %q, want global", vertex.Region)
	}
	if len(vertex.Scopes) != 1 || vertex.Scopes[0] != "https://www.googleapis.com/auth/cloud-platform" {
		t.Fatalf("Scopes = %#v, want cloud-platform scope", vertex.Scopes)
	}
}
