package skills

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/spf13/afero"
)

const recordFilename = "skills.json"

type installationRecords struct {
	Version int                    `json:"version"`
	Skills  map[string]SkillSource `json:"skills"`
}

func validDirectoryName(name string) bool {
	return name != "" && name != "." && name != ".." && utf8.ValidString(name) && !strings.ContainsAny(
		name,
		"/\\\x00:",
	) && filepath.Base(name) == name
}

func (m *Manager) destination(ctx context.Context, directory string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m == nil || m.destinations == nil {
		return errors.New("destination filesystem is unavailable")
	}
	if strings.TrimSpace(directory) == "" {
		return errors.New("destination directory is required")
	}
	return m.rejectDestinationLinks(directory)
}

// Inspect native components before cleaning them, so link/.. cannot hide a link.
func (m *Manager) rejectDestinationLinks(directory string) error {
	volume := filepath.VolumeName(directory)
	prefix := volume
	remaining := directory[len(volume):]
	if filepath.IsAbs(directory) {
		prefix += string(filepath.Separator)
	}
	unresolved := false
	for component := range strings.SplitSeq(
		remaining,
		string(filepath.Separator),
	) {
		if component == "" || component == "." {
			continue
		}
		if component == ".." && unresolved {
			return errors.New("destination parent traversal follows a missing directory component")
		}
		if prefix != "" && !strings.HasSuffix(
			prefix,
			string(filepath.Separator),
		) {
			prefix += string(filepath.Separator)
		}
		prefix += component
		info, err := m.pathInfo(prefix)
		if errors.Is(
			err,
			fs.ErrNotExist,
		) {
			unresolved = true
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf(
				"destination path %s is a symlink",
				prefix,
			)
		}
		if !info.IsDir() {
			return fmt.Errorf(
				"destination path %s is not a directory",
				prefix,
			)
		}
	}
	return nil
}

func (m *Manager) records(directory string) (installationRecords, error) {
	filename := filepath.Join(
		directory,
		recordFilename,
	)
	info, statErr := m.pathInfo(filename)
	if errors.Is(
		statErr,
		fs.ErrNotExist,
	) {
		return installationRecords{
			Version: 1,
			Skills:  map[string]SkillSource{},
		}, nil
	}
	if statErr != nil {
		return installationRecords{}, statErr
	}
	if !info.Mode().IsRegular() {
		return installationRecords{}, fmt.Errorf(
			"%s must be a regular metadata file",
			filename,
		)
	}
	data, err := afero.ReadFile(
		m.destinations,
		filename,
	)
	if errors.Is(
		err,
		fs.ErrNotExist,
	) {
		return installationRecords{
			Version: 1,
			Skills:  map[string]SkillSource{},
		}, nil
	}
	if err != nil {
		return installationRecords{}, fmt.Errorf(
			"read %s: %w",
			filename,
			err,
		)
	}
	var records installationRecords
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&records); err != nil {
		return installationRecords{}, fmt.Errorf(
			"decode %s: %w",
			filename,
			err,
		)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return installationRecords{}, fmt.Errorf(
			"%s must contain one JSON object",
			filename,
		)
	}
	if records.Version != 1 {
		return installationRecords{}, fmt.Errorf(
			"%s: unsupported version %d",
			filename,
			records.Version,
		)
	}
	if records.Skills == nil {
		return installationRecords{}, fmt.Errorf(
			"%s: missing skills records",
			filename,
		)
	}
	for name, source := range records.Skills {
		if !validDirectoryName(name) || strings.TrimSpace(source.Location) == "" || !fs.ValidPath(source.Directory) || strings.ContainsAny(
			source.Directory,
			"\\\x00",
		) {
			return installationRecords{}, fmt.Errorf(
				"%s: invalid record %q",
				filename,
				name,
			)
		}
	}
	return records, nil
}

func selectedNames(names []string) ([]string, error) {
	seen := map[string]bool{}
	result := make(
		[]string,
		0,
		len(names),
	)
	for _, name := range names {
		if !validDirectoryName(name) {
			return nil, fmt.Errorf(
				"invalid skill directory name %q",
				name,
			)
		}
		if !seen[name] {
			seen[name] = true
			result = append(
				result,
				name,
			)
		}
	}
	return result, nil
}
