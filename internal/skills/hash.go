package skills

import (
	"cmp"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"slices"

	"github.com/spachava753/cpe/internal/bundlefs"
)

// folderHash fingerprints file paths and contents, independent of traversal order.
// Paths use byte ordering; lengths distinguish file names, contents, and boundaries.
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
	slices.SortFunc(
		files,
		func(a, b bundlefs.Entry) int {
			return cmp.Compare(
				a.Path,
				b.Path,
			)
		},
	)
	hash := sha256.New()
	var length [8]byte
	for _, file := range files {
		binary.BigEndian.PutUint64(
			length[:],
			uint64(len(file.Path)),
		)
		_, _ = hash.Write(length[:])
		_, _ = hash.Write([]byte(file.Path))
		binary.BigEndian.PutUint64(
			length[:],
			uint64(len(file.Data)),
		)
		_, _ = hash.Write(length[:])
		_, _ = hash.Write(file.Data)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func (lock *skillLock) hashRecord(skill Skill, payload []bundlefs.Entry, record lockedSkill) lockedSkill {
	record.SkillPath = skill.Path
	hash := folderHash(payload)
	if lock.file.Format == GlobalLock {
		record.SkillFolderHash = hash
	} else {
		record.ComputedHash = hash
	}
	return record
}

func (lock *skillLock) recordedHash(record lockedSkill) string {
	if lock.file.Format == GlobalLock {
		return record.SkillFolderHash
	}
	return record.ComputedHash
}
