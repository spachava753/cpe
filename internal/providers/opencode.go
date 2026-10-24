package providers

import (
	"net/http"

	"github.com/spachava753/cpe/internal/conversation"
	"github.com/spachava753/cpe/internal/version"
)

type openCodeTransport struct {
	base      http.RoundTripper
	sessionID conversation.ID
}

func (t openCodeTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Scheme != httpsScheme || request.URL.Host != "opencode.ai" {
		return t.base.RoundTrip(request)
	}
	copy := request.Clone(request.Context())
	copy.Header.Set(
		"X-Opencode-Session",
		string(t.sessionID),
	)
	copy.Header.Set(
		"User-Agent",
		"cpe/"+version.Get(),
	)
	return t.base.RoundTrip(copy)
}
