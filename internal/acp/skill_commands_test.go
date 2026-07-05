package acp

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/spachava753/acp-sdk/acp"

	"github.com/spachava753/cpe/internal/skills"
)

func TestAvailableSkillCommands(t *testing.T) {
	catalog := skills.Catalog{Skills: []skills.Skill{
		{Name: "visible", Description: "Visible description"},
		{Name: "hidden", Description: "Hidden description", DisableModelInvocation: true},
	}}

	visibleInput := acp.UnstructuredAvailableCommandInput("Visible description")
	hiddenInput := acp.UnstructuredAvailableCommandInput("Hidden description")
	want := []acp.AvailableCommand{
		{Name: "skill:visible", Description: "Visible description", Input: &visibleInput},
		{Name: "skill:hidden", Description: "Hidden description", Input: &hiddenInput},
	}
	got := availableSkillCommands(catalog)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("availableSkillCommands() mismatch\nwant: %#v\n got: %#v", want, got)
	}
}

func TestExpandSkillSlashCommands(t *testing.T) {
	catalog := skills.Catalog{Skills: []skills.Skill{
		{Name: "domain-modeling", Path: filepath.Join("~", ".agents", "skills", "domain-modeling")},
	}}
	blocks := []acp.ContentBlock{
		acp.TextContentBlock("Use /skill:domain-modeling and leave /skill:missing alone"),
		acp.ImageContentBlock("image-data", "image/png"),
	}

	got := expandSkillSlashCommands(blocks, catalog)
	want := []acp.ContentBlock{
		acp.TextContentBlock("Use " + filepath.Join("~", ".agents", "skills", "domain-modeling") + " and leave /skill:missing alone"),
		acp.ImageContentBlock("image-data", "image/png"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expandSkillSlashCommands() mismatch\nwant: %#v\n got: %#v", want, got)
	}
	if blocks[0].Text != "Use /skill:domain-modeling and leave /skill:missing alone" {
		t.Fatalf("expandSkillSlashCommands() mutated input: %#v", blocks)
	}
}
