package skills

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/bundlefs"
	"github.com/spachava753/cpe/internal/source"
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
	fields map[string]json.RawMessage
	skills map[string]json.RawMessage
	file   LockFile
	dirty  bool
}

type skillSourceType string

const (
	githubSource skillSourceType = "github"
	gitSource    skillSourceType = "git"
)

type lockedSkill struct {
	Source          string          `json:"source"`
	SourceType      skillSourceType `json:"sourceType"`
	SourceURL       string          `json:"sourceUrl,omitempty"`
	Ref             string          `json:"ref,omitempty"`
	SkillPath       string          `json:"skillPath"`
	ComputedHash    string          `json:"computedHash,omitempty"`
	SkillFolderHash string          `json:"skillFolderHash,omitempty"`
	InstalledAt     string          `json:"installedAt,omitempty"`
	UpdatedAt       string          `json:"updatedAt,omitempty"`
}

func (m *Manager) readLock(ctx context.Context, file LockFile) (*skillLock, error) {
	var version int
	switch file.Format {
	case ProjectLock:
		version = 1
	case GlobalLock:
		version = 3
	default:
		return nil, errors.New("unsupported lock format")
	}
	if strings.TrimSpace(file.Path) == "" {
		return nil, errors.New("lock file path is required")
	}
	var filesystem afero.Fs
	if m != nil {
		filesystem = m.destinations
	}
	lock := &skillLock{
		file:   file,
		fields: map[string]json.RawMessage{},
		skills: map[string]json.RawMessage{},
	}
	data, err := bundlefs.ReadFile(
		ctx,
		filesystem,
		file.Path,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"read lock %s: %w",
			file.Path,
			err,
		)
	}
	if data == nil {
		lock.fields["version"] = json.RawMessage(fmt.Sprint(version))
		return lock, nil
	}
	if err := json.Unmarshal(
		data,
		&lock.fields,
	); err != nil || lock.fields == nil {
		return nil, fmt.Errorf(
			"invalid lock file %s",
			file.Path,
		)
	}
	var actualVersion int
	if err := json.Unmarshal(
		lock.fields["version"],
		&actualVersion,
	); err != nil || actualVersion != version {
		return nil, fmt.Errorf(
			"lock file %s requires version %d",
			file.Path,
			version,
		)
	}
	if err := json.Unmarshal(
		lock.fields["skills"],
		&lock.skills,
	); err != nil || lock.skills == nil {
		return nil, fmt.Errorf(
			"invalid skills in lock file %s",
			file.Path,
		)
	}
	return lock, nil
}

func (lock *skillLock) record(name string) (lockedSkill, source.GitLocator, error) {
	var record lockedSkill
	var locator source.GitLocator
	if err := json.Unmarshal(
		lock.skills[name],
		&record,
	); err != nil {
		return record, locator, errors.New("invalid lock record")
	}
	if record.SourceType != githubSource && record.SourceType != gitSource {
		return record, locator, fmt.Errorf(
			"unsupported source type %q",
			record.SourceType,
		)
	}
	if record.Ref != "" {
		return record, locator, fmt.Errorf(
			"unsupported recorded ref %q",
			record.Ref,
		)
	}
	if strings.TrimSpace(record.Source) == "" {
		return record, locator, errors.New("lock record has no source")
	}
	if !fs.ValidPath(record.SkillPath) || strings.ContainsAny(
		record.SkillPath,
		"\\\x00",
	) || path.Base(record.SkillPath) != skillDefinition {
		return record, locator, errors.New("lock record has no valid skillPath")
	}
	if record.SourceType == gitSource && record.SourceURL == "" && !strings.Contains(
		record.Source,
		"://",
	) {
		return record, locator, errors.New("generic Git source has no unambiguous sourceUrl")
	}
	locator, err := source.ParseGitLocator(record.locator())
	if err != nil {
		return record, locator, fmt.Errorf(
			"unsupported recorded source: %w",
			err,
		)
	}
	return record, locator, nil
}

