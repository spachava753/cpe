package responses

import (
	"encoding/json"
	"reflect"
	"testing"

	api "github.com/openai/openai-go/v3/responses"
	"github.com/spachava753/gai"
)

func TestUsageServiceCapture(t *testing.T) {
	for _, test := range []struct {
		name, body string
		want       gai.Metadata
	}{
		{name: "absent", body: `{}`},
		{name: "null usage", body: `{"usage":null}`},
		{name: "empty usage", body: `{"usage":{}}`, want: gai.Metadata{}},
		{name: "explicit zeroes", body: `{"usage":{"input_tokens":0,"output_tokens":0}}`, want: gai.Metadata{gai.UsageMetricInputTokens: 0, gai.UsageMetricGenerationTokens: 0}},
		{name: "input only", body: `{"usage":{"input_tokens":100}}`, want: gai.Metadata{gai.UsageMetricInputTokens: 100}},
		{name: "output only", body: `{"usage":{"output_tokens":20}}`, want: gai.Metadata{gai.UsageMetricGenerationTokens: 20}},
		{name: "null input", body: `{"usage":{"input_tokens":null,"output_tokens":20}}`, want: gai.Metadata{gai.UsageMetricGenerationTokens: 20}},
		{name: "null output", body: `{"usage":{"input_tokens":100,"output_tokens":null}}`, want: gai.Metadata{gai.UsageMetricInputTokens: 100}},
		{name: "cache counters", body: `{"usage":{"input_tokens":100,"output_tokens":20,"input_tokens_details":{"cached_tokens":40,"cache_write_tokens":30}}}`, want: gai.Metadata{gai.UsageMetricInputTokens: 100, gai.UsageMetricGenerationTokens: 20, gai.UsageMetricCacheReadTokens: 40, gai.UsageMetricCacheWriteTokens: 30}},
		{name: "null cache details", body: `{"usage":{"input_tokens_details":null}}`, want: gai.Metadata{}},
		{name: "partial cache details", body: `{"usage":{"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":null}}}`, want: gai.Metadata{gai.UsageMetricCacheReadTokens: 0}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var response api.Response
			if err := json.Unmarshal([]byte(test.body), &response); err != nil {
				t.Fatal(err)
			}
			service := &usageService{}
			service.capture(response)
			if !reflect.DeepEqual(service.usage, test.want) {
				t.Fatalf("capture() = %#v, want %#v", service.usage, test.want)
			}
		})
	}
}
