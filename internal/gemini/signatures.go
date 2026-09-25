package gemini

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"

	"google.golang.org/genai"
)

// signatureStream observes the same complete SSE data events consumed by genai.
// It leaves wire bytes, errors, cancellation, and closing with the SDK. Each
// instance belongs to one Stream iteration, so concurrent requests cannot mix
// signatures. Entries correspond to gai's function-name and argument chunks.
type signatureStream struct {
	queue []string
}

type signatureKey struct{}
type signatureTransport struct{ base http.RoundTripper }

func (t signatureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	s, _ := req.Context().Value(signatureKey{}).(*signatureStream)
	if s != nil && err == nil && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		// Retries replace a failed attempt; its unconsumed metadata is obsolete.
		s.queue = nil
		resp.Body = &signatureBody{ReadCloser: resp.Body, signatures: s}
	}
	return resp, err
}

func (s *signatureStream) observe(line []byte) {
	data, ok := bytes.CutPrefix(line, []byte("data:"))
	if !ok {
		return
	}
	var response genai.GenerateContentResponse
	if json.Unmarshal(data, &response) != nil || len(response.Candidates) != 1 || response.Candidates[0] == nil || response.Candidates[0].Content == nil {
		return // The SDK handles malformed or unsupported responses.
	}
	for _, part := range response.Candidates[0].Content.Parts {
		if part == nil || part.FunctionCall == nil || part.Text != "" || part.InlineData != nil {
			continue
		}
		signature := base64.StdEncoding.EncodeToString(part.ThoughtSignature)
		if part.FunctionCall.Name != "" {
			s.queue = append(s.queue, signature)
			signature = ""
		}
		if part.FunctionCall.Args != nil {
			s.queue = append(s.queue, signature)
		}
	}
}

func (s *signatureStream) take() string {
	if len(s.queue) == 0 {
		return ""
	}
	signature := s.queue[0]
	s.queue = s.queue[1:]
	return signature
}

type signatureBody struct {
	io.ReadCloser
	signatures *signatureStream
	pending    []byte
	scan       int
}

func (b *signatureBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.pending = append(b.pending, p[:n]...)
	for {
		// Match the event delimiters accepted by genai's scanner. Start just
		// before the new bytes, avoiding repeated scans of large media events.
		i, size := bytes.Index(b.pending[b.scan:], []byte("\n\n")), 2
		if j := bytes.Index(b.pending[b.scan:], []byte("\r\n\r\n")); j >= 0 && (i < 0 || j < i) {
			i, size = j, 4
		}
		if i < 0 {
			break
		}
		i += b.scan
		b.signatures.observe(b.pending[:i])
		b.pending = b.pending[i+size:]
		b.scan = 0
	}
	if err != nil {
		b.signatures.observe(b.pending)
		b.pending = nil
	}
	b.scan = max(0, len(b.pending)-3)
	return n, err
}
