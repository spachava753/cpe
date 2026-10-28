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
	"slices"

	"github.com/spachava753/gai"
)

// mediaKey identifies one block of one tool result.
type mediaKey string

func newMediaKey(callID string, index int) mediaKey {
	return mediaKey(fmt.Sprintf(
		"%s\x00%d",
		callID,
		index,
	))
}

// mediaRejection reports whether a provider error could have been caused by
// media. gai reports media an adapter cannot represent precisely; provider
// rejections only arrive as invalid or oversized requests.
func mediaRejection(err error) (string, bool) {
	if modality, ok := errors.AsType[gai.UnsupportedInputModalityErr](err); ok {
		return fmt.Sprintf(
			"the selected model's provider does not accept %s input",
			string(modality),
		), true
	}
	if api, ok := errors.AsType[*gai.ApiErr](err); ok && (api.Kind == gai.APIErrorKindInvalidRequest || api.Kind == gai.APIErrorKindRequestTooLarge) {
		message := api.Message
		if message == "" {
			message = api.Error()
		}
		return message, true
	}
	return "", false
}

// rejectMedia replaces media in the tool results after the last assistant
// message with notices naming files that hold the media, so the model can
// convert it and view it again. It returns nil when there is no such media.
func (h *turnHooks) rejectMedia(dialog gai.Dialog, reason string) (gai.Dialog, map[mediaKey]gai.Block) {
	last := -1
	for i, message := range dialog {
		if message.Role == gai.Assistant {
			last = i
		}
	}
	replacements := make(map[mediaKey]gai.Block)
	dialog = slices.Clone(dialog)
	for i := last + 1; i < len(dialog); i++ {
		message := dialog[i]
		if message.Role != gai.ToolResult {
			continue
		}
		blocks := slices.Clone(message.Blocks)
		for index, block := range blocks {
			if block.BlockType != gai.Content || block.ModalityType == gai.Text || block.Content == nil {
				continue
			}
			notice := gai.TextBlock(h.saveRejectedMedia(
				block,
				reason,
			))
			notice.ID = block.ID
			blocks[index] = notice
			replacements[newMediaKey(
				block.ID,
				index,
			)] = notice
		}
		message.Blocks = blocks
		dialog[i] = message
	}
	if len(replacements) == 0 {
		return nil, nil
	}
	return dialog, replacements
}

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

func (h *turnHooks) saveRejectedMedia(block gai.Block, reason string) string {
	path, size, err := h.saveMedia(block)
	if err != nil {
		return fmt.Sprintf(
			"[The provider rejected this %s media: %s. The media could not be saved: %v]",
			block.MimeType,
			reason,
			err,
		)
	}
	return fmt.Sprintf(
		"[The provider rejected this %s media (%d bytes): %s. The media was saved to %s; convert or shrink it, then view it again.]",
		block.MimeType,
		size,
		reason,
		path,
	)
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

// applyMediaReplacements swaps media the provider rejected earlier in this turn
// for its notices, so neither later requests nor saved history resend it.
func (h *turnHooks) applyMediaReplacements(dialog gai.Dialog) gai.Dialog {
	if len(h.mediaReplacements) == 0 {
		return dialog
	}
	dialog = slices.Clone(dialog)
	for i, message := range dialog {
		if message.Role != gai.ToolResult {
			continue
		}
		var blocks []gai.Block
		for index, block := range message.Blocks {
			replacement, ok := h.mediaReplacements[newMediaKey(
				block.ID,
				index,
			)]
			if !ok {
				continue
			}
			if blocks == nil {
				blocks = slices.Clone(message.Blocks)
			}
			blocks[index] = replacement
		}
		if blocks != nil {
			message.Blocks = blocks
			dialog[i] = message
		}
	}
	return dialog
}
