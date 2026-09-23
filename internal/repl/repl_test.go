package repl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spachava753/dyson"

	"github.com/spachava753/cpe/internal/session"
	"github.com/spachava753/cpe/internal/testutil/testgate"
)

func TestReplayRestoresNativeValuesWithoutHostEffects(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"value":41,"items":[1,2,3]}`)
	}))
	defer server.Close()
	if err := os.WriteFile(filepath.Join(dir, "input"), []byte("old\xff\x00"), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := session.Open(path, dir, Runtime)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	tools := []Tool{{Name: "answer", Execute: func(context.Context, map[string]any) (any, error) {
		calls++
		return map[string]any{"value": 1234567890123456789, "list": []any{true, nil, "hello"}}, nil
	}}}
	r, err := New(t.Context(), Options{Store: store, CWD: dir, Tools: tools})
	if err != nil {
		t.Fatal(err)
	}
	code := fmt.Sprintf(`load("os.star", "os")
load("requests.star", "requests")
load("time.star", "time")
load("tools.star", "answer")
file = open("input", "rb")
raw = file.read()
file.close()
fd = os.open("effect", os.O_WRONLY | os.O_CREAT | os.O_APPEND, 0o600)
os.write(fd, "once\n")
os.close(fd)
response = requests.post(%q, json={"test": 1})
number = response.json()["value"]
clock_value = time.time_ns()
value = answer()
large = 99999999999999999999999999999999999999
things = [raw, (True, None, large), {"number": number}]
def compute(x):
    return x + number
print(compute(1), value["value"])
`, server.URL)
	result, err := r.Eval(t.Context(), "first", code)
	if err != nil || result.Error != "" {
		t.Fatalf("eval: %+v, %v", result, err)
	}
	if result.Output != "42 1234567890123456789\n" {
		t.Fatalf("output %q", result.Output)
	}
	wantClock := r.j.store.Path()
	if len(wantClock) < 5 {
		t.Fatal("missing journal")
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	server.Close()
	if err := os.Remove(filepath.Join(dir, "input")); err != nil {
		t.Fatal(err)
	}
	store, err = session.Open(path, dir, Runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	tools[0].Execute = func(context.Context, map[string]any) (any, error) {
		t.Fatal("tool called during replay")
		return nil, nil
	}
	r, err = New(t.Context(), Options{Store: store, CWD: dir, Tools: tools, Host: &dyson.StdlibConfig{}})
	if err != nil {
		t.Fatalf("restore with all host capabilities disabled: %v", err)
	}
	defer r.Close()
	result, err = r.Eval(t.Context(), "inspect", `print(compute(2), response.json()["items"], raw, things[1], value["list"], clock_value > 0)`)
	if err != nil || result.Error != "" {
		t.Fatalf("inspect: %+v, %v", result, err)
	}
	if !strings.Contains(result.Output, "43 [1, 2, 3]") || !strings.Contains(result.Output, "99999999999999999999999999999999999999") {
		t.Fatalf("restored types: %s", result.Output)
	}
	effect, err := os.ReadFile(filepath.Join(dir, "effect"))
	if err != nil {
		t.Fatal(err)
	}
	if string(effect) != "once\n" || requests.Load() != 1 || calls != 1 {
		t.Fatalf("repeated effects: %q, HTTP=%d, tool=%d", effect, requests.Load(), calls)
	}
}

func TestFailedAndCanceledChunksRollbackInterpreterOnly(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Open(filepath.Join(dir, "s.jsonl"), dir, Runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	r, err := New(t.Context(), Options{Store: store, CWD: dir, Timeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	result, err := r.Eval(t.Context(), "setup", `x = [1]`)
	if err != nil || result.Error != "" {
		t.Fatal(result, err)
	}
	result, err = r.Eval(t.Context(), "failed", `load("os.star", "os")
x.append(2)
os.mkdir("created")
fail("oops")`)
	if err != nil || !strings.Contains(result.Error, "oops") {
		t.Fatal(result, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "created")); err != nil {
		t.Fatal("external effect lost", err)
	}
	result, err = r.Eval(t.Context(), "inspect", `print(x)`)
	if err != nil || result.Output != "[1]\n" {
		t.Fatal(result, err)
	}
	result, err = r.Eval(t.Context(), "loop", `x.append(3)
