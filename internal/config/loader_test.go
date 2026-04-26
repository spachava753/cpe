package config

import "testing"

func TestParseConfigDataRejectsJSON(t *testing.T) {
	_, err := parseConfigData([]byte(`{"models":[]}`), "cpe.json")
	if err == nil {
		t.Fatal("expected JSON config error")
	}
	want := "JSON config files are no longer supported; use YAML (.yaml or .yml)"
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

func TestConfigWritePathRejectsJSON(t *testing.T) {
	if err := ValidateConfigPathForWrite("cpe.json"); err == nil {
		t.Fatal("expected JSON write path error")
	}
	if _, err := LoadOrCreateRawConfig("cpe.json"); err == nil {
		t.Fatal("expected LoadOrCreateRawConfig JSON path error")
	}
	if err := WriteRawConfig("cpe.json", &RawConfig{}); err == nil {
		t.Fatal("expected WriteRawConfig JSON path error")
	}
}
