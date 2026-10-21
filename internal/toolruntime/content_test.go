package toolruntime

import (
	"bytes"
	"context"
	"encoding/base64"
	stdimage "image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"

	"github.com/spachava753/dyson"
	"github.com/spachava753/gai"
	"github.com/spf13/afero"
)

func TestImage(t *testing.T) {
	fixture := stdimage.NewRGBA(stdimage.Rect(
		0,
		0,
		1,
		1,
	))
	var pngData, jpegData, gifData bytes.Buffer
	if err := png.Encode(
		&pngData,
		fixture,
	); err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(
		&jpegData,
		fixture,
		nil,
	); err != nil {
		t.Fatal(err)
	}
	if err := gif.Encode(
		&gifData,
		fixture,
		nil,
	); err != nil {
		t.Fatal(err)
	}
	webpData, err := base64.StdEncoding.DecodeString("UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name       string
		data       []byte
		code       string
		mime       string
		wantImages int
		wantError  string
		save       bool
		ordered    bool
	}{
		{
			name: "PNG file bytes with ordered text and image output",
			data: pngData.Bytes(),
			code: `print("before")
image(open("/image", "rb").read())
print("after")`,
			mime:       "image/png",
			wantImages: 1,
			ordered:    true,
		},
		{
			name:       "JPEG bytes with keyword argument",
			data:       jpegData.Bytes(),
			code:       `image(data=open("/image", "rb").read())`,
			mime:       "image/jpeg",
			wantImages: 1,
		},
		{
			name:       "GIF bytes",
			data:       gifData.Bytes(),
			code:       `image(open("/image", "rb").read())`,
			mime:       "image/gif",
			wantImages: 1,
		},
		{
			name:       "WebP bytes",
			data:       webpData,
			code:       `image(open("/image", "rb").read())`,
			mime:       "image/webp",
			wantImages: 1,
		},
		{
			name:       "stored bytes survive source removal and a fresh invocation",
			data:       pngData.Bytes(),
			code:       `image(kv.get("image"))`,
			mime:       "image/png",
			wantImages: 1,
			save:       true,
		},
		{
			name: "image output survives later script failure",
			data: pngData.Bytes(),
			code: `image(open("/image", "rb").read())
fail("later failure")`,
			mime:       "image/png",
			wantImages: 1,
			wantError:  "later failure",
		},
		{
			name:      "path strings are not image bytes",
			data:      pngData.Bytes(),
			code:      `image("/image")`,
			wantError: "want bytes",
		},
		{
			name:      "empty bytes",
			code:      `image(b"")`,
			wantError: "image is empty",
		},
		{
			name:      "unsupported format",
			code:      `image(b"<svg xmlns='http://www.w3.org/2000/svg'></svg>")`,
			wantError: "only PNG, JPEG, GIF, and WebP images are supported",
		},
		{
			name: "oversized image",
			data: append(
				pngData.Bytes(),
				make(
					[]byte,
					maxOutputBytes*3/4,
				)...,
			),
			code:      `image(open("/image", "rb").read())`,
			wantError: "exceeds output limit",
		},
		{
			name: "image output respects the shared collector limit",
			data: pngData.Bytes(),
			code: `data = open("/image", "rb").read()
for i in range(1025):
    image(data)`,
			mime:       "image/png",
			wantImages: 1023,
			wantError:  "output limit reached",
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				host := hostFixture(
					t,
					emptyMCP{},
					runnerFunc(func(context.Context, dyson.Command) (dyson.CommandResult, error) {
						return dyson.CommandResult{}, nil
					}),
				)
				if err := afero.WriteFile(
					host.stdlib.FS,
					"/image",
					test.data,
					0o600,
				); err != nil {
					t.Fatal(err)
				}
				if test.save {
					result, err := host.execute(
						t.Context(),
						toolRequest(
							starlarkToolName,
							"code",
							`kv.store("image", open("/image", "rb").read())`,
						),
					)
					if err != nil || result.ToolResultError {
						t.Fatalf(
							"save bytes: %v, %v",
							result,
							err,
						)
					}
					if err := host.stdlib.FS.Remove("/image"); err != nil {
						t.Fatal(err)
					}
				}
				result, err := host.execute(
					t.Context(),
					toolRequest(
						starlarkToolName,
						"code",
						test.code,
					),
				)
				if err != nil || result.ToolResultError != (test.wantError != "") {
					t.Fatalf(
						"execute: %v, %v",
						result,
						err,
					)
				}
				images := 0
				var text strings.Builder
				for _, block := range result.Blocks {
					if block.ModalityType == gai.Image {
						images++
						if block.MimeType != test.mime || block.Content.String() != base64.StdEncoding.EncodeToString(test.data) {
							t.Fatal("image format or data changed")
						}
					} else {
						text.WriteString(block.Content.String())
					}
				}
				if images != test.wantImages || !strings.Contains(
					text.String(),
					test.wantError,
				) {
					t.Fatalf(
						"images=%d, text=%q; want images=%d, error=%q",
						images,
						text.String(),
						test.wantImages,
						test.wantError,
					)
				}
				if test.ordered && (len(result.Blocks) != 3 || result.Blocks[0].Content.String() != "before\n" || result.Blocks[1].ModalityType != gai.Image || result.Blocks[2].Content.String() != "after\n") {
					t.Fatal("text and image output were reordered")
				}
			},
		)
	}
}
