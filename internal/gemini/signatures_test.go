package gemini

import (
	"encoding/base64"
	"io"
	"reflect"
	"strings"
	"testing"
)

func TestSignatureBody(t *testing.T) {
	signature := base64.StdEncoding.EncodeToString([]byte("authentic"))
	event := `data: {"candidates":[{"content":{"parts":[{"functionCall":{"name":"first","args":{}},"thoughtSignature":"` + signature + `"},{"functionCall":{"name":"second","args":{}}}]}}]}`
	for _, test := range []struct {
		name, text string
		size       int
	}{
		{"LF", event + "\n\n", 4096}, {"CRLF", event + "\r\n\r\n", 4096},
		{"split CRLF", event + "\r\n\r\n", 1}, {"EOF", event, 7},
		{"pretty JSON", strings.ReplaceAll(event, "{", "{\n") + "\n\n", 11},
	} {
		t.Run(test.name, func(t *testing.T) {
			signatures := &signatureStream{}
			body := &signatureBody{ReadCloser: io.NopCloser(strings.NewReader(test.text)), signatures: signatures}
			defer body.Close()
			var got strings.Builder
			buffer := make([]byte, test.size)
			for {
				n, err := body.Read(buffer)
				got.Write(buffer[:n])
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			if got.String() != test.text {
				t.Fatal("changed wire bytes")
			}
			if !reflect.DeepEqual(signatures.queue, []string{signature, "", "", ""}) {
				t.Fatalf("signature alignment=%v", signatures.queue)
			}
		})
	}
}
