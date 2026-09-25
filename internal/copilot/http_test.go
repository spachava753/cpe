package copilot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

func TestReadJSON(t *testing.T) {
	for _, test := range []struct {
		name    string
		body    string
		readErr error
		wantErr string
		cause   error
	}{
		{name: "valid", body: `{"value":"ok"}`},
		{name: "invalid JSON", body: `{"secret"`, wantErr: "invalid GitHub Copilot response"},
		{name: "oversized", body: strings.Repeat(" ", (4<<20)+1), wantErr: "invalid GitHub Copilot response"},
		{name: "read error omits detail", readErr: errors.New("secret"), wantErr: "invalid GitHub Copilot response"},
		{name: "canceled during body read", readErr: fmt.Errorf("secret: %w", context.Canceled), wantErr: context.Canceled.Error(), cause: context.Canceled},
		{name: "deadline during body read", readErr: fmt.Errorf("secret: %w", context.DeadlineExceeded), wantErr: context.DeadlineExceeded.Error(), cause: context.DeadlineExceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			resp := jsonResponse(200, test.body)
			if test.readErr != nil {
				resp.Body = io.NopCloser(iotest.ErrReader(test.readErr))
			}
			var got struct {
				Value string `json:"value"`
			}
			err := readJSON(resp, &got)
			if test.wantErr == "" {
				if err != nil || got.Value != "ok" {
					t.Fatalf("decoded value=%q err=%v", got.Value, err)
				}
				return
			}
			if err == nil || err.Error() != test.wantErr || (test.cause != nil && !errors.Is(err, test.cause)) {
				t.Fatalf("readJSON error=%v; want %q with cause %v", err, test.wantErr, test.cause)
			}
		})
	}
}
