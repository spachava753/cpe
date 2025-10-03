package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	jsonpatch "github.com/evanphx/json-patch/v5"
	"github.com/spachava753/cpe/internal/config"
)

type PatchTransport struct {
	base        http.RoundTripper
	jsonPatches []jsonpatch.Patch
	headers     map[string]string
}

func NewPatchTransport(base http.RoundTripper, patches []jsonpatch.Patch, headers map[string]string) *PatchTransport {
	if base == nil {
		base = http.DefaultTransport
	}
	return &PatchTransport{
		base:        base,
		jsonPatches: patches,
		headers:     headers,
	}
}

func (t *PatchTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	for key, value := range t.headers {
		req.Header.Set(key, value)
	}

	if req.Body != nil && len(t.jsonPatches) > 0 {
		contentType := req.Header.Get("Content-Type")
		if contentType == "application/json" || contentType == "" {
			body, err := io.ReadAll(req.Body)
			if err != nil {
				return nil, fmt.Errorf("reading request body: %w", err)
			}
			req.Body.Close()

			for _, patch := range t.jsonPatches {
				modified, err := patch.Apply(body)
				if err != nil {
					return nil, fmt.Errorf("applying JSON patch: %w", err)
				}
				body = modified
			}

			req.Body = io.NopCloser(bytes.NewReader(body))
			req.ContentLength = int64(len(body))
		}
	}

	return t.base.RoundTrip(req)
}

func BuildPatchTransportFromConfig(base http.RoundTripper, patchConfig *config.PatchRequestConfig) (http.RoundTripper, error) {
	if patchConfig == nil {
		return base, nil
	}

	var patches []jsonpatch.Patch
	if len(patchConfig.JSONPatch) > 0 {
		patchJSON, err := json.Marshal(patchConfig.JSONPatch)
		if err != nil {
			return nil, fmt.Errorf("marshaling JSON patch configuration: %w", err)
		}

		patch, err := jsonpatch.DecodePatch(patchJSON)
		if err != nil {
			return nil, fmt.Errorf("decoding JSON patch: %w", err)
		}
		patches = append(patches, patch)
	}

	if len(patches) == 0 && len(patchConfig.IncludeHeaders) == 0 {
		return base, nil
	}

	return NewPatchTransport(base, patches, patchConfig.IncludeHeaders), nil
}
