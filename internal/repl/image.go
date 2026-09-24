package repl

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"

	"github.com/spachava753/starlarkx/starlark"
)

// Image is an inline image emitted by Starlark. Data is canonical base64 and
// MIMEType identifies PNG, JPEG, GIF, or WebP bytes. Images are saved in eval_end
// independently of the text output limit and compared during restoration.
type Image struct {
	Data     string `json:"data"`
	MIMEType string `json:"mimeType"`
}

const maxImageBytes = 20 << 20

func (r *REPL) emitImage(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var value starlark.Value
	var mimeType string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "image", &value, "mime_type?", &mimeType); err != nil {
		return nil, err
	}
	if dict, ok := value.(*starlark.Dict); ok {
		if mimeType != "" {
			return nil, errors.New("emit_image: omit mime_type when passing an MCP image")
		}
		var fields [3]string
		for i, key := range []string{"type", "data", "mimeType"} {
			v, found, err := dict.Get(starlark.String(key))
			if err != nil {
				return nil, err
			}
			text, ok := starlark.AsString(v)
			if !found || !ok {
				return nil, fmt.Errorf("emit_image: MCP image requires string %s", key)
			}
			fields[i] = text
		}
		if fields[0] != "image" {
			return nil, errors.New("emit_image: expected MCP image content")
		}
		value, mimeType = starlark.String(fields[1]), fields[2]
	}
	var data []byte
	switch v := value.(type) {
	case starlark.String:
		if len(v) > base64.StdEncoding.EncodedLen(maxImageBytes-r.imageBytes) {
			return nil, errors.New("emit_image: images exceed 20 MiB per evaluation")
		}
		var err error
		data, err = base64.StdEncoding.Strict().DecodeString(string(v))
		if err != nil {
			return nil, fmt.Errorf("emit_image: invalid base64: %w", err)
		}
	case starlark.Bytes:
		data = []byte(v)
	default:
		return nil, errors.New("emit_image: pass an MCP image, base64 string, or bytes with mime_type")
	}
	if len(data) == 0 {
		return nil, errors.New("emit_image: empty image")
	}
	if len(data) > maxImageBytes-r.imageBytes {
		return nil, errors.New("emit_image: images exceed 20 MiB per evaluation")
	}
	switch mimeType {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
	default:
		return nil, errors.New("emit_image: mime_type must be image/png, image/jpeg, image/gif, or image/webp")
	}
	if http.DetectContentType(data) != mimeType {
		return nil, errors.New("emit_image: bytes do not match the image MIME type")
	}
	r.images = append(r.images, Image{Data: base64.StdEncoding.EncodeToString(data), MIMEType: mimeType})
	r.imageBytes += len(data)
	return starlark.None, nil
}