while True:
    pass`)
	if err != nil || result.Committed || !strings.Contains(result.Error, "deadline") {
		t.Fatal(result, err)
	}
	result, err = r.Eval(t.Context(), "inspect2", `print(x)`)
	if err != nil || result.Output != "[1]\n" {
		t.Fatal(result, err)
	}
}

func TestInterruptedHostCallIsNotRetried(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	store, err := session.Open(path, dir, Runtime)
	if err != nil {
		t.Fatal(err)
	}
	id, err := store.Append("eval_start", evalStart{CallID: "interrupted", Code: `load("os.star", "os"); os.mkdir("must-not-exist")`})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Append("host_call", hostCall{Name: "fs.mkdir", Args: json.RawMessage(`["must-not-exist",511]`)})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = session.Open(path, dir, Runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	r, err := New(t.Context(), Options{Store: store, CWD: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err := os.Stat(filepath.Join(dir, "must-not-exist")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("host call executed: %v", err)
	}
	entries := store.Path()
	last := entries[len(entries)-1]
	var result Result
	if err := json.Unmarshal(last.Data, &result); err != nil {
		t.Fatal(err)
	}
	if last.Type != "eval_end" || result.EvalID != id || result.Committed || !strings.Contains(result.Error, "may have occurred") {
		t.Fatalf("missing uncertain outcome: %+v", result)
	}
}

func TestReplayFailsClosedOnDivergence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	store, err := session.Open(path, dir, Runtime)
	if err != nil {
		t.Fatal(err)
	}
	r, err := New(t.Context(), Options{Store: store, CWD: dir})
	if err != nil {
		t.Fatal(err)
	}
	result, err := r.Eval(t.Context(), "call", `load("os.star", "os"); os.mkdir("first")`)
	if err != nil || result.Error != "" {
		t.Fatal(result, err)
	}
	_ = r.Close()
	_ = store.Close()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Modify only source, not the recorded host arguments.
	data = []byte(strings.Replace(string(data), `os.mkdir(\"first\")`, `os.mkdir(\"second\")`, 1))
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	store, err = session.Open(path, dir, Runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, err = New(t.Context(), Options{Store: store, CWD: dir})
	if err == nil || !strings.Contains(err.Error(), "diverged") {
		t.Fatalf("expected replay divergence, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "second")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("replay escaped to host: %v", err)
	}
}

func TestReplayPreservesFilesystemErrors(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Open(filepath.Join(dir, "s.jsonl"), dir, Runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	r, err := New(t.Context(), Options{Store: store, CWD: dir})
	if err != nil {
		t.Fatal(err)
	}
	result, err := r.Eval(t.Context(), "missing", `load("os.star", "os"); missing = os.path.exists("absent"); print(missing)`)
	if err != nil || result.Output != "False\n" {
		t.Fatal(result, err)
	}
	_ = r.Close()
	r, err = New(t.Context(), Options{Store: store, CWD: dir, Host: &dyson.StdlibConfig{}})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	result, err = r.Eval(t.Context(), "check", `print(missing)`)
	if err != nil || result.Output != "False\n" {
		t.Fatal(result, err)
	}
}

func TestCommandReplayDoesNotExecuteProcess(t *testing.T) {
	testgate.RequireIntegration(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	store, err := session.Open(path, dir, Runtime)
	if err != nil {
		t.Fatal(err)
	}
	r, err := New(t.Context(), Options{Store: store, CWD: dir})
	if err != nil {
		t.Fatal(err)
	}
	result, err := r.Eval(t.Context(), "command", `load("subprocess.star", "subprocess")
proc = subprocess.run(["sh", "-c", "printf once >> effect; printf output"], capture_output=True, text=True)
print(proc.stdout, proc.returncode)`)
	if err != nil || result.Error != "" || result.Output != "output 0\n" {
		t.Fatal(result, err)
	}
	_ = r.Close()
	_ = store.Close()
	store, err = session.Open(path, dir, Runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	r, err = New(t.Context(), Options{Store: store, CWD: dir, Host: &dyson.StdlibConfig{}})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	result, err = r.Eval(t.Context(), "check", `print(proc.stdout, proc.returncode)`)
	if err != nil || result.Output != "output 0\n" {
		t.Fatal(result, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "effect"))
	if err != nil || string(data) != "once" {
		t.Fatalf("repeated command: %q %v", data, err)
	}
}

func TestRestoredHandlesReattachOnlyOnNewIOWithoutTruncation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	if err := os.WriteFile(filepath.Join(dir, "read"), []byte("abcdef"), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := session.Open(path, dir, Runtime)
	if err != nil {
		t.Fatal(err)
	}
	r, err := New(t.Context(), Options{Store: store, CWD: dir})
	if err != nil {
		t.Fatal(err)
	}
	result, err := r.Eval(t.Context(), "open", `load("os.star", "os")
reader = open("read", "rb")
prefix = reader.read(2)
fd = os.open("write", os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
os.write(fd, "first")`)
	if err != nil || result.Error != "" {
		t.Fatal(result, err)
	}
	_ = r.Close()
	_ = store.Close()
	store, err = session.Open(path, dir, Runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	r, err = New(t.Context(), Options{Store: store, CWD: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	before, err := os.ReadFile(filepath.Join(dir, "write"))
	if err != nil || string(before) != "first" {
		t.Fatalf("replay truncated file: %q %v", before, err)
	}
	result, err = r.Eval(t.Context(), "continue", `print(prefix, reader.read())
os.write(fd, "second")
os.close(fd)
reader.close()`)
	if err != nil || result.Error != "" || !strings.Contains(result.Output, `b"ab" b"cdef"`) {
		t.Fatal(result, err)
	}
	after, err := os.ReadFile(filepath.Join(dir, "write"))
	if err != nil || string(after) != "firstsecond" {
		t.Fatalf("reattach lost position: %q %v", after, err)
	}
}

func TestResultWriteFailureNeverRetriesEffect(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	store, err := session.Open(path, dir, Runtime)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	tool := Tool{Name: "effect", Execute: func(context.Context, map[string]any) (any, error) {
		calls++
		// Simulate losing the journal after an external effect, before its outcome
		// can be written. The intent is already on disk.
		if err := store.Close(); err != nil {
			return nil, err
		}
		return "performed", nil
	}}
	r, err := New(t.Context(), Options{Store: store, CWD: dir, Tools: []Tool{tool}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Eval(t.Context(), "failed-result", `load("tools.star", "effect"); first = effect(); second = effect()`)
	if err == nil || calls != 1 {
		t.Fatalf("result write failure: calls=%d err=%v", calls, err)
	}
	_ = r.Close()
	store, err = session.Open(path, dir, Runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	r, err = New(t.Context(), Options{Store: store, CWD: dir, Tools: []Tool{tool}})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if calls != 1 {
		t.Fatal("uncertain effect retried")
	}
	result, err := r.Eval(t.Context(), "fresh", `print("ready")`)
	if err != nil || result.Output != "ready\n" {
		t.Fatal(result, err)
	}
}
