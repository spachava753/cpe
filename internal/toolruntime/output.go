package toolruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/spachava753/gai"
	"github.com/spachava753/starlarkx/starlark"
)

// This bounds only the model-facing preview, never execution or captured command data.
const maxTextBytes = 128 << 10

type outputCollector struct {
	kv     KeyValueStore
	text   strings.Builder
	images []gai.Block
}

func (o *outputCollector) add(block gai.Block) {
	if block.ModalityType == gai.Text {
		o.text.WriteString(block.Content.String())
		return
	}
	o.images = append(
		o.images,
		block,
	)
}

func (o *outputCollector) finish(ctx context.Context, runErr error) ([]gai.Block, error) {
	text := o.text.String()
	if runErr != nil {
		if text != "" && !strings.HasSuffix(
			text,
			"\n",
		) {
			text += "\n"
		}
		text += "Execution error: " + runErr.Error()
	}
	if len(text) > maxTextBytes {
		digest := sha256.Sum256([]byte(text))
		key := "overflow_" + hex.EncodeToString(digest[:])
		data, saveErr := json.Marshal(encodeKV(starlark.String(text)))
		if saveErr == nil {
			// Preserve emitted output even when cancellation ended the script.
			saveErr = o.kv.Store(
				context.WithoutCancel(ctx),
				key,
				data,
			)
		}
		marker := fmt.Sprintf(
			"\n[output truncated] Complete script output: kv.get(%q)",
			key,
		)
		if saveErr != nil {
			marker = "\n[output truncated; could not save complete script output to KV: " + saveErr.Error() + "]"
			if len(marker) >= maxTextBytes {
				marker = "\n[output truncated; could not save complete script output to KV]"
			}
			runErr = errors.Join(
				runErr,
				saveErr,
			)
		}
		text = text[:maxTextBytes-len(marker)] + marker
	}
	var blocks []gai.Block
	if text != "" {
		blocks = append(
			blocks,
			gai.TextBlock(text),
		)
	}
	blocks = append(
		blocks,
		o.images...,
	)
	return blocks, runErr
}
