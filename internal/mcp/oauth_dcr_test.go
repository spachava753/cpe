package mcp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"slices"
	"sync"
	"testing"
	"time"

	authsdk "github.com/modelcontextprotocol/go-sdk/auth"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"

	"github.com/spachava753/cpe/internal/mcpconfig"
)

func TestConnectAndListServerWithDCRProtectedHTTPServer(t *testing.T) {
	authServer := newFakeDCRAuthorizationServer(t)
	resourceServer := newDCRProtectedMCPServer(t, authServer.URL())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	serverConfig := mcpconfig.ServerConfig{
		Type:    "http",
		URL:     resourceServer.URL + "/mcp",
		Timeout: 5,
	}
	transport, err := createTransport(ctx, serverConfig, browserlessAuthorizationCodeFetcher(t))
	if err != nil {
		t.Fatalf("createTransport() error = %v", err)
	}
	session, err := NewClient().Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("client.Connect() error = %v", err)
	}
	conn := &MCPConn{ServerName: "dcr", Config: serverConfig, ClientSession: session}
	defer conn.Close()
	if err := populateConnectionTools(ctx, conn); err != nil {
		t.Fatalf("populateConnectionTools() error = %v", err)
	}

	if len(conn.Tools) != 1 {
		t.Fatalf("tools count = %d, want 1", len(conn.Tools))
	}
	if got := conn.Tools[0].Name; got != "dcr_echo" {
		t.Fatalf("tool name = %q, want dcr_echo", got)
	}
	authServer.assertDCRFlow(t, mcpOAuthRedirectURL())
}

type fakeDCRAuthorizationServer struct {
	server *httptest.Server

	mu                  sync.Mutex
	clients             map[string]fakeDCRClient
	codes               map[string]fakeDCRCode
	registrationRequest *oauthex.ClientRegistrationMetadata
	registered          bool
	authorized          bool
	tokenExchanged      bool
}

type fakeDCRClient struct {
	secret       string
	redirectURIs []string
}

type fakeDCRCode struct {
	clientID      string
	codeChallenge string
}

func newFakeDCRAuthorizationServer(t *testing.T) *fakeDCRAuthorizationServer {
	t.Helper()
	s := &fakeDCRAuthorizationServer{
		clients: make(map[string]fakeDCRClient),
		codes:   make(map[string]fakeDCRCode),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-authorization-server", s.handleMetadata)
	mux.HandleFunc("/register", s.handleRegister)
	mux.HandleFunc("/authorize", s.handleAuthorize)
	mux.HandleFunc("/token", s.handleToken)
	s.server = httptest.NewServer(mux)
	t.Cleanup(s.server.Close)
	return s
}

func (s *fakeDCRAuthorizationServer) URL() string {
	return s.server.URL
}

func (s *fakeDCRAuthorizationServer) handleMetadata(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(&oauthex.AuthServerMeta{
		Issuer:                            s.URL(),
		AuthorizationEndpoint:             s.URL() + "/authorize",
		TokenEndpoint:                     s.URL() + "/token",
		RegistrationEndpoint:              s.URL() + "/register",
		ResponseTypesSupported:            []string{"code"},
		CodeChallengeMethodsSupported:     []string{"S256"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_basic", "client_secret_post"},
	})
}

func (s *fakeDCRAuthorizationServer) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var metadata oauthex.ClientRegistrationMetadata
	if err := json.NewDecoder(r.Body).Decode(&metadata); err != nil {
		http.Error(w, "invalid client metadata", http.StatusBadRequest)
		return
	}
	if len(metadata.RedirectURIs) == 0 {
		http.Error(w, "missing redirect_uris", http.StatusBadRequest)
		return
	}

	clientID := "client_" + randomText()
	clientSecret := "secret_" + randomText()
	s.mu.Lock()
	s.registrationRequest = &metadata
	s.registered = true
	s.clients[clientID] = fakeDCRClient{secret: clientSecret, redirectURIs: slices.Clone(metadata.RedirectURIs)}
	s.mu.Unlock()

	metadata.TokenEndpointAuthMethod = "client_secret_basic"
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(&oauthex.ClientRegistrationResponse{
		ClientID:                   clientID,
		ClientSecret:               clientSecret,
		ClientRegistrationMetadata: metadata,
	})
}

func (s *fakeDCRAuthorizationServer) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	clientID := r.URL.Query().Get("client_id")
	redirectURI := r.URL.Query().Get("redirect_uri")
	codeChallenge := r.URL.Query().Get("code_challenge")
	state := r.URL.Query().Get("state")

	s.mu.Lock()
	clientInfo, ok := s.clients[clientID]
	s.mu.Unlock()
	if !ok {
		http.Error(w, "unknown client_id", http.StatusBadRequest)
		return
	}
	if !slices.Contains(clientInfo.redirectURIs, redirectURI) {
		http.Error(w, "invalid redirect_uri", http.StatusBadRequest)
		return
	}
	if codeChallenge == "" {
		http.Error(w, "missing code_challenge", http.StatusBadRequest)
		return
	}
	code := "code_" + randomText()
	s.mu.Lock()
	s.authorized = true
	s.codes[code] = fakeDCRCode{clientID: clientID, codeChallenge: codeChallenge}
	s.mu.Unlock()

	redirect, err := url.Parse(redirectURI)
	if err != nil {
		http.Error(w, "invalid redirect_uri", http.StatusBadRequest)
		return
	}
	q := redirect.Query()
	q.Set("code", code)
	q.Set("state", state)
	redirect.RawQuery = q.Encode()
	http.Redirect(w, r, redirect.String(), http.StatusFound)
}

