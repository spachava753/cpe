package acp

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	acpsdk "github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/dyson"
	"github.com/spachava753/gai"
	"github.com/spachava753/starlarkx/starlark"

	acpstar "github.com/spachava753/cpe/internal/acp/acpstar"
	"github.com/spachava753/cpe/internal/xio"
)

const pdfMIMEType = "application/pdf"

type starlarkREPL struct {
	cwd         string
	outputLimit int
	sphere      *dyson.Sphere

	output  *xio.TailBuffer
	content []gai.Block
}

type starlarkResult struct {
	Output   string
	Content  []gai.Block
	TimedOut bool
}

func newStarlarkREPL(
	cwd string,
	outputLimit int,
	sessionID acpsdk.SessionId,
	store acpstar.SessionStore,
) *starlarkREPL {
	r := &starlarkREPL{
		cwd:         cwd,
		outputLimit: outputLimit,
	}
	stdlibConfig := dyson.HostStdlibConfig(cwd)
	stdlibConfig.CommandRunner = dyson.HostCommandRunner()
	stdlibConfig.HTTPClient = dyson.HostHTTPClient()
	r.sphere = dyson.NewSphere(
		func(_ *starlark.Thread, msg string) {
			if r.output != nil {
				fmt.Fprint(r.output, msg)
			}
		},
		dyson.NewStdlib(stdlibConfig),
		acpstar.Module(store, sessionID, cwd),
		dyson.GlobalSet{
			"view_file": starlark.NewBuiltin("view_file", r.viewFile),
		},
	)
	return r
}

func (r *starlarkREPL) Close() error {
	return r.sphere.Close()
}

func (r *starlarkREPL) Eval(ctx context.Context, code string, timeout time.Duration) (starlarkResult, error) {
	r.output = xio.NewTailBuffer(r.outputLimit)
	r.content = nil

	timeoutErr := fmt.Errorf("execution timed out after %s", timeout)
	executionCtx, cancel := context.WithTimeoutCause(ctx, timeout, timeoutErr)
	defer cancel()
	err := r.sphere.Eval(executionCtx, code)
	timedOut := errors.Is(context.Cause(executionCtx), timeoutErr)

	result := starlarkResult{
		Output:   r.output.String(),
		Content:  append([]gai.Block(nil), r.content...),
		TimedOut: timedOut,
	}
	if r.output.Truncated() {
		result.Output = "NOTE: output beginning was truncated\n\n" + result.Output
	}
	if result.TimedOut {
		return result, timeoutErr
	}
	return result, err
}

// viewFile resolves and reads the requested file, determines its media type, then publishes the matching ACP artifact block.
func (r *starlarkREPL) viewFile(
	_ *starlark.Thread,
	fn *starlark.Builtin,
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (starlark.Value, error) {
	var path, mimeType string
	if err := starlark.UnpackArgs(fn.Name(), args, kwargs, "path", &path, "mime_type?", &mimeType); err != nil {
		return nil, err
	}
	if path == "" {
		return nil, errors.New("view_file: path must not be empty")
	}

	resolved := path
	if !filepath.IsAbs(resolved) && r.cwd != "" {
		resolved = filepath.Join(r.cwd, resolved)
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return nil, fmt.Errorf("view_file: %w", err)
	}
	mimeType, err = artifactMIMEType(path, mimeType, data)
	if err != nil {
		return nil, err
	}

	filename := filepath.Base(path)
	var block gai.Block
	switch {
	case mimeType == pdfMIMEType:
		block = gai.PDFBlock(data, filename)
	case strings.HasPrefix(mimeType, "image/"):
		block = gai.ImageBlock(data, mimeType)
	case strings.HasPrefix(mimeType, "audio/"):
		block = gai.AudioBlock(data, mimeType)
	case strings.HasPrefix(mimeType, "video/"):
		block = gai.Block{
			BlockType:    gai.Content,
			ModalityType: gai.Video,
			MimeType:     mimeType,
			Content:      gai.Str(base64.StdEncoding.EncodeToString(data)),
		}
	default:
		return nil, fmt.Errorf("view_file: unsupported MIME type %q", mimeType)
	}
	if block.ExtraFields == nil {
		block.ExtraFields = make(map[string]any)
	}
	block.ExtraFields[gai.BlockFieldFilenameKey] = filename
	r.content = append(r.content, block)
	return starlark.None, nil
}

func artifactMIMEType(path, explicit string, data []byte) (string, error) {
	mimeType := explicit
	if mimeType == "" {
		mimeType = mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
	}
	if mimeType == "" {
		mimeType = http.DetectContentType(data)
	}
	mediaType, _, err := mime.ParseMediaType(mimeType)
	if err != nil {
		return "", fmt.Errorf("view_file: invalid MIME type %q: %w", mimeType, err)
	}
	if mediaType == "application/x-pdf" {
		mediaType = pdfMIMEType
	}
	return mediaType, nil
}
