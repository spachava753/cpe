package acp

import (
	"regexp"
	"slices"
	"strings"

	"github.com/spachava753/acp-sdk/acp"

	"github.com/spachava753/cpe/internal/skills"
)

// skillSlashCommandReg matches the ACP prompt text form emitted by clients for
// skill slash commands, for example "/skill:domain-modeling". The skill name
// grammar mirrors internal/skills validation so malformed text is left alone.
var skillSlashCommandReg = regexp.MustCompile(`/skill:([a-z0-9]+(?:-[a-z0-9]+)*)`)

// availableSkillCommands converts the discovered skill catalog into ACP command
// metadata for session/update notifications.
//
// ACP clients use these commands only for autocomplete/typeahead; if selected,
// the client still sends plain prompt text such as "/skill:domain-modeling".
// Hidden skills are intentionally included here because a user may explicitly
// invoke them even when disable-model-invocation prevents default model
// visibility in the system prompt.
func availableSkillCommands(catalog skills.Catalog) []acp.AvailableCommand {
	commands := make([]acp.AvailableCommand, len(catalog.Skills))
	for i, skill := range catalog.Skills {
		input := acp.UnstructuredAvailableCommandInput(skill.Description)
		commands[i] = acp.AvailableCommand{
			Name:        "skill:" + skill.Name,
			Description: skill.Description,
			Input:       &input,
		}
	}
	return commands
}

// expandSkillSlashCommands rewrites known skill slash-command references in ACP
// text content blocks to the skill display path before the prompt is converted
// to a model message and persisted.
//
// For example, if the catalog contains a project skill named
// "domain-modeling", the text "Use /skill:domain-modeling" becomes
// "Use ./.agents/skills/domain-modeling". Unknown references, such as
// "/skill:missing", are left unchanged so ordinary user text is preserved. The
// input slice is cloned; non-text content blocks are copied without rewriting.
func expandSkillSlashCommands(blocks []acp.ContentBlock, catalog skills.Catalog) []acp.ContentBlock {
	byName := make(map[string]skills.Skill, len(catalog.Skills))
	for _, skill := range catalog.Skills {
		byName[skill.Name] = skill
	}

	expanded := slices.Clone(blocks)
	for i, block := range expanded {
		if block.Type != acp.ContentBlockTypeText {
			continue
		}
		expanded[i].Text = skillSlashCommandReg.ReplaceAllStringFunc(block.Text, func(match string) string {
			name := strings.TrimPrefix(match, "/skill:")
			skill, ok := byName[name]
			if !ok {
				return match
			}
			return skill.Path
		})
	}
	return expanded
}
