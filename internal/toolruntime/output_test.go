package toolruntime

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func writeAll(t *testing.T, sink *tailSink, chunks ...string) tailResult {
	t.Helper()
	for _, chunk := range chunks {
		if _, err := sink.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	return sink.finish()
}

func TestTailSinkKeepsSmallOutputInMemory(t *testing.T) {
	dir := t.TempDir()
	result := writeAll(
		t,
		newTailSink(
			dir,
			"x-*.txt",
			100,
			10,
		),
		"one\n",
		"two\n",
	)
	if result.truncated || result.text != "one\ntwo\n" || result.notice() != "" {
		t.Fatalf(
			"result = %+v",
			result,
		)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf(
			"small output touched the disk: %v",
			entries,
		)
	}
}

func TestTailSinkKeepsLastLinesAndSpillsEverything(t *testing.T) {
	var lines []string
	for i := 1; i <= 30; i++ {
		lines = append(
			lines,
			fmt.Sprintf(
				"line %d\n",
				i,
			),
		)
	}
	result := writeAll(
		t,
		newTailSink(
			t.TempDir(),
			"x-*.txt",
			1000,
			5,
		),
		lines...,
	)
	if !result.truncated || result.text != strings.Join(
		lines[25:],
		"",
	) || result.firstLine != 26 || result.lines != 30 || result.partial {
		t.Fatalf(
			"result = %+v",
			result,
		)
	}
	saved, err := os.ReadFile(result.path)
	if err != nil {
		t.Fatal(err)
	}
	if string(saved) != strings.Join(
		lines,
		"",
	) {
		t.Fatalf(
			"saved output = %q",
			saved,
		)
	}
	if notice := result.notice(); !strings.Contains(
		notice,
		"showing lines 26-30 of 30",
	) || !strings.Contains(
		notice,
		result.path,
	) {
		t.Fatalf(
			"notice = %q",
			notice,
		)
	}
}

func TestTailSinkByteLimitMarksPartialLines(t *testing.T) {
	long := strings.Repeat(
		"é",
		100,
	)
	result := writeAll(
		t,
		newTailSink(
			t.TempDir(),
			"x-*.txt",
			51,
			0,
		),
		long,
	)
	if !result.truncated || !result.partial || result.shownBytes > 51 || !strings.HasPrefix(
		long,
		"é",
	) || strings.ContainsRune(
		result.text,
		'�',
	) {
		t.Fatalf(
			"result = %+v",
			result,
		)
	}
	if result.text != strings.Repeat(
		"é",
		25,
	) {
		t.Fatalf(
			"tail = %q; want whole runes only",
			result.text,
		)
	}
	if !strings.Contains(
		result.notice(),
		"first line shown is partial",
	) {
		t.Fatalf(
			"notice = %q",
			result.notice(),
		)
	}
}

func TestTailSinkReportsUnsavedOutput(t *testing.T) {
	result := writeAll(
		t,
		newTailSink(
			"/nonexistent/cpe-test-dir",
			"x-*.txt",
			4,
			0,
		),
		"abcdefgh",
	)
	if !result.truncated || result.err == nil || result.path != "" || result.text != "efgh" {
		t.Fatalf(
			"result = %+v",
			result,
		)
	}
	if !strings.Contains(
		result.notice(),
		"could not be saved",
	) {
		t.Fatalf(
			"notice = %q",
			result.notice(),
		)
	}
}

func TestTailSinkMarksPartialLineAfterRollingTrim(t *testing.T) {
	// Several writes force the rolling buffer to drop the start of one long
	// line before finish, so finish itself needs no further cut.
	chunk := strings.Repeat(
		"x",
		1024,
	)
	sink := newTailSink(
		t.TempDir(),
		"x-*.txt",
		2048,
		0,
	)
	for range 149 {
		if _, err := sink.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	result := sink.finish()
	if !result.truncated || !result.partial || len(result.text) > 2048 || !strings.Contains(
		result.notice(),
		"first line shown is partial",
	) {
		t.Fatalf(
			"result = %+v, notice = %q",
			result,
			result.notice(),
		)
	}
}