func (s *fakeDCRAuthorizationServer) handleToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	clientID, clientSecret, ok := r.BasicAuth()
	if !ok {
		clientID = r.Form.Get("client_id")
		clientSecret = r.Form.Get("client_secret")
	}
	s.mu.Lock()
	clientInfo, clientOK := s.clients[clientID]
	codeInfo, codeOK := s.codes[r.Form.Get("code")]
	s.mu.Unlock()
	if !clientOK || clientInfo.secret != clientSecret {
		http.Error(w, "invalid client credentials", http.StatusUnauthorized)
		return
	}
	if r.Form.Get("grant_type") != "authorization_code" {
		http.Error(w, "unsupported grant_type", http.StatusBadRequest)
		return
	}
	if !codeOK || codeInfo.clientID != clientID {
		http.Error(w, "unknown authorization code", http.StatusBadRequest)
		return
	}
	if !validPKCEChallenge(r.Form.Get("code_verifier"), codeInfo.codeChallenge) {
		http.Error(w, "PKCE verification failed", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	s.tokenExchanged = true
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token": "test_access_token",
		"token_type":   "Bearer",
		"expires_in":   3600,
	})
}

func (s *fakeDCRAuthorizationServer) assertDCRFlow(t *testing.T, redirectURL string) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.registered {
		t.Fatal("auth server did not receive dynamic client registration request")
	}
	if !s.authorized {
		t.Fatal("auth server did not receive authorization request")
	}
	if !s.tokenExchanged {
		t.Fatal("auth server did not receive token exchange request")
	}
	if s.registrationRequest.ClientName != "CPE" {
		t.Fatalf("registered client_name = %q, want CPE", s.registrationRequest.ClientName)
	}
	if !slices.Contains(s.registrationRequest.RedirectURIs, redirectURL) {
		t.Fatalf("registered redirect_uris = %v, want to contain %q", s.registrationRequest.RedirectURIs, redirectURL)
	}
	if s.registrationRequest.ApplicationType != "native" {
		t.Fatalf("registered application_type = %q, want native", s.registrationRequest.ApplicationType)
	}
}

func newDCRProtectedMCPServer(t *testing.T, authorizationServerURL string) *httptest.Server {
	t.Helper()
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "dcr-test-server", Version: "1.0.0"}, nil)
	mcpsdk.AddTool[dcrEchoInput, dcrEchoOutput](server, &mcpsdk.Tool{Name: "dcr_echo", Description: "echo input"}, func(ctx context.Context, req *mcpsdk.CallToolRequest, input dcrEchoInput) (*mcpsdk.CallToolResult, dcrEchoOutput, error) {
		return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: input.Text}}}, dcrEchoOutput(input), nil
	})

	mux := http.NewServeMux()
	httpServer := httptest.NewServer(mux)
	t.Cleanup(httpServer.Close)

	resourceURL := httpServer.URL + "/mcp"
	mux.Handle("/.well-known/oauth-protected-resource/mcp", authsdk.ProtectedResourceMetadataHandler(&oauthex.ProtectedResourceMetadata{
		Resource:             resourceURL,
		AuthorizationServers: []string{authorizationServerURL},
		ScopesSupported:      []string{"read"},
	}))
	mcpHandler := mcpsdk.NewStreamableHTTPHandler(func(req *http.Request) *mcpsdk.Server { return server }, nil)
	authMiddleware := authsdk.RequireBearerToken(func(ctx context.Context, token string, req *http.Request) (*authsdk.TokenInfo, error) {
		if token != "test_access_token" {
			return nil, authsdk.ErrInvalidToken
		}
		return &authsdk.TokenInfo{Scopes: []string{"read"}, Expiration: time.Now().Add(time.Hour)}, nil
	}, &authsdk.RequireBearerTokenOptions{
		Scopes:              []string{"read"},
		ResourceMetadataURL: httpServer.URL + "/.well-known/oauth-protected-resource/mcp",
	})
	mux.Handle("/mcp", authMiddleware(mcpHandler))
	return httpServer
}

type dcrEchoInput struct {
	Text string `json:"text"`
}

type dcrEchoOutput struct {
	Text string `json:"text"`
}

func browserlessAuthorizationCodeFetcher(t *testing.T) authsdk.AuthorizationCodeFetcher {
	t.Helper()
	return func(ctx context.Context, args *authsdk.AuthorizationArgs) (*authsdk.AuthorizationResult, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, args.URL, nil)
		if err != nil {
			return nil, fmt.Errorf("creating authorization request: %w", err)
		}
		client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("visiting authorization URL: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusFound {
			dump, _ := httputil.DumpResponse(resp, true)
			return nil, fmt.Errorf("authorization endpoint returned %s: %s", resp.Status, dump)
		}
		location, err := resp.Location()
		if err != nil {
			return nil, fmt.Errorf("reading authorization redirect: %w", err)
		}
		return &authsdk.AuthorizationResult{
			Code:  location.Query().Get("code"),
			State: location.Query().Get("state"),
		}, nil
	}
}

func validPKCEChallenge(verifier, challenge string) bool {
	if verifier == "" || challenge == "" {
		return false
	}
	sha := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sha[:]) == challenge
}

func randomText() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
