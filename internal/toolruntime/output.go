package toolruntime

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"sync"
	"unicode/utf8"
)

const (
	// toolMaxBytes and toolMaxLines bound the text the model sees from one tool call.
	toolMaxBytes = 50 * 1024
	toolMaxLines = 2000
	// scriptBashMaxBytes bounds the output a script receives from the bash builtin.
	scriptBashMaxBytes = 1 << 20
)

// tailSink keeps the tail of a stream within byte and line limits. Once either
// limit is exceeded, the complete stream also goes to a file in dir, so small
// outputs never touch the disk and large ones never sit whole in memory.
type tailSink struct {
	file     *os.File
	fileErr  error
	dir      string
	pattern  string
	path     string
	buf      []byte
	total    int64
	newlines int64
	maxBytes int
	// maxLines is zero when only bytes are limited.
	maxLines int
	mu       sync.Mutex
	last     byte
	// midLine records that trimming buf dropped the start of its first line.
	midLine bool
}

func newTailSink(dir, pattern string, maxBytes, maxLines int) *tailSink {
	return &tailSink{
		dir:      dir,
		pattern:  pattern,
		maxBytes: maxBytes,
		maxLines: maxLines,
	}
}

func (s *tailSink) lines() int64 {
	if s.total > 0 && s.last != '\n' {
		return s.newlines + 1
	}
	return s.newlines
}

func (s *tailSink) exceeded() bool {
	return s.total > int64(s.maxBytes) || s.maxLines > 0 && s.lines() > int64(s.maxLines)
}

// Write never fails: a spill error is reported with the result, and the tail
// stays complete either way.
func (s *tailSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(p) == 0 {
		return 0, nil
	}
	s.total += int64(len(p))
	s.newlines += int64(bytes.Count(
		p,
		[]byte{'\n'},
	))
	s.last = p[len(p)-1]
	spilled := s.file != nil || s.fileErr != nil
	s.buf = append(
		s.buf,
		p...,
	)
	if !spilled && s.exceeded() {
		s.spill()
	} else if s.file != nil {
		s.writeFile(p)
	}
	if (s.file != nil || s.fileErr != nil) && len(s.buf) > 2*s.maxBytes {
		s.midLine = s.buf[len(s.buf)-s.maxBytes-1] != '\n'
		s.buf = append(
			s.buf[:0],
			s.buf[len(s.buf)-s.maxBytes:]...,
		)
	}
	return len(p), nil
}

// spill copies everything seen so far, which is still wholly in buf, to a new file.
func (s *tailSink) spill() {
	file, err := os.CreateTemp(
		s.dir,
		s.pattern,
	)
	if err != nil {
		s.fileErr = err
		return
	}
	s.file = file
	s.path = file.Name()
	s.writeFile(s.buf)
}

func (s *tailSink) writeFile(p []byte) {
	if _, err := s.file.Write(p); err != nil {
		s.fileErr = err
		_ = s.file.Close()
		s.file = nil
	}
}

// tailResult describes the visible part of a stream.
type tailResult struct {
	err        error
	text       string
	path       string
	total      int64
	lines      int64
	firstLine  int64
	shownBytes int
	truncated  bool
	partial    bool
}

// finish closes any spill file and returns the visible tail.
func (s *tailSink) finish() tailResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file != nil {
		if err := s.file.Close(); err != nil && s.fileErr == nil {
			s.fileErr = err
		}
		s.file = nil
	}
	result := tailResult{
		total:     s.total,
		lines:     s.lines(),
		firstLine: 1,
	}
	if !s.exceeded() {
		result.text = string(s.buf)
		result.shownBytes = len(s.buf)
		return result
	}
	result.truncated = true
	if s.fileErr == nil {
		result.path = s.path
	} else {
		result.err = s.fileErr
	}
	tail := s.buf
	if len(tail) > s.maxBytes {
		tail = tail[len(tail)-s.maxBytes:]
	}
	cut := len(s.buf) - len(tail)
	// Never begin inside a UTF-8 sequence.
	for len(tail) > 0 && !utf8.RuneStart(tail[0]) {
		tail = tail[1:]
		cut++
	}
	if cut > 0 {
		result.partial = s.buf[cut-1] != '\n'
	} else {
		result.partial = s.midLine
	}
	if s.maxLines > 0 {
		// Keep the last maxLines lines; a trailing newline does not start a line.
		body := tail
		trailing := len(body) > 0 && body[len(body)-1] == '\n'
		if trailing {
			body = body[:len(body)-1]
		}
		if count := bytes.Count(
			body,
			[]byte{'\n'},
		) + 1; count > s.maxLines {
			index := len(body)
			for range s.maxLines {
				index = bytes.LastIndexByte(
					body[:index],
					'\n',
				)
			}
			tail = tail[index+1:]
			result.partial = false
		}
	}
	shown := int64(bytes.Count(
		tail,
		[]byte{'\n'},
	))
	if len(tail) > 0 && tail[len(tail)-1] != '\n' {
		shown++
	}
	result.firstLine = result.lines - shown + 1
	result.text = string(tail)
	result.shownBytes = len(tail)
	return result
}

// notice explains a truncated tail to the model, naming the full output file.
func (r tailResult) notice() string {
	if !r.truncated {
		return ""
	}
	var text strings.Builder
	fmt.Fprintf(
		&text,
		"[Output truncated: showing lines %d-%d of %d (%s of %s).",
		r.firstLine,
		r.lines,
		r.lines,
		formatSize(int64(r.shownBytes)),
		formatSize(r.total),
	)
	if r.partial {
		text.WriteString(" The first line shown is partial.")
	}
	if r.err != nil {
		fmt.Fprintf(
			&text,
			" The full output could not be saved: %v]",
			r.err,
		)
	} else {
		fmt.Fprintf(
			&text,
			" Full output: %s]",
			r.path,
		)
	}
	return text.String()
}

func formatSize(size int64) string {
	switch {
	case size < 1024:
		return fmt.Sprintf(
			"%d B",
			size,
		)
	case size < 1024*1024:
		return fmt.Sprintf(
			"%.1f KB",
			float64(size)/1024,
		)
	default:
		return fmt.Sprintf(
			"%.1f MB",
			float64(size)/(1024*1024),
		)
	}
}

// joinSections joins nonempty pieces of a tool result with newlines.
func joinSections(sections ...string) string {
	var parts []string
	for _, section := range sections {
		section = strings.TrimRight(
			section,
			"\n",
		)
		if section != "" {
			parts = append(
				parts,
				section,
			)
		}
	}
	return strings.Join(
		parts,
		"\n",
	)
}
