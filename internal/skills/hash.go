package skills

import (
	"context"
	"io/fs"

	"github.com/spachava753/cpe/internal/bundlefs"
)

func folderHash(payload []bundlefs.Entry) string {
	panic("not implemented")
}

func gitTreeHash(ctx context.Context, filesystem fs.FS, directory string) (string, error) {
	panic("not implemented")
}
