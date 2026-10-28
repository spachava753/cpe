package toolruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spachava753/gai"
	gaiagent "github.com/spachava753/gai/agent"

	"github.com/spachava753/cpe/internal/mcpclient"
	"github.com/spachava753/cpe/internal/plugins"
)

type testRuntime struct {
	*runtime
	cwd     string
	tempDir string
	calls   int
}

// newTestRuntime connects the given in-memory servers, keyed by configured name.
func newTestRuntime(t *testing.T, servers map[string]*mcp.Server) *testRuntime {
	t.Helper()
	pool, err := mcpclient.NewPool(
		func(ctx context.Context, server plugins.MCPServer, _ io.Writer) (*mcp.ClientSession, error) {
			remote := servers[server.Command]
			a, b := mcp.NewInMemoryTransports()
			session, err := remote.Connect(
				ctx,
				a,
				nil,
			)
			if err != nil {
				return nil, err
			}
			t.Cleanup(func() { _ = session.Close() })
			return mcp.NewClient(
				&mcp.Implementation{
					Name:    "test",
					Version: "1",
				},
				nil,
			).Connect(
				ctx,
				b,
				nil,
			)
		},
		0,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	var specs []mcpclient.ServerSpec
	for name := range servers {
		specs = append(
			specs,
			mcpclient.ServerSpec{
				ID:          mcpclient.ServerID(name),
				Source:      mcpclient.SourceSettings,
				Description: "configured " + name,
				Transport: plugins.MCPServer{
					Type:    plugins.Stdio,
					Command: name,
				},
			},
		)
	}
	connected, failures := mcpclient.Setup(
		t.Context(),
		pool,
		specs,
		5*time.Second,
	)
	t.Cleanup(connected.Close)
	if len(failures) != 0 {
		t.Fatal(failures[0].Err)
	}
	prefixes, err := mcpclient.Prefixes(specs)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := mcpclient.NewCatalog(
		prefixes,
		connected.Connected(),
	)
	if err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	tempDir := t.TempDir()
	host, err := New(
		t.Context(),
		Config{
			Cwd:     cwd,
			TempDir: tempDir,
			Catalog: catalog,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return &testRuntime{
		runtime: host.(*runtime),
		cwd:     cwd,
		tempDir: tempDir,
	}
}

func (r *testRuntime) call(t *testing.T, ctx context.Context, tool string, parameters map[string]any) gai.Message {
	t.Helper()
	r.calls++
	id := fmt.Sprintf(
		"call_%d",
		r.calls,
	)
	for _, candidate := range r.Tools() {
		if candidate.Definition.Name != tool {
			continue
		}
		message, err := candidate.Handler.Execute(
			ctx,
			gaiagent.ToolRequest{
				Block: gai.Block{
					ID: id,
				},
				Call: gai.ToolCallInput{
					Name:       tool,
					Parameters: parameters,
				},
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		return message
	}
	t.Fatalf(
		"unknown tool %q",
		tool,
	)
	return gai.Message{}
}

func (r *testRuntime) script(t *testing.T, code string) (string, bool) {
	t.Helper()
	message := r.call(
		t,
		t.Context(),
		StarlarkToolName,
		map[string]any{
			"code": code,
		},
	)
	return resultText(message), message.ToolResultError
}

func (r *testRuntime) bash(t *testing.T, script string) (string, bool) {
	t.Helper()
	message := r.call(
		t,
		t.Context(),
		BashToolName,
		map[string]any{
			"script": script,
		},
	)
	return resultText(message), message.ToolResultError
}

func resultText(message gai.Message) string {
	var texts []string
	for _, block := range message.Blocks {
		if block.ModalityType == gai.Text {
			texts = append(
				texts,
				block.Content.String(),
			)
		}
	}
	return strings.Join(
		texts,
		"\n",
	)
}

func TestToolDefinitions(t *testing.T) {
	r := newTestRuntime(
		t,
		nil,
	)
	tools := r.Tools()
	if len(tools) != 2 || tools[0].Definition.Name != "bash" || tools[1].Definition.Name != "starlark_exec" {
		t.Fatalf(
			"tools = %+v",
			tools,
		)
	}
	schema, err := json.Marshal(tools[0].Definition.InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(
		string(schema),
		`"required":["script"]`,
	) || !strings.Contains(
		string(schema),
		`"timeout"`,
	) {
		t.Fatalf(
			"bash schema = %s",
			schema,
		)
	}
}

func TestBashTool(t *testing.T) {
	r := newTestRuntime(
		t,
		nil,
	)
	text, failed := r.bash(
		t,
		"echo out; echo err >&2; echo again; pwd",
	)
	if failed || text != "out\nerr\nagain\n"+r.cwd {
		t.Fatalf(
			"output = %q, failed = %t",
			text,
			failed,
		)
	}
	text, failed = r.bash(
		t,
		"echo partial; exit 3",
	)
	if !failed || text != "partial\n[Exit status 3]" {
		t.Fatalf(
			"nonzero exit output = %q, failed = %t",
			text,
			failed,
		)
	}
	text, failed = r.bash(
		t,
		"true",
	)
	if failed || text != "(no output)" {
		t.Fatalf(
			"silent output = %q",
			text,
		)
	}
	text, failed = r.bash(
		t,
		"  ",
	)
	if !failed || !strings.Contains(
		text,
		"nonempty",
	) {
		t.Fatalf(
			"empty script = %q",
			text,
		)
	}
}

func TestBashToolTimeoutKillsProcessGroup(t *testing.T) {
	r := newTestRuntime(
		t,
		nil,
	)
	marker := filepath.Join(
		r.cwd,
		"survived",
	)
	start := time.Now()
	message := r.call(
		t,
		t.Context(),
		BashToolName,
		map[string]any{
			"script":  "echo started; (sleep 2; touch " + marker + ") & sleep 30",
			"timeout": 0.5,
		},
	)
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf(
			"timeout took %s",
			elapsed,
		)
	}
	text := resultText(message)
	if !message.ToolResultError || !strings.HasPrefix(
		text,
		"started\n[Timed out after 500ms",
	) {
		t.Fatalf(
			"timeout output = %q",
			text,
		)
	}
	time.Sleep(3 * time.Second)
	if _, err := os.Stat(marker); !errors.Is(
		err,
		os.ErrNotExist,
	) {
		t.Fatal("a background child outlived the timed-out call")
	}
	bad := r.call(
		t,
		t.Context(),
		BashToolName,
		map[string]any{
			"script":  "true",
			"timeout": -1.0,
		},
	)
	if !bad.ToolResultError {
		t.Fatal("negative timeout was accepted")
	}
}

func TestBashToolTruncatesToTail(t *testing.T) {
	r := newTestRuntime(
		t,
		nil,
	)
	text, failed := r.bash(
		t,
		"for i in $(seq 1 5000); do echo line $i; done; echo done >&2; exit 1",
	)
	if !failed {
		t.Fatal("nonzero exit was not an error")
	}
	lines := strings.Split(
		text,
		"\n",
	)
	if !strings.HasPrefix(
		lines[0],
		"[Output truncated: showing lines 3002-5001 of 5001",
	) || lines[1] != "line 3002" || lines[len(lines)-2] != "done" || lines[len(lines)-1] != "[Exit status 1]" {
		t.Fatalf(
			"head = %q, tail = %q",
			lines[:2],
			lines[len(lines)-2:],
		)
	}
	path := lines[0][strings.Index(
		lines[0],
		"Full output: ",
	)+len("Full output: ") : len(lines[0])-1]
	if !strings.HasPrefix(
		path,
		r.tempDir,
	) || !strings.Contains(
		filepath.Base(path),
		"call_1-bash-",
	) {
		t.Fatalf(
			"overflow path = %q",
			path,
		)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(
		string(saved),
		"line 1\nline 2\n",
	) || !strings.HasSuffix(
		string(saved),
		"line 5000\ndone\n",
	) {
		t.Fatalf(
			"saved output has %d bytes",
			len(saved),
		)
	}
}

func TestStarlarkBasics(t *testing.T) {
	r := newTestRuntime(
		t,
		nil,
	)
	for _, test := range []struct {
		name       string
		code       string
		want       string
		wantFailed bool
	}{
		{
			name: "print",
			code: "print('hello', 1)\nprint(json.encode({'a': [1, 2]}))",
			want: "hello 1\n{\"a\":[1,2]}",
		},
		{
			name: "silent",
			code: "x = 1",
			want: "Script completed with no output.",
		},
		{
			name: "while and top-level control",
			code: "i = 0\nwhile i < 3:\n    i += 1\nif i == 3:\n    print('looped', i)",
			want: "looped 3",
		},
		{
			name: "sets",
			code: "print(sorted(set([3, 1, 3])))",
			want: "[1, 3]",
		},
		{
			name:       "error keeps earlier output",
			code:       "print('before')\nfail('boom')\nprint('after')",
			want:       "before\nError: Traceback",
			wantFailed: true,
		},
		{
			name:       "recursion is disabled",
			code:       "def f(n):\n    return f(n - 1) if n else 0\nprint(f(2))",
			want:       "Error: Traceback",
			wantFailed: true,
		},
		{
			name:       "no load",
			code:       "load('os.star', 'os')",
			want:       "Error:",
			wantFailed: true,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				text, failed := r.script(
					t,
					test.code,
				)
				if failed != test.wantFailed || !strings.HasPrefix(
					text,
					test.want,
				) {
					t.Fatalf(
						"output = %q, failed = %t",
						text,
						failed,
					)
				}
				if strings.Contains(
					text,
					"after",
				) {
					t.Fatal("statements after an error ran")
				}
			},
		)
	}
}

func TestStarlarkTimeout(t *testing.T) {
	r := newTestRuntime(
		t,
		nil,
	)
	start := time.Now()
	message := r.call(
		t,
		t.Context(),
		StarlarkToolName,
		map[string]any{
			"code":    "print('spinning')\nwhile True:\n    pass",
			"timeout": 0.2,
		},
	)
	if time.Since(start) > 5*time.Second {
		t.Fatal("timeout did not stop the script")
	}
	text := resultText(message)
	if !message.ToolResultError || !strings.HasPrefix(
		text,
		"spinning\n[Timed out after 200ms",
	) {
		t.Fatalf(
			"output = %q",
			text,
		)
	}
	start = time.Now()
	message = r.call(
		t,
		t.Context(),
		StarlarkToolName,
		map[string]any{
			"code":    "bash('sleep 30')",
			"timeout": 0.2,
		},
	)
	if time.Since(start) > 5*time.Second || !message.ToolResultError {
		t.Fatalf(
			"timeout did not stop a bash builtin: %q",
			resultText(message),
		)
	}
}

func TestStarlarkCancellationReturnsContextError(t *testing.T) {
	r := newTestRuntime(
		t,
		nil,
	)
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	for _, tool := range r.Tools() {
		_, err := tool.Handler.Execute(
			ctx,
			gaiagent.ToolRequest{
				Block: gai.Block{
					ID: "x",
				},
				Call: gai.ToolCallInput{
					Name: tool.Definition.Name,
					Parameters: map[string]any{
						"code":   "bash('sleep 30')",
						"script": "sleep 30",
					},
				},
			},
		)
		if !errors.Is(
			err,
			context.Canceled,
		) {
			t.Fatalf(
				"%s cancellation error = %v",
				tool.Definition.Name,
				err,
			)
		}
	}
}

func TestFileBuiltins(t *testing.T) {
	r := newTestRuntime(
		t,
		nil,
	)
	text, failed := r.script(
		t,
		`write_file("a/b/c.txt", "hello world\n")
write_file("raw.bin", b"\x00\x01")
data = read_file("a/b/c.txt")
print(type(data), data.decode())
print(len(read_file("raw.bin")))
print(edit_file("a/b/c.txt", "world", "there"))
print(read_file("a/b/c.txt").decode())`,
	)
	if failed || text != "bytes hello world\n\n2\nNone\nhello there" {
		t.Fatalf(
			"output = %q, failed = %t",
			text,
			failed,
		)
	}
	target := filepath.Join(
		r.cwd,
		"target.txt",
	)
	if err := os.WriteFile(
		target,
		[]byte("one"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(
		r.cwd,
		"link.txt",
	)
	if err := os.Symlink(
		target,
		link,
	); err != nil {
		t.Fatal(err)
	}
	if text, failed := r.script(
		t,
		`write_file("link.txt", "two")
edit_file("link.txt", "two", "three")`,
	); failed {
		t.Fatal(text)
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	targetInfo, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 || string(content) != "three" || targetInfo.Mode().Perm() != 0o600 {
		t.Fatalf(
			"link kept = %t, content = %q, mode = %v",
			info.Mode()&os.ModeSymlink != 0,
			content,
			targetInfo.Mode().Perm(),
		)
	}
}

func TestEditFileFailureStopsScript(t *testing.T) {
	r := newTestRuntime(
		t,
		nil,
	)
	for name, content := range map[string]string{
		"one.txt":   "alpha",
		"two.txt":   "beta beta",
		"three.txt": "gamma",
	} {
		if err := os.WriteFile(
			filepath.Join(
				r.cwd,
				name,
			),
			[]byte(content),
			0o644,
		); err != nil {
			t.Fatal(err)
		}
	}
	text, failed := r.script(
		t,
		`edit_file("one.txt", "alpha", "ALPHA")
print("first edited")
edit_file("two.txt", "beta", "BETA")
edit_file("three.txt", "gamma", "GAMMA")
print("never")`,
	)
	if !failed || !strings.Contains(
		text,
		"first edited",
	) || !strings.Contains(
		text,
		"matches 2 times",
	) || strings.Contains(
		text,
		"never",
	) {
		t.Fatalf(
			"output = %q",
			text,
		)
	}
	for name, want := range map[string]string{
		"one.txt":   "ALPHA",
		"two.txt":   "beta beta",
		"three.txt": "gamma",
	} {
		got, err := os.ReadFile(filepath.Join(
			r.cwd,
			name,
		))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Fatalf(
				"%s = %q, want %q",
				name,
				got,
				want,
			)
		}
	}
	for code, want := range map[string]string{
		`edit_file("one.txt", "missing", "x")`:   "was not found",
		`edit_file("one.txt", "ALPHA", "ALPHA")`: "would not change",
		`edit_file("one.txt", "", "x")`:          "must be nonempty",
		`write_file("x.txt", 3)`:                 "must be str or bytes",
	} {
		text, failed := r.script(
			t,
			code,
		)
		if !failed || !strings.Contains(
			text,
			want,
		) {
			t.Fatalf(
				"%s: output = %q",
				code,
				text,
			)
		}
	}
	if text, failed := r.script(
		t,
		`edit_file("two.txt", "aa", "x")`,
	); !failed {
		t.Fatal(text)
	}
	if err := os.WriteFile(
		filepath.Join(
			r.cwd,
			"overlap.txt",
		),
		[]byte("aaa"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if text, failed := r.script(
		t,
		`edit_file("overlap.txt", "aa", "b")`,
	); !failed || !strings.Contains(
		text,
		"matches 2 times",
	) {
		t.Fatalf(
			"overlapping match output = %q",
			text,
		)
	}
}

func TestBashBuiltin(t *testing.T) {
	r := newTestRuntime(
		t,
		nil,
	)
	text, failed := r.script(
		t,
		`result = bash("echo hi; echo there >&2; exit 4")
print(repr(result.output), result.exit_code, result.timed_out, result.truncated, result.full_output_path)
big = bash("head -c 1100000 /dev/zero | tr '\\0' 'x'; echo; echo END")
print(big.truncated, len(big.output) <= 1048576, big.output.endswith("END\n"))
print(len(read_file(big.full_output_path)))
slow = bash("sleep 5", timeout=0.2)
print(slow.timed_out, slow.exit_code)`,
	)
	if failed || text != "\"hi\\nthere\\n\" 4 False False None\nTrue True True\n1100005\nTrue 137" {
		t.Fatalf(
			"output = %q, failed = %t",
			text,
			failed,
		)
	}
}

func TestView(t *testing.T) {
	r := newTestRuntime(
		t,
		nil,
	)
	png := []byte("\x89PNG\r\n\x1a\nrest")
	if err := os.WriteFile(
		filepath.Join(
			r.cwd,
			"shot.png",
		),
		png,
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	message := r.call(
		t,
		t.Context(),
		StarlarkToolName,
		map[string]any{
			"code": `print("look")
view(read_file("shot.png"), "image/png")
view(b"%PDF-1.4", "application/pdf")
view(b"RIFF", "audio/wav")`,
		},
	)
	if message.ToolResultError || len(message.Blocks) != 4 {
		t.Fatalf(
			"message = %+v",
			message,
		)
	}
	if message.Blocks[0].Content.String() != "look" || message.Blocks[1].ModalityType != gai.Image || message.Blocks[1].MimeType != "image/png" || message.Blocks[2].MimeType != "application/pdf" || message.Blocks[3].ModalityType != gai.Audio {
		t.Fatalf(
			"blocks = %+v",
			message.Blocks,
		)
	}
	for code, want := range map[string]string{
		`view(b"x", "")`:           "mime_type is required",
		`view(b"x")`:               "missing argument for mime_type",
		`view(b"", "image/png")`:   "empty",
		`view(b"x", "text/plain")`: "cannot show",
	} {
		text, failed := r.script(
			t,
			code,
		)
		if !failed || !strings.Contains(
			text,
			want,
		) {
			t.Fatalf(
				"%s: output = %q",
				code,
				text,
			)
		}
	}
}

func TestCompact(t *testing.T) {
	r := newTestRuntime(
		t,
		nil,
	)
	text, failed := r.script(
		t,
		`compact("summary")`,
	)
	if !failed || !strings.Contains(
		text,
		"requires an active prompt",
	) {
		t.Fatalf(
			"output = %q",
			text,
		)
	}
	var accepted string
	message := r.call(
		t,
		WithCompaction(
			t.Context(),
			func(summary string) { accepted = summary },
		),
		StarlarkToolName,
		map[string]any{
			"code": `compact("done: " + str(1 + 1))`,
		},
	)
	if message.ToolResultError || accepted != "done: 2" {
		t.Fatalf(
			"accepted = %q, output = %q",
			accepted,
			resultText(message),
		)
	}
}

func TestClosedRuntimeRejectsCalls(t *testing.T) {
	r := newTestRuntime(
		t,
		nil,
	)
	if err := r.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	_, err := r.Tools()[0].Handler.Execute(
		t.Context(),
		gaiagent.ToolRequest{
			Call: gai.ToolCallInput{
				Parameters: map[string]any{
					"script": "true",
				},
			},
		},
	)
	if err == nil {
		t.Fatal("closed runtime ran a command")
	}
}
