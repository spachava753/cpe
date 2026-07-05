package acp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// createACPSkill writes a minimal skill fixture under baseDir/folder for ACP
// integration tests. The metadata map becomes the SKILL.md YAML frontmatter.
func createACPSkill(t *testing.T, baseDir, folder string, metadata map[string]any) {
	t.Helper()

	skillDir := filepath.Join(baseDir, folder)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("os.MkdirAll() error = %v", err)
	}

	var frontmatter strings.Builder
	for _, key := range []string{"name", "description", "disable-model-invocation", "group"} {
		value, ok := metadata[key]
		if !ok {
			continue
		}
		fmt.Fprintf(&frontmatter, "%s: %v\n", key, value)
	}
	content := fmt.Sprintf("---\n%s---\n\n# %s\n", frontmatter.String(), metadata["name"])
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
}
