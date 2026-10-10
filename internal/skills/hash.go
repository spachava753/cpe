package skills

import (
	"bytes"
	"context"
	"crypto/sha1" // Git object IDs use SHA-1, not a security checksum.
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"

	"github.com/spachava753/cpe/internal/bundlefs"
)

func folderHash(payload []bundlefs.Entry) string {
	files := make(
		[]bundlefs.Entry,
		0,
		len(payload),
	)
	for _, entry := range payload {
		if !entry.Mode.IsDir() {
			files = append(
				files,
				entry,
			)
		}
	}
	// Upstream sorts with JavaScript localeCompare, rather than byte ordering.
	order := collate.New(language.English)
	slices.SortFunc(
		files,
		func(a, b bundlefs.Entry) int {
			return order.CompareString(
				a.Path,
				b.Path,
			)
		},
	)
	hash := sha256.New()
	for _, file := range files {
		_, _ = hash.Write([]byte(file.Path))
		_, _ = hash.Write(file.Data)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func gitTreeHash(ctx context.Context, filesystem fs.FS, directory string) (string, error) {
	if err := bundlefs.CheckSourceDirectory(
		filesystem,
		directory,
	); err != nil {
		return "", err
	}
	hash, _, err := hashGitDirectory(
		ctx,
		filesystem,
		directory,
	)
	return hex.EncodeToString(hash), err
}

type gitTreeEntry struct {
	name string
	mode string
	hash []byte
}

func hashGitDirectory(ctx context.Context, filesystem fs.FS, directory string) ([]byte, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	entries, err := fs.ReadDir(
		filesystem,
		directory,
	)
	if err != nil {
		return nil, false, err
	}
	var tree []gitTreeEntry
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		if entry.Name() == ".git" && entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, false, err
		}
		filename := path.Join(
			directory,
			entry.Name(),
		)
		item := gitTreeEntry{
			name: entry.Name(),
			mode: "100644",
		}
		if info.IsDir() {
			var nonempty bool
			item.hash, nonempty, err = hashGitDirectory(
				ctx,
				filesystem,
				filename,
			)
			if err != nil {
				return nil, false, err
			}
			if !nonempty {
				continue
			}
			item.mode = "40000"
		} else {
			if !info.Mode().IsRegular() {
				return nil, false, fmt.Errorf(
					"git tree file %s is not regular",
					filename,
				)
			}
			data, err := fs.ReadFile(
				filesystem,
				filename,
			)
			if err != nil {
				return nil, false, err
			}
			item.hash = gitObjectHash(
				"blob",
				data,
			)
			if info.Mode().Perm()&0o111 != 0 {
				item.mode = "100755"
			}
		}
		tree = append(
			tree,
			item,
		)
	}
	slices.SortFunc(
		tree,
		func(a, b gitTreeEntry) int {
			left, right := a.name, b.name
			if a.mode == "40000" {
				left += "/"
			}
			if b.mode == "40000" {
				right += "/"
			}
			return strings.Compare(
				left,
				right,
			)
		},
	)
	var data bytes.Buffer
	for _, entry := range tree {
		data.WriteString(entry.mode + " " + entry.name)
		data.WriteByte(0)
		data.Write(entry.hash)
	}
	return gitObjectHash(
		"tree",
		data.Bytes(),
	), len(tree) > 0, nil
}

func gitObjectHash(kind string, data []byte) []byte {
	hash := sha1.New() //nolint:gosec // Git's tree/blob object format requires SHA-1.
	_, _ = fmt.Fprintf(
		hash,
		"%s %d\x00",
		kind,
		len(data),
	)
	_, _ = hash.Write(data)
	return hash.Sum(nil)
}

func (lock *skillLock) hashRecord(ctx context.Context, filesystem fs.FS, skill Skill, payload []bundlefs.Entry, record lockedSkill) (lockedSkill, error) {
	record.SkillPath = skill.Path
	if lock.file.Format == GlobalLock && record.SourceType == githubSource {
		var err error
		record.SkillFolderHash, err = gitTreeHash(
			ctx,
			filesystem,
			sourceDirectory(skill),
		)
		return record, err
	}
	hash := folderHash(payload)
	if lock.file.Format == GlobalLock {
		record.SkillFolderHash = hash
	} else {
		record.ComputedHash = hash
	}
	return record, nil
}
