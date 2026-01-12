package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/invopop/jsonschema"

	"github.com/spachava753/cpe/internal/config"
)

func main() {
	if err := generateSchema(); err != nil {
		fmt.Fprintf(os.Stderr, "Error generating schema: %v\n", err)
		os.Exit(1)
	}
}

func generateSchema() error {
	reflector := &jsonschema.Reflector{
		AllowAdditionalProperties:  false,
		RequiredFromJSONSchemaTags: true,
	}

	schema := reflector.Reflect(&config.RawConfig{})
	schema.Title = "CPE Configuration Schema"
	schema.Description = "JSON Schema for CPE (Chat-based Programming Editor) configuration files"
	schema.Version = "https://json-schema.org/draft/2020-12/schema"
	schema.ID = "https://raw.githubusercontent.com/spachava753/cpe/refs/heads/main/schema/cpe-config-schema.json"

	schemaJSON, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal schema: %w", err)
	}

	// Get the GOMOD from environment to find the module root
	gomod := os.Getenv("GOMOD")
	var moduleRoot string
	if gomod != "" {
		moduleRoot = filepath.Dir(gomod)
	} else {
		// Fallback: start from current directory and traverse up
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get working directory: %w", err)
		}
		moduleRoot = findModuleRoot(wd)
	}

	schemaPath := filepath.Join(moduleRoot, "schema", "cpe-config-schema.json")
	if err := os.MkdirAll(filepath.Dir(schemaPath), 0755); err != nil {
		return fmt.Errorf("failed to create schema directory: %w", err)
	}

	if err := os.WriteFile(schemaPath, schemaJSON, 0644); err != nil {
		return fmt.Errorf("failed to write schema file: %w", err)
	}

	fmt.Printf("Generated schema: %s\n", schemaPath)
	return nil
}

func findModuleRoot(start string) string {
	current := start
	for {
		if _, err := os.Stat(filepath.Join(current, "go.mod")); err == nil {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return current
		}
		current = parent
	}
}
