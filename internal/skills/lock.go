package skills

import (
	"context"
	"encoding/json"

	"github.com/spachava753/cpe/internal/bundlefs"
)

// LockFormat identifies a supported skill lock file schema.
type LockFormat uint8

const (
	// ProjectLock uses the version 1 skills-lock.json schema.
	ProjectLock LockFormat = iota + 1
	// GlobalLock uses the version 3 .skill-lock.json schema.
	GlobalLock
)

// LockFile selects a tracking file and its schema. Path uses native filesystem syntax.
type LockFile struct {
	Path   string
	Format LockFormat
}

// Keep records opaque until an update selects them. Unrelated entries and tool
// preferences survive writes, including source types that CPE cannot acquire.
type skillLock struct {
	file   LockFile
	fields map[string]json.RawMessage
	skills map[string]json.RawMessage
}

type lockedSkill struct {
	Source          string `json:"source"`
	SourceType      string `json:"sourceType"`
	SourceURL       string `json:"sourceUrl,omitempty"`
	Ref             string `json:"ref,omitempty"`
	SkillPath       string `json:"skillPath"`
	ComputedHash    string `json:"computedHash,omitempty"`
	SkillFolderHash string `json:"skillFolderHash,omitempty"`
	InstalledAt     string `json:"installedAt,omitempty"`
	UpdatedAt       string `json:"updatedAt,omitempty"`
}

func (m *Manager) readLock(ctx context.Context, file LockFile) (*skillLock, error) {
	panic("not implemented")
}

func (lock *skillLock) record(name string) (lockedSkill, error) {
	panic("not implemented")
}

func (lock *skillLock) encode() ([]byte, error) {
	panic("not implemented")
}

func (m *Manager) applyTracked(ctx context.Context, directory string, changes []bundlefs.Change, lock *skillLock) (bundlefs.Outcome, error) {
	panic("not implemented")
}
