package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"text/template"

	"github.com/google/jsonschema-go/jsonschema"
)

const (
	compactionTemplateName           = "compaction initial message template"
	defaultMaxAutoCompactionRestarts = 3
)

// CompactionConfig controls manual and threshold-driven conversation compaction.
type CompactionConfig struct {
	Enabled                   bool    `yaml:"enabled" json:"enabled"`
	AutoTriggerThreshold      float64 `yaml:"autoTriggerThreshold,omitempty" json:"autoTriggerThreshold,omitempty"`
	MaxAutoCompactionRestarts int     `yaml:"maxAutoCompactionRestarts,omitempty" json:"maxAutoCompactionRestarts,omitempty"`
	ToolDescription           string  `yaml:"toolDescription,omitempty" json:"toolDescription,omitempty"`
	InputSchema               any     `yaml:"inputSchema,omitempty" json:"inputSchema,omitempty" jsonschema:"oneof_type=object;boolean"`
	InitialMessageTemplate    string  `yaml:"initialMessageTemplate,omitempty" json:"initialMessageTemplate,omitempty"`
}

// CompactionTemplateData is the template input used to create a compacted branch root.
type CompactionTemplateData struct {
	OriginalUserMessage string
	ToolInput           map[string]any
}

// ResolvedCompactionConfig is the runtime-ready compaction configuration.
type ResolvedCompactionConfig struct {
	Enabled                   bool
	AutoTriggerThreshold      float64
	MaxAutoCompactionRestarts int
	ToolDescription           string
	InputSchema               *jsonschema.Schema
	InitialMessageTemplate    string

	initialMessageTemplate *template.Template
}

// RenderInitialMessage renders the compacted branch root message template.
func (c *ResolvedCompactionConfig) RenderInitialMessage(data CompactionTemplateData) (string, error) {
	if c == nil || c.initialMessageTemplate == nil {
		return "", fmt.Errorf("compaction initial message template is not configured")
	}
	if data.ToolInput == nil {
		data.ToolInput = map[string]any{}
	}

	var buf bytes.Buffer
	if err := c.initialMessageTemplate.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("rendering compaction initial message template: %w", err)
	}
	return buf.String(), nil
}

func resolveCompaction(model ModelConfig, defaults Defaults) (*ResolvedCompactionConfig, error) {
	if model.Compaction != nil {
		return compileCompactionConfig(model.Compaction)
	}
	return compileCompactionConfig(defaults.Compaction)
}

func validateCompactionConfig(compaction *CompactionConfig, fieldPrefix string) error {
	_, err := compileCompactionConfig(compaction)
	if err != nil {
		return fmt.Errorf("%s: %w", fieldPrefix, err)
	}
	return nil
}

func compileCompactionConfig(compaction *CompactionConfig) (*ResolvedCompactionConfig, error) {
	if compaction == nil {
		return nil, nil
	}

	if compaction.Enabled {
		if compaction.AutoTriggerThreshold <= 0 || compaction.AutoTriggerThreshold > 1 {
			return nil, fmt.Errorf("autoTriggerThreshold must be > 0 and <= 1")
		}
		if compaction.ToolDescription == "" {
			return nil, fmt.Errorf("toolDescription must not be empty when compaction is enabled")
		}
		if compaction.InitialMessageTemplate == "" {
			return nil, fmt.Errorf("initialMessageTemplate must not be empty when compaction is enabled")
		}
	} else if compaction.AutoTriggerThreshold != 0 && (compaction.AutoTriggerThreshold <= 0 || compaction.AutoTriggerThreshold > 1) {
		return nil, fmt.Errorf("autoTriggerThreshold must be > 0 and <= 1")
	}
	if compaction.MaxAutoCompactionRestarts < 0 {
		return nil, fmt.Errorf("maxAutoCompactionRestarts must be >= 1 when set")
	}
	maxAutoCompactionRestarts := compaction.MaxAutoCompactionRestarts
	if maxAutoCompactionRestarts == 0 {
		maxAutoCompactionRestarts = defaultMaxAutoCompactionRestarts
	}

	inputSchema, err := parseCompactionInputSchema(compaction.InputSchema, compaction.Enabled)
	if err != nil {
		return nil, fmt.Errorf("inputSchema: %w", err)
	}

	parsedTemplate, err := parseCompactionTemplate(compaction.InitialMessageTemplate)
	if err != nil {
		return nil, fmt.Errorf("initialMessageTemplate: %w", err)
	}

	return &ResolvedCompactionConfig{
		Enabled:                   compaction.Enabled,
		AutoTriggerThreshold:      compaction.AutoTriggerThreshold,
		MaxAutoCompactionRestarts: maxAutoCompactionRestarts,
		ToolDescription:           compaction.ToolDescription,
		InputSchema:               inputSchema,
		InitialMessageTemplate:    compaction.InitialMessageTemplate,
		initialMessageTemplate:    parsedTemplate,
	}, nil
}

func parseCompactionInputSchema(rawSchema any, enabled bool) (*jsonschema.Schema, error) {
	if rawSchema == nil {
		if !enabled {
			return nil, nil
		}
		return &jsonschema.Schema{
			Type:                 "object",
			AdditionalProperties: &jsonschema.Schema{Not: &jsonschema.Schema{}},
		}, nil
	}
	if schemaBool, ok := rawSchema.(bool); ok {
		if !schemaBool {
			if enabled {
				return nil, fmt.Errorf("enabled compaction cannot use a false boolean schema")
			}
			return &jsonschema.Schema{Not: &jsonschema.Schema{}}, nil
		}
		return &jsonschema.Schema{}, nil
	}

	schemaJSON, err := json.Marshal(rawSchema)
	if err != nil {
		return nil, fmt.Errorf("marshaling input schema: %w", err)
	}

	var schema *jsonschema.Schema
	if err := json.Unmarshal(schemaJSON, &schema); err != nil {
		return nil, fmt.Errorf("unmarshaling input schema: %w", err)
	}
	if schema == nil && enabled {
		return &jsonschema.Schema{
			Type:                 "object",
			AdditionalProperties: &jsonschema.Schema{Not: &jsonschema.Schema{}},
		}, nil
	}
	return schema, nil
}

func parseCompactionTemplate(raw string) (*template.Template, error) {
	if raw == "" {
		return nil, nil
	}
	parsed, err := template.New(compactionTemplateName).Option("missingkey=error").Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parsing template: %w", err)
	}
	return parsed, nil
}
