package toolruntime

import (
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"strings"

	"github.com/spachava753/gai"
	"github.com/spachava753/starlarkx/starlark"
	"github.com/spachava753/starlarkx/starlarkstruct"
)

// MediaBlock builds a model content block for media with a declared MIME type.
// CPE applies no size or format limits; the provider decides what it accepts.
func MediaBlock(data []byte, mimeType string) (gai.Block, error) {
	if len(data) == 0 {
		return gai.Block{}, errors.New("media data is empty")
	}
	mediaType, _, err := mime.ParseMediaType(mimeType)
	if err != nil {
		return gai.Block{}, fmt.Errorf(
			"invalid MIME type %q",
			mimeType,
		)
	}
	switch {
	case strings.HasPrefix(
		mediaType,
		"image/",
	):
		return gai.ImageBlock(
			data,
			mediaType,
		), nil
	case strings.HasPrefix(
		mediaType,
		"audio/",
	):
		return gai.AudioBlock(
			data,
			mediaType,
		), nil
	case strings.HasPrefix(
		mediaType,
		"video/",
	):
		return gai.Block{
			BlockType:    gai.Content,
			ModalityType: gai.Video,
			MimeType:     mediaType,
			Content:      gai.Str(base64.StdEncoding.EncodeToString(data)),
		}, nil
	case mediaType == "application/pdf":
		return gai.PDFBlock(
			data,
			"document.pdf",
		), nil
	default:
		return gai.Block{}, fmt.Errorf(
			"cannot show %q to the model; view accepts image/*, audio/*, video/*, and application/pdf",
			mediaType,
		)
	}
}

func view(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var data starlark.Bytes
	var mimeType string
	if err := starlark.UnpackArgs(
		fn.Name(),
		args,
		kwargs,
		"data",
		&data,
		"mime_type",
		&mimeType,
	); err != nil {
		return nil, err
	}
	current, err := currentInvocation(thread)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(mimeType) == "" {
		return nil, errors.New("view: mime_type is required, for example view(data, \"image/png\")")
	}
	block, err := MediaBlock(
		[]byte(data),
		mimeType,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"view: %w",
			err,
		)
	}
	current.addMedia(block)
	return starlark.None, nil
}

func (r *runtime) bashBuiltin(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var script string
	var timeoutValue starlark.Value = starlark.None
	if err := starlark.UnpackArgs(
		fn.Name(),
		args,
		kwargs,
		"script",
		&script,
		"timeout?",
		&timeoutValue,
	); err != nil {
		return nil, err
	}
	current, err := currentInvocation(thread)
	if err != nil {
		return nil, err
	}
	timeout, err := timeoutArg(timeoutValue)
	if err != nil {
		return nil, fmt.Errorf(
			"bash: %w",
			err,
		)
	}
	sink := newTailSink(
		r.config.TempDir,
		outputPattern(
			current.callID,
			fmt.Sprintf(
				"script-bash%d",
				current.bashRun.Add(1),
			),
		),
		scriptBashMaxBytes,
		0,
	)
	run, err := runBash(
		current.ctx,
		r.config.Cwd,
		script,
		timeout,
		sink,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"bash: %w",
			err,
		)
	}
	fullOutput := starlark.Value(starlark.None)
	if run.output.path != "" {
		fullOutput = starlark.String(run.output.path)
	}
	return starlarkstruct.FromStringDict(
		starlark.String("BashResult"),
		starlark.StringDict{
			"output":           starlark.String(run.output.text),
			"exit_code":        starlark.MakeInt(run.exitCode),
			"timed_out":        starlark.Bool(run.timedOut),
			"truncated":        starlark.Bool(run.output.truncated),
			"full_output_path": fullOutput,
		},
	), nil
}