func (record lockedSkill) locator() string {
	if record.SourceURL != "" {
		return record.SourceURL
	}
	return record.Source
}

func sourceRecord(locator string) lockedSkill {
	record := lockedSkill{
		Source:     locator,
		SourceType: gitSource,
		SourceURL:  locator,
	}
	if !strings.Contains(
		locator,
		"://",
	) {
		record.SourceType = githubSource
		record.Source = strings.TrimSuffix(
			locator,
			".git",
		)
		record.SourceURL = "https://github.com/" + record.Source
		return record
	}
	parsed, err := url.Parse(locator)
	if err == nil && strings.EqualFold(
		parsed.Hostname(),
		"github.com",
	) {
		parts := strings.Split(
			strings.Trim(
				parsed.Path,
				"/",
			),
			"/",
		)
		if len(parts) == 2 {
			record.SourceType = githubSource
			record.Source = strings.TrimSuffix(
				strings.Join(
					parts,
					"/",
				),
				".git",
			)
		}
	}
	return record
}

func (lock *skillLock) put(name string, record lockedSkill) error {
	fields := map[string]json.RawMessage{}
	if existing := lock.skills[name]; existing != nil {
		if err := json.Unmarshal(
			existing,
			&fields,
		); err != nil || fields == nil {
			return fmt.Errorf(
				"invalid lock record for %q",
				name,
			)
		}
	}
	if lock.file.Format == GlobalLock {
		now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
		record.UpdatedAt = now
		if previous := fields["installedAt"]; previous != nil {
			if err := json.Unmarshal(
				previous,
				&record.InstalledAt,
			); err != nil {
				return fmt.Errorf(
					"invalid installedAt for %q",
					name,
				)
			}
		}
		if record.InstalledAt == "" {
			record.InstalledAt = now
		}
		record.ComputedHash = ""
	} else {
		record.SkillFolderHash = ""
		record.InstalledAt = ""
		record.UpdatedAt = ""
		if record.SourceType == githubSource && record.SourceURL == "https://github.com/"+record.Source {
			record.SourceURL = ""
		}
	}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	// Replace known provenance fields; retain unrelated tool metadata.
	for _, key := range []string{
		"source",
		"sourceType",
		"sourceUrl",
		"ref",
		"skillPath",
		"computedHash",
		"skillFolderHash",
		"installedAt",
		"updatedAt",
	} {
		delete(
			fields,
			key,
		)
	}
	var updated map[string]json.RawMessage
	if decodeErr := json.Unmarshal(
		data,
		&updated,
	); decodeErr != nil {
		return decodeErr
	}
	maps.Copy(
		fields,
		updated,
	)
	lock.skills[name], err = json.Marshal(fields)
	if err == nil {
		lock.dirty = true
	}
	return err
}

func (lock *skillLock) encode() ([]byte, error) {
	data, err := json.Marshal(lock.skills)
	if err != nil {
		return nil, err
	}
	lock.fields["skills"] = data
	data, err = json.MarshalIndent(
		lock.fields,
		"",
		"  ",
	)
	if err != nil {
		return nil, err
	}
	return append(
		data,
		'\n',
	), nil
}

func (m *Manager) applyTracked(ctx context.Context, directory string, changes []bundlefs.Change, lock *skillLock, checkExisting func(string) error) (bundlefs.Outcome, error) {
	if lock == nil {
		return bundlefs.Apply(
			ctx,
			m.destinations,
			directory,
			changes,
			checkExisting,
		)
	}
	var data []byte
	if lock.dirty {
		var err error
		data, err = lock.encode()
		if err != nil {
			return bundlefs.NotCommitted, err
		}
	}
	return bundlefs.ApplyWithFile(
		ctx,
		m.destinations,
		directory,
		changes,
		checkExisting,
		bundlefs.FileChange{
			Path: lock.file.Path,
			Data: data,
		},
	)
}
