package skills

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"

	"github.com/spachava753/cpe/internal/bundlefs"
	"github.com/spachava753/cpe/internal/pathcollate"
	"github.com/spachava753/cpe/internal/source"
)

func folderHash(payload []bundlefs.Entry) string {
	type sortableFile struct {
		bundlefs.Entry
		key []byte
	}
	files := make(
		[]sortableFile,
		0,
		len(payload),
	)
	for _, entry := range payload {
		if !entry.Mode.IsDir() {
			files = append(
				files,
				sortableFile{
					Entry: entry,
					key:   pathcollate.Key(entry.Path),
				},
			)
		}
	}
	// Upstream's localeCompare sort is stable, including canonical-equivalence ties.
	slices.SortStableFunc(
		files,
		func(a, b sortableFile) int {
			return bytes.Compare(
				a.key,
				b.key,
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

func (lock *skillLock) hashRecord(ctx context.Context, checkout source.Checkout, skill Skill, payload []bundlefs.Entry, record lockedSkill) (lockedSkill, error) {
	record.SkillPath = skill.Path
	if lock.file.Format == GlobalLock && record.SourceType == githubSource {
		if checkout.GitTreeHash == nil {
			return record, errors.New("source Git tree metadata is unavailable")
		}
		var err error
		record.SkillFolderHash, err = checkout.GitTreeHash(
			ctx,
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
