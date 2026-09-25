package copilot

import (
	"net/http"
	"strings"
	"testing"
)

func TestDiscover(t *testing.T) {
	for _, test := range []struct {
		name, item, protocol string
		empty                bool
		nonstreaming         bool
	}{
		{name: "chat", item: fixtureModel, protocol: chatProtocol},
		{name: "nonstreaming model", item: strings.Replace(fixtureModel, `"streaming":true`, `"streaming":false`, 1), protocol: chatProtocol, nonstreaming: true},
		{name: "omitted streaming", item: strings.Replace(fixtureModel, `,"streaming":true`, "", 1), protocol: chatProtocol, nonstreaming: true},
		{name: "legacy omitted endpoints", item: strings.Replace(fixtureModel, `"supported_endpoints":["/chat/completions"],`, "", 1), protocol: chatProtocol},
		{name: "null endpoints", item: strings.Replace(fixtureModel, `["/chat/completions"]`, `null`, 1), protocol: chatProtocol},
		{name: "explicit empty endpoints", item: strings.Replace(fixtureModel, `["/chat/completions"]`, `[]`, 1), empty: true},
		{name: "responses", item: strings.Replace(fixtureModel, "/chat/completions", "/responses", 1), protocol: responsesProtocol},
		{name: "messages", item: strings.Replace(fixtureModel, "/chat/completions", "/v1/messages", 1), protocol: messagesProtocol},
		{name: "protocol preference", item: strings.Replace(fixtureModel, `"/chat/completions"`, `"/chat/completions","/responses"`, 1), protocol: responsesProtocol},
		{name: "disabled policy", item: strings.Replace(fixtureModel, `"id":`, `"policy":{"state":"disabled"},"id":`, 1), empty: true},
		{name: "requires policy consent", item: strings.Replace(fixtureModel, `"id":`, `"policy":{"state":"unconfigured"},"id":`, 1), empty: true},
		{name: "not selectable", item: strings.Replace(fixtureModel, `"model_picker_enabled":true`, `"model_picker_enabled":false`, 1), empty: true},
		{name: "no tools", item: strings.Replace(fixtureModel, `"tool_calls":true`, `"tool_calls":false`, 1), empty: true},
		{name: "unknown endpoint", item: strings.Replace(fixtureModel, "/chat/completions", "/unknown", 1), empty: true},
		{name: "bad limits", item: strings.Replace(fixtureModel, `"max_prompt_tokens":180000`, `"max_prompt_tokens":0`, 1), empty: true},
		{name: "no input room", item: strings.Replace(fixtureModel, `"max_context_window_tokens":200000`, `"max_context_window_tokens":100`, 1), empty: true},
		{name: "malformed item", item: `{"id":42}`, empty: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := newClient()
			client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != apiURL+"/models" || r.Header.Get("Authorization") != "Bearer fixture-copilot-secret" {
					t.Error("catalog route")
				}
				return jsonResponse(200, `{"data":[null,`+test.item+`]}`), nil
			})
			models, err := discover(t.Context(), client, fixtureCredential())
			if (err != nil) != test.empty {
				t.Fatalf("discover: %v", err)
			}
			if test.empty {
				return
			}
			model := models[credentialName+"/fixture"]
			if len(models) != 1 || model.Provider != test.protocol || model.Credential != credentialName || model.ID != "fixture" || model.ContextWindow != 128000 || model.MaxOutputTokens != 16384 || model.ReasoningEffort != "" || model.Cost != nil || model.CopilotStreaming == test.nonstreaming {
				t.Fatalf("profile: %+v", model)
			}
		})
	}
}

func TestParseAPIURL(t *testing.T) {
	for _, test := range []struct{ name, url, want string }{
		{"default", apiURL, apiURL}, {"individual", "https://api.individual.githubcopilot.com/", "https://api.individual.githubcopilot.com"}, {"business", "https://api.business.githubcopilot.com", "https://api.business.githubcopilot.com"}, {"enterprise", "https://api.enterprise.githubcopilot.com", "https://api.enterprise.githubcopilot.com"},
		{"http", "http://api.githubcopilot.com", ""}, {"unrelated", "https://example.com", ""}, {"suffix attack", "https://api.githubcopilot.com.evil.example", ""}, {"userinfo", "https://secret@api.githubcopilot.com", ""}, {"port", "https://api.githubcopilot.com:444", ""}, {"path", apiURL + "/models", ""}, {"query", apiURL + "?x=y", ""}, {"empty query", apiURL + "?", ""}, {"fragment", apiURL + "#x", ""}, {"escaped slash", apiURL + "/%2f", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseAPIURL(test.url)
			if got != test.want || (err != nil) != (test.want == "") {
				t.Fatalf("endpoint=%q err=%v", got, err)
			}
		})
	}
}
