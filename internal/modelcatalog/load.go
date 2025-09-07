package modelcatalog

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func Load(path string) ([]Model, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read model catalog: %w", err)
	}
	var models []Model
	if err := json.Unmarshal(b, &models); err != nil {
		return nil, fmt.Errorf("parse model catalog JSON: %w", err)
	}
	if err := Validate(models); err != nil {
		return nil, err
	}
	return models, nil
}

func Validate(models []Model) error {
	seen := map[string]struct{}{}
	for i, m := range models {
		if strings.TrimSpace(m.Name) == "" {
			return fmt.Errorf("model[%d]: name is required", i)
		}
		if strings.TrimSpace(m.ID) == "" {
			return fmt.Errorf("model[%d]: id is required", i)
		}
		t := strings.ToLower(strings.TrimSpace(m.Type))
		switch t {
		case "openai", "anthropic", "gemini", "cerebras", "responses":
		default:
			return fmt.Errorf("model[%d] %q: invalid type %q (must be openai|anthropic|gemini|cerebras|responses)", i, m.Name, m.Type)
		}
		if _, ok := seen[m.Name]; ok {
			return fmt.Errorf("duplicate model name: %s", m.Name)
		}
		seen[m.Name] = struct{}{}
		if !m.SupportsReasoning && strings.TrimSpace(m.DefaultReasoningEffort) != "" {
			// ignore silently by zeroing to keep consistency
			models[i].DefaultReasoningEffort = ""
		}
	}
	return nil
}
