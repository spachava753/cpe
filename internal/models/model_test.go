package models_test

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/negrel/secrecy"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/models"
)

func TestLoadModels(t *testing.T) {
	const validJSON = `[{
		"base_url": "http://localhost:8080/v1",
		"api": "Openai",
		"id": "local-model",
		"input_modalities": ["text", "image"],
		"output_modalities": ["text"],
		"limit": 32768,
		"max_tokens": 4096
	}]`
	localModel := models.Model{
		BaseURL: url.URL{
			Scheme: "http",
			Host:   "localhost:8080",
			Path:   "/v1",
		},
		API:              models.Openai,
		ID:               "local-model",
		InputModalities:  []gai.Modality{gai.Text, gai.Image},
		OutputModalities: []gai.Modality{gai.Text},
		Limit:            32768,
		MaxTokens:        4096,
	}
	otherModel := localModel
	otherModel.ID = "other-model"
	unicodeModel := localModel
	unicodeModel.ID = "模型-🦙-�"
	secret := secrecy.NewSecretString([]byte("test-api-key"))

	type testCase struct {
		readErr          error
		wantErr          error
		name             string
		content          string
		wantErrText      string
		want             []models.Model
		failAfterContent bool
	}
	tests := []testCase{
		{
			name:    "empty model list",
			content: `[]`,
			want:    []models.Model{},
		},
		{
			name:    "one configured model",
			content: validJSON,
			want:    []models.Model{localModel},
		},
		{
			name:    "surrounding whitespace",
			content: " \n\t" + validJSON + "\r\n",
			want:    []models.Model{localModel},
		},
		{
			name:    "Unicode model ID",
			content: strings.Replace(validJSON, "local-model", "模型-🦙-�", 1),
			want:    []models.Model{unicodeModel},
		},
		{
			name:        "invalid UTF-8 in ID",
			content:     strings.Replace(validJSON, "local-model", string([]byte{0xff}), 1),
			wantErrText: "models[0]: invalid UTF-8",
		},
		{
			name:        "invalid UTF-8 in field name",
			content:     strings.Replace(validJSON, `"id"`, `"`+string([]byte{0xff})+`"`, 1),
			wantErrText: "models[0]: invalid UTF-8",
		},
		{
			name:        "invalid UTF-8 in second model rejects entire load",
			content:     strings.TrimSuffix(validJSON, "]") + "," + strings.TrimPrefix(strings.Replace(validJSON, "local-model", string([]byte{0xff}), 1), "["),
			wantErrText: "models[1]: invalid UTF-8",
		},
		{
			name:    "multiple models preserve order",
			content: strings.TrimSuffix(validJSON, "]") + "," + strings.TrimPrefix(strings.Replace(validJSON, "local-model", "other-model", 1), "["),
			want:    []models.Model{localModel, otherModel},
		},
		{
			name: "optional settings and secret",
			content: `[{
				"base_url": "https://api.example.com/v1?region=test",
				"api": "Openai",
				"api_key": "test-api-key",
				"id": "configured-model",
				"input_modalities": ["text"],
				"output_modalities": ["text"],
				"input": 1.25,
				"output": 2.5,
				"cache_read": 0.125,
				"cache_write": 0.5,
				"limit": 32768,
				"max_tokens": 4096,
				"reasoning_levels": ["low", "high"],
				"reasoning_field": "reasoning_content",
				"use_max_tokens": true,
				"extra_fields": {"temperature":0.7,"thinking":{"enabled":true},"vendor_flag":null}
			}]`,
			want: []models.Model{{
				BaseURL: url.URL{
					Scheme:   "https",
					Host:     "api.example.com",
					Path:     "/v1",
					RawQuery: "region=test",
				},
				API:              models.Openai,
				APIKey:           &secret,
				ID:               "configured-model",
				InputModalities:  []gai.Modality{gai.Text},
				OutputModalities: []gai.Modality{gai.Text},
				Input:            1.25,
				Output:           2.5,
				CacheRead:        0.125,
				CacheWrite:       0.5,
				Limit:            32768,
				MaxTokens:        4096,
				ReasoningLevels:  []string{"low", "high"},
				ReasoningField:   models.ReasoningContent,
				UseMaxTokens:     true,
				ExtraFields: map[string]json.RawMessage{
					"temperature": json.RawMessage(`0.7`),
					"thinking":    json.RawMessage(`{"enabled":true}`),
					"vendor_flag": json.RawMessage(`null`),
				},
			}},
		},
		{
			name:    "empty API key is absent",
			content: strings.Replace(validJSON, `"id": "local-model"`, `"api_key": "", "id": "local-model"`, 1),
			want:    []models.Model{localModel},
		},
		{
			name:    "empty file",
			wantErr: io.EOF,
		},
		{
			name:    "whitespace only",
			content: " \n\t",
			wantErr: io.EOF,
		},
		{
			name:        "null list",
			content:     `null`,
			wantErrText: "JSON array",
		},
		{
			name:        "object instead of array",
			content:     `{}`,
			wantErrText: "decode models",
		},
		{
			name:        "string instead of array",
			content:     `"models"`,
			wantErrText: "decode models",
		},
		{
			name:        "malformed JSON",
			content:     `[{"api":]}`,
			wantErrText: "decode models",
		},
		{
			name:        "truncated JSON",
			content:     strings.TrimSuffix(validJSON, "]"),
			wantErrText: "decode models",
		},
		{
			name:        "trailing array",
			content:     validJSON + `[]`,
			wantErrText: "trailing data",
		},
		{
			name:        "trailing null",
			content:     validJSON + `null`,
			wantErrText: "trailing data",
		},
		{
			name:        "trailing malformed data",
			content:     validJSON + `garbage`,
			wantErrText: "trailing data",
		},
		{
			name:    "read failure",
			content: validJSON,
			readErr: io.ErrClosedPipe,
			wantErr: io.ErrClosedPipe,
		},
		{
			name:             "read failure after JSON",
			content:          validJSON,
			readErr:          io.ErrClosedPipe,
			failAfterContent: true,
			wantErr:          io.ErrClosedPipe,
		},
		{
			name:        "null model entry",
			content:     `[null]`,
			wantErrText: "models[0]",
		},
		{
			name:        "non-object model entry",
			content:     `[true]`,
			wantErrText: "models[0]",
		},
		{
			name:        "empty model entry",
			content:     `[{}]`,
			wantErrText: "models[0].base_url",
		},
		{
			name:        "unknown model field",
			content:     strings.Replace(validJSON, `"api": "Openai"`, `"api": "Openai", "typo": true`, 1),
			wantErrText: `unknown field "typo"`,
		},
		{
			name:        "invalid second model rejects entire load",
			content:     strings.TrimSuffix(validJSON, "]") + "," + strings.TrimPrefix(strings.Replace(validJSON, `"Openai"`, `"unknown"`, 1), "["),
			wantErrText: "models[1].api",
		},
		{
			name:        "malformed second model rejects entire load",
			content:     strings.TrimSuffix(validJSON, "]") + `,{"api":}]`,
			wantErrText: "decode models",
		},
		{
			name:        "missing base URL",
			content:     strings.Replace(validJSON, `"base_url": "http://localhost:8080/v1",`, "", 1),
			wantErrText: "models[0].base_url",
		},
		{
			name:        "empty base URL",
			content:     strings.Replace(validJSON, `"http://localhost:8080/v1"`, `""`, 1),
			wantErrText: ".base_url",
		},
		{
			name:        "null base URL",
			content:     strings.Replace(validJSON, `"http://localhost:8080/v1"`, `null`, 1),
			wantErrText: ".base_url",
		},
		{
			name:        "relative base URL",
			content:     strings.Replace(validJSON, `"http://localhost:8080/v1"`, `"/v1"`, 1),
			wantErrText: ".base_url",
		},
		{
			name:        "unsupported URL scheme",
			content:     strings.Replace(validJSON, `"http://localhost:8080/v1"`, `"ftp://localhost/v1"`, 1),
			wantErrText: ".base_url",
		},
		{
			name:        "missing URL host",
			content:     strings.Replace(validJSON, `"http://localhost:8080/v1"`, `"https:///v1"`, 1),
			wantErrText: ".base_url",
		},
		{
			name:        "missing URL hostname",
			content:     strings.Replace(validJSON, `"http://localhost:8080/v1"`, `"http://:8080/v1"`, 1),
			wantErrText: ".base_url",
		},
		{
			name:        "invalid URL escape",
			content:     strings.Replace(validJSON, `"http://localhost:8080/v1"`, `"http://localhost/%zz"`, 1),
			wantErrText: ".base_url",
		},
		{
			name:        "invalid URL port",
			content:     strings.Replace(validJSON, `"http://localhost:8080/v1"`, `"http://localhost:invalid/v1"`, 1),
			wantErrText: ".base_url",
		},
		{
			name:        "missing API",
			content:     strings.Replace(validJSON, `"api": "Openai",`, "", 1),
			wantErrText: ".api",
		},
		{
			name:        "null API",
			content:     strings.Replace(validJSON, `"Openai"`, `null`, 1),
			wantErrText: ".api",
		},
		{
			name:        "unknown API",
			content:     strings.Replace(validJSON, `"Openai"`, `"unknown"`, 1),
			wantErrText: ".api",
		},
		{
			name:        "API names are case sensitive",
			content:     strings.Replace(validJSON, `"Openai"`, `"openai"`, 1),
			wantErrText: ".api",
		},
		{
			name:        "wrong API type",
			content:     strings.Replace(validJSON, `"Openai"`, `3`, 1),
			wantErrText: "models[0]",
		},
		{
			name:        "missing ID",
			content:     strings.Replace(validJSON, `"id": "local-model",`, "", 1),
			wantErrText: ".id",
		},
		{
			name:        "null ID",
			content:     strings.Replace(validJSON, `"local-model"`, `null`, 1),
			wantErrText: ".id",
		},
		{
			name:        "empty ID",
			content:     strings.Replace(validJSON, `"local-model"`, `""`, 1),
			wantErrText: ".id",
		},
		{
			name:        "blank ID",
			content:     strings.Replace(validJSON, `"local-model"`, `" \t"`, 1),
			wantErrText: ".id",
		},
		{
			name:        "wrong ID type",
			content:     strings.Replace(validJSON, `"local-model"`, `42`, 1),
			wantErrText: "models[0]",
		},
		{
			name:        "missing input modalities",
			content:     strings.Replace(validJSON, `"input_modalities": ["text", "image"],`, "", 1),
			wantErrText: ".input_modalities",
		},
		{
			name:        "empty input modalities",
			content:     strings.Replace(validJSON, `["text", "image"]`, `[]`, 1),
			wantErrText: ".input_modalities",
		},
		{
			name:        "null input modalities",
			content:     strings.Replace(validJSON, `["text", "image"]`, `null`, 1),
			wantErrText: ".input_modalities",
		},
		{
			name:        "unknown input modality",
			content:     strings.Replace(validJSON, `["text", "image"]`, `["text", "unknown"]`, 1),
			wantErrText: ".input_modalities[1]",
		},
		{
			name:        "null input modality",
			content:     strings.Replace(validJSON, `["text", "image"]`, `[null]`, 1),
			wantErrText: ".input_modalities[0]",
		},
		{
			name:        "missing output modalities",
			content:     strings.Replace(validJSON, `"output_modalities": ["text"],`, "", 1),
			wantErrText: ".output_modalities",
		},
		{
			name:        "empty output modalities",
			content:     strings.Replace(validJSON, `"output_modalities": ["text"]`, `"output_modalities": []`, 1),
			wantErrText: ".output_modalities",
		},
		{
			name:        "null output modalities",
			content:     strings.Replace(validJSON, `"output_modalities": ["text"]`, `"output_modalities": null`, 1),
			wantErrText: ".output_modalities",
		},
		{
			name:        "unknown output modality",
			content:     strings.Replace(validJSON, `"output_modalities": ["text"]`, `"output_modalities": ["unknown"]`, 1),
			wantErrText: ".output_modalities[0]",
		},
		{
			name:        "wrong modality type",
			content:     strings.Replace(validJSON, `["text", "image"]`, `"text"`, 1),
			wantErrText: "models[0]",
		},
		{
			name:        "missing context limit",
			content:     strings.Replace(validJSON, `"limit": 32768,`, "", 1),
			wantErrText: ".limit",
		},
		{
			name:        "zero context limit",
			content:     strings.Replace(validJSON, `"limit": 32768`, `"limit": 0`, 1),
			wantErrText: ".limit",
		},
		{
			name:        "null context limit",
			content:     strings.Replace(validJSON, `"limit": 32768`, `"limit": null`, 1),
			wantErrText: ".limit",
		},
		{
			name:        "negative context limit",
			content:     strings.Replace(validJSON, `"limit": 32768`, `"limit": -1`, 1),
			wantErrText: "models[0]",
		},
		{
			name:        "fractional context limit",
			content:     strings.Replace(validJSON, `"limit": 32768`, `"limit": 1.5`, 1),
			wantErrText: "models[0]",
		},
		{
			name:        "overflowing context limit",
			content:     strings.Replace(validJSON, `"limit": 32768`, `"limit": 18446744073709551616`, 1),
			wantErrText: "models[0]",
		},
		{
			name:        "missing generation limit",
			content:     strings.Replace(validJSON, ",\n\t\t\"max_tokens\": 4096", "", 1),
			wantErrText: ".max_tokens",
		},
		{
			name:        "zero generation limit",
			content:     strings.Replace(validJSON, `"max_tokens": 4096`, `"max_tokens": 0`, 1),
			wantErrText: ".max_tokens",
		},
		{
			name:        "null generation limit",
			content:     strings.Replace(validJSON, `"max_tokens": 4096`, `"max_tokens": null`, 1),
			wantErrText: ".max_tokens",
		},
		{
			name:        "negative generation limit",
			content:     strings.Replace(validJSON, `"max_tokens": 4096`, `"max_tokens": -1`, 1),
			wantErrText: "models[0]",
		},
		{
			name:        "negative input cost",
			content:     strings.Replace(validJSON, `"api": "Openai"`, `"api": "Openai", "input": -1`, 1),
			wantErrText: ".input",
		},
		{
			name:        "negative output cost",
			content:     strings.Replace(validJSON, `"api": "Openai"`, `"api": "Openai", "output": -1`, 1),
			wantErrText: ".output",
		},
		{
			name:        "negative cache read cost",
			content:     strings.Replace(validJSON, `"api": "Openai"`, `"api": "Openai", "cache_read": -1`, 1),
			wantErrText: ".cache_read",
		},
		{
			name:        "negative cache write cost",
			content:     strings.Replace(validJSON, `"api": "Openai"`, `"api": "Openai", "cache_write": -1`, 1),
			wantErrText: ".cache_write",
		},
		{
			name:        "overflowing cost",
			content:     strings.Replace(validJSON, `"api": "Openai"`, `"api": "Openai", "input": 1e10000`, 1),
			wantErrText: "models[0]",
		},
		{
			name:        "wrong API key type",
			content:     strings.Replace(validJSON, `"api": "Openai"`, `"api": "Openai", "api_key": 42`, 1),
			wantErrText: "models[0]",
		},
		{
			name:        "unknown reasoning field",
			content:     strings.Replace(validJSON, `"api": "Openai"`, `"api": "Openai", "reasoning_field": "unknown"`, 1),
			wantErrText: ".reasoning_field",
		},
		{
			name:        "blank reasoning level",
			content:     strings.Replace(validJSON, `"api": "Openai"`, `"api": "Openai", "reasoning_levels": ["low", " "]`, 1),
			wantErrText: ".reasoning_levels[1]",
		},
		{
			name:        "null reasoning level",
			content:     strings.Replace(validJSON, `"api": "Openai"`, `"api": "Openai", "reasoning_levels": [null]`, 1),
			wantErrText: ".reasoning_levels[0]",
		},
		{
			name:        "wrong token flag type",
			content:     strings.Replace(validJSON, `"api": "Openai"`, `"api": "Openai", "use_max_tokens": "true"`, 1),
			wantErrText: "models[0]",
		},
		{
			name:        "extra fields must be an object",
			content:     strings.Replace(validJSON, `"api": "Openai"`, `"api": "Openai", "extra_fields": []`, 1),
			wantErrText: "models[0]",
		},
		{
			name:        "null option in second model rejects entire load",
			content:     strings.TrimSuffix(validJSON, "]") + "," + strings.TrimPrefix(strings.Replace(validJSON, `"api": "Openai"`, `"api": "Openai", "input": null`, 1), "["),
			wantErrText: "models[1].input",
		},
	}

	for _, field := range []string{"input", "output", "cache_read", "cache_write", "api_key", "reasoning_levels", "reasoning_field", "use_max_tokens", "extra_fields"} {
		tests = append(tests, testCase{
			name:        "null optional field " + field,
			content:     strings.Replace(validJSON, `"api": "Openai"`, `"api": "Openai", "`+field+`": null`, 1),
			wantErrText: "models[0]." + field,
		})
	}
	for _, api := range []struct {
		name string
		want models.API
	}{
		{
			name: "Responses",
			want: models.Responses,
		},
		{
			name: "Anthropic",
			want: models.Anthropic,
		},
		{
			name: "Gemini",
			want: models.Gemini,
		},
		{
			name: "Openai",
			want: models.Openai,
		},
		{
			name: "Zai",
			want: models.Zai,
		},
		{
			name: "Moonshot",
			want: models.Moonshot,
		},
		{
			name: "Deepseek",
			want: models.Deepseek,
		},
		{
			name: "Cerebras",
			want: models.Cerebras,
		},
		{
			name: "OpenRouter",
			want: models.OpenRouter,
		},
	} {
		want := localModel
		want.API = api.want
		tests = append(tests, testCase{
			name:    "API " + api.name,
			content: strings.Replace(validJSON, `"Openai"`, `"`+api.name+`"`, 1),
			want:    []models.Model{want},
		})
	}
	for _, modality := range []struct {
		name string
		want gai.Modality
	}{
		{
			name: "text",
			want: gai.Text,
		},
		{
			name: "image",
			want: gai.Image,
		},
		{
			name: "audio",
			want: gai.Audio,
		},
		{
			name: "video",
			want: gai.Video,
		},
	} {
		want := localModel
		want.InputModalities = []gai.Modality{modality.want}
		want.OutputModalities = []gai.Modality{modality.want}
		tests = append(tests, testCase{
			name:    "modality " + modality.name,
			content: strings.ReplaceAll(strings.Replace(validJSON, `["text", "image"]`, `["text"]`, 1), `"text"`, `"`+modality.name+`"`),
			want:    []models.Model{want},
		})
	}
	for _, reasoning := range []struct {
		name string
		want models.ReasoningField
	}{
		{
			name: "reasoning_details",
			want: models.ReasoningDetails,
		},
		{
			name: "reasoning_content",
			want: models.ReasoningContent,
		},
		{
			name: "reasoning",
			want: models.Reasoning,
		},
	} {
		want := localModel
		want.ReasoningField = reasoning.want
		tests = append(tests, testCase{
			name:    "reasoning field " + reasoning.name,
			content: strings.Replace(validJSON, `"api": "Openai"`, `"api": "Openai", "reasoning_field": "`+reasoning.name+`"`, 1),
			want:    []models.Model{want},
		})
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fsys := fstest.MapFS{
				"models.json": {
					Data: []byte(test.content),
				},
			}
			file, err := fsys.Open("models.json")
			if err != nil {
				t.Fatal(err)
			}
			f := &modelFile{
				File:             file,
				readErr:          test.readErr,
				failAfterContent: test.failAfterContent,
			}
			defer f.Close()

			got, err := models.LoadModels(f)
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Errorf("LoadModels error = %v; want %v", err, test.wantErr)
				}
			} else if test.wantErrText != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErrText) {
					t.Errorf("LoadModels error = %v; want it to contain %q", err, test.wantErrText)
				}
			} else if err != nil {
				t.Errorf("LoadModels error = %v; want nil", err)
			}
			if err != nil && got != nil {
				t.Errorf("LoadModels returned %d models on error; want nil", len(got))
			}
			if diff := cmp.Diff(test.want, got, cmpopts.EquateEmpty(), cmp.Comparer(func(a, b *secrecy.SecretString) bool {
				if a == nil || b == nil {
					return a == b
				}
				return a.ExposeSecret() == b.ExposeSecret()
			})); diff != "" {
				t.Errorf("LoadModels mismatch (-want +got):\n%s", diff)
			}
			if f.closed {
				t.Error("LoadModels closed the caller's file")
			}
		})
	}
}

type modelFile struct {
	fs.File
	readErr          error
	failAfterContent bool
	closed           bool
}

func (f *modelFile) Read(data []byte) (int, error) {
	if f.readErr != nil && !f.failAfterContent {
		return 0, f.readErr
	}
	n, err := f.File.Read(data)
	if f.readErr != nil && err == io.EOF {
		return n, f.readErr
	}
	return n, err
}

func (f *modelFile) Close() error {
	f.closed = true
	return f.File.Close()
}
