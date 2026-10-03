package skills

import (
	"context"
	"fmt"

	"github.com/spf13/afero"
)

// Manager owns skill discovery, destination files, and skills.json bookkeeping.
// It acquires source filesystems through its source client and returns results
// without prompting or writing command output. Its operations are still stubs.
type Manager struct {
	sources      SourceClient //nolint:unused // Reserved for the signature-only operation stubs.
	destinations afero.Fs     //nolint:unused // Reserved for the signature-only operation stubs.
}

// NewManager supplies source acquisition and destination filesystem boundaries.
// All destination files and skills.json records belong to destinations; Manager
// does not choose a disk filesystem itself. A nil source client leaves source
// acquisition unavailable; it is not needed for installed listing or removal.
func NewManager(sources SourceClient, destinations afero.Fs) *Manager {
	return &Manager{sources: sources, destinations: destinations}
}

// DiscoveryOptions controls source discovery without command presentation flags.
type DiscoveryOptions struct {
	FullDepth bool
}

// SkillSource identifies a bundle in its original source. Location is the source
// locator; Directory is relative to the source filesystem, not a checkout path.
type SkillSource struct {
	Location  string
	Directory string
}

// InstalledSkill associates a definition with its destination bundle directory.
// A nil Source denotes an unmanaged installation.
type InstalledSkill struct {
	Skill     Skill
	Directory string
	Source    *SkillSource
}

// InstallRequest selects a source's skills and one exact destination root.
// An empty Skills slice selects all discovered skills. Directory is the skill
// root, not a project directory; its metadata file is Directory/skills.json.
type InstallRequest struct {
	Source    string
	Directory string
	Skills    []string
	Discovery DiscoveryOptions
}

// RemoveRequest selects installed directory names in one exact skill root.
// Removal does not require installation records or source acquisition.
type RemoveRequest struct {
	Directory string
	Skills    []string
}

// UpdateRequest selects managed installations in one exact skill root.
// An empty Skills slice selects all managed installations in that root.
type UpdateRequest struct {
	Directory string
	Skills    []string
}

// ListSource will acquire a source filesystem and discover available skills.
// Skill.Path in each result is source-relative and uses io/fs path conventions.
func (m *Manager) ListSource(ctx context.Context, source string, opts DiscoveryOptions) ([]Skill, error) {
	return nil, fmt.Errorf("list source skills: %w", ErrNotImplemented)
}

// Install will acquire a source filesystem, discover and copy selected bundles, and
// record their provenance. It returns installations without formatting output.
func (m *Manager) Install(ctx context.Context, request InstallRequest) ([]InstalledSkill, error) {
	return nil, fmt.Errorf("manager install skills: %w", ErrNotImplemented)
}

// ListInstalled will inspect one exact skill root, including unmanaged skills,
// without acquiring a source filesystem.
func (m *Manager) ListInstalled(ctx context.Context, directory string) ([]InstalledSkill, error) {
	return nil, fmt.Errorf("list installed skills: %w", ErrNotImplemented)
}

// Remove will delete named installations and their records when present. The
// result contains destination bundle paths, not source-filesystem paths.
func (m *Manager) Remove(ctx context.Context, request RemoveRequest) ([]string, error) {
	return nil, fmt.Errorf("manager remove skills: %w", ErrNotImplemented)
}

// Update will acquire the recorded sources of managed installations and update
// their bundles and bookkeeping. Unmanaged installations are not selected.
func (m *Manager) Update(ctx context.Context, request UpdateRequest) ([]InstalledSkill, error) {
	return nil, fmt.Errorf("manager update skills: %w", ErrNotImplemented)
}
