package agent

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"os"
	"path/filepath"

	"github.com/spachava753/gai"
)

// saveMedia writes media to the session's temporary directory under a name
// derived from its content, so saving the same media again reuses the file.
func (h *turnHooks) saveMedia(block gai.Block) (string, int, error) {
	data, err := base64.StdEncoding.DecodeString(block.Content.String())
	if err != nil {
		return "", 0, err
	}
	extension := ".bin"
	if extensions, _ := mime.ExtensionsByType(block.MimeType); len(extensions) != 0 {
		extension = extensions[0]
	}
	sum := sha256.Sum256(data)
	path := filepath.Join(
		h.session.tempDir,
		"media-"+hex.EncodeToString(sum[:8])+extension,
	)
	if _, statErr := os.Stat(path); statErr == nil {
		return path, len(data), nil
	}
	file, err := os.CreateTemp(
		h.session.tempDir,
		".media-*",
	)
	if err != nil {
		return "", 0, err
	}
	_, err = file.Write(data)
	err = errors.Join(
		err,
		file.Close(),
	)
	if err == nil {
		err = os.Rename(
			file.Name(),
			path,
		)
	}
	if err != nil {
		_ = os.Remove(file.Name())
		return "", 0, err
	}
	return path, len(data), nil
}

// unsupportedMediaNotice replaces tool media the selected model cannot accept.
// The media stays in history for models that can, and is saved to a file so
// this model can still convert or inspect it.
func (h *turnHooks) unsupportedMediaNotice(block gai.Block) string {
	path, size, err := h.saveMedia(block)
	if err != nil {
		return fmt.Sprintf(
			"[This %s media is retained in history, but the selected model does not accept %s input. The media could not be saved: %v]",
			block.MimeType,
			block.ModalityType,
			err,
		)
	}
	return fmt.Sprintf(
		"[This %s media (%d bytes) is retained in history, but the selected model does not accept %s input. The media was saved to %s; convert it, for example to text, to use it.]",
		block.MimeType,
		size,
		block.ModalityType,
		path,
	)
}
