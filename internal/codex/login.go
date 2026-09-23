package codex

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Login runs a browser PKCE or device-code login and saves credentials to
// authFile only after a successful exchange. notify receives display-only login
// instructions; these must never be added to model context or session history.
// Cancellation closes the callback listener and stops device polling.
func Login(ctx context.Context, authFile, method string, notify func(string)) error {
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: noRedirect}
	l := &loginClient{
		store:  &credentialStore{path: authFile, tokenURL: tokenURL, client: client},
		client: client, authorizeURL: "https://auth.openai.com/oauth/authorize",
		deviceURL:       "https://auth.openai.com/api/accounts/deviceauth/usercode",
		pollURL:         "https://auth.openai.com/api/accounts/deviceauth/token",
		verificationURL: "https://auth.openai.com/codex/device",
		redirectURI:     "http://localhost:1455/auth/callback", listenAddress: "127.0.0.1:1455",
		deviceRedirect: "https://auth.openai.com/deviceauth/callback", openBrowser: openBrowser,
		minimumInterval: time.Second,
	}
	return l.login(ctx, method, notify)
}

type loginClient struct {
	store                                             *credentialStore
	client                                            *http.Client
	authorizeURL, deviceURL, pollURL, verificationURL string
	redirectURI, listenAddress, deviceRedirect        string
	openBrowser                                       func(context.Context, string) error
	minimumInterval                                   time.Duration
}

func (l *loginClient) login(ctx context.Context, method string, notify func(string)) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	if notify == nil {
		notify = func(string) {}
	}
	var cred credential
	var err error
	switch method {
	case "browser":
		cred, err = l.browser(ctx, notify)
	case "device":
		cred, err = l.device(ctx, notify)
	default:
		return errors.New("use /login or /login device")
	}
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return l.store.save(ctx, cred)
}

func (l *loginClient) browser(ctx context.Context, notify func(string)) (credential, error) {
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return credential{}, err
	}
	verifier := base64.RawURLEncoding.EncodeToString(random[:])
	challenge := sha256.Sum256([]byte(verifier))
	state := rand.Text()
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp4", l.listenAddress)
	if err != nil {
		return credential{}, errors.New("cannot listen on localhost:1455; close the other login flow or use /login device")
	}
	redirect := l.redirectURI
	if redirect == "" {
		redirect = "http://" + listener.Addr().String() + "/auth/callback"
	}
	type callback struct {
		code string
		err  error
	}
	result := make(chan callback, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /auth/callback", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'")
		q := r.URL.Query()
		if len(q["state"]) != 1 || subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(state)) != 1 {
			http.Error(w, "Invalid login state. Return to the original login window.", http.StatusBadRequest)
			return
		}
		if q.Get("error") != "" {
			select {
			case result <- callback{err: errors.New("login was denied; use /login to try again")}:
			default:
			}
			http.Error(w, "Login was denied. Return to CPE.", http.StatusBadRequest)
			return
		}
		if len(q["code"]) != 1 || q.Get("code") == "" {
			http.Error(w, "Missing authorization code.", http.StatusBadRequest)
			return
		}
		select {
		case result <- callback{code: q.Get("code")}:
		default:
		}
		fmt.Fprint(w, "Authorization received. Return to CPE to finish signing in. You can close this window.")
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, MaxHeaderBytes: 16384, ErrorLog: log.New(io.Discard, "", 0)}
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.Serve(listener) }()
	defer server.Close()
	authURL, err := url.Parse(l.authorizeURL)
	if err != nil {
		return credential{}, err
	}
	authURL.RawQuery = url.Values{
		"response_type": {"code"}, clientIDKey: {clientID}, "redirect_uri": {redirect},
		"scope": {"openid profile email offline_access"}, "state": {state},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(challenge[:])}, "code_challenge_method": {"S256"},
		"id_token_add_organizations": {"true"}, "codex_cli_simplified_flow": {"true"}, "originator": {"cpe"},
	}.Encode()
	notify("Sign in to OpenAI Codex\n\nComplete login in your browser. If it does not open, open this URL:\n\n" + authURL.String() + "\n\nWaiting for the browser callback. Esc cancels. For a remote terminal, cancel and use /login device.")
	if l.openBrowser != nil {
		if err := l.openBrowser(ctx, authURL.String()); err != nil && ctx.Err() == nil {
			notify("The browser could not be opened automatically. Open the URL above to continue.")
		}
	}
	select {
	case <-ctx.Done():
		return credential{}, ctx.Err()
	case err := <-serverDone:
		return credential{}, fmt.Errorf("login callback server stopped: %w", err)
	case callback := <-result:
		if callback.err != nil {
			return credential{}, callback.err
		}
		return l.exchange(ctx, callback.code, verifier, redirect)
	}
}

func (l *loginClient) exchange(ctx context.Context, code, verifier, redirect string) (credential, error) {
	form := url.Values{"grant_type": {"authorization_code"}, clientIDKey: {clientID}, "code": {code}, "code_verifier": {verifier}, "redirect_uri": {redirect}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, l.store.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return credential{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := l.client.Do(req)
	if err != nil {
		return credential{}, fmt.Errorf("login token exchange failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return credential{}, fmt.Errorf("login token exchange failed (HTTP %d); use /login to try again", resp.StatusCode)
	}
	var token struct {
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
		Expires int64  `json:"expires_in"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&token) != nil || !validHeaderValue(token.Access) || !validHeaderValue(token.Refresh) || token.Expires <= 0 || token.Expires > 366*24*3600 {
		return credential{}, errors.New("invalid login token response; credentials were not saved")
	}
	account := accountFromToken(token.Access)
	if !validHeaderValue(account) {
		return credential{}, errors.New("login token has no account ID; credentials were not saved")
	}
	return credential{access: token.Access, refresh: token.Refresh, accountID: account, expires: time.Now().Add(time.Duration(token.Expires) * time.Second).UnixMilli()}, nil
}

func (l *loginClient) post(ctx context.Context, endpoint string, body any) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return l.client.Do(req)
}

func (l *loginClient) device(ctx context.Context, notify func(string)) (credential, error) {
	resp, err := l.post(ctx, l.deviceURL, map[string]string{clientIDKey: clientID})
	if err != nil {
		return credential{}, err
	}
	var device struct {
		ID       string          `json:"device_auth_id"`
		Code     string          `json:"user_code"`
		Interval json.RawMessage `json:"interval"`
	}
	decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&device)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return credential{}, fmt.Errorf("device login unavailable (HTTP %d); use /login for browser login", resp.StatusCode)
	}
	seconds, err := strconv.ParseFloat(strings.Trim(string(device.Interval), `"`), 64)
	if decodeErr != nil || err != nil || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 || seconds > 60 || device.ID == "" || device.Code == "" {
		return credential{}, errors.New("invalid device login response")
	}
	interval := max(l.minimumInterval, time.Duration(seconds*float64(time.Second)))
	notify("Sign in to OpenAI Codex\n\nOpen " + l.verificationURL + "\n\nEnter code: " + device.Code + "\n\nWaiting for authorization. Esc cancels. This code expires in 15 minutes.")
	for {
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return credential{}, ctx.Err()
		case <-timer.C:
		}
		resp, err := l.post(ctx, l.pollURL, map[string]string{"device_auth_id": device.ID, "user_code": device.Code})
		if err != nil {
			return credential{}, err
		}
		var result struct {
			Code     string          `json:"authorization_code"`
			Verifier string          `json:"code_verifier"`
			Error    json.RawMessage `json:"error"`
		}
		err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result)
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			if err != nil || result.Code == "" || result.Verifier == "" {
				return credential{}, errors.New("invalid device authorization response")
			}
			return l.exchange(ctx, result.Code, result.Verifier, l.deviceRedirect)
		}
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
			continue
		}
		var code string
		if json.Unmarshal(result.Error, &code) != nil {
			var nested struct {
				Code string `json:"code"`
			}
			_ = json.Unmarshal(result.Error, &nested)
			code = nested.Code
		}
		switch code {
		case "deviceauth_authorization_pending", "authorization_pending":
			continue
		case "slow_down":
			interval += 5 * time.Second
		default:
			return credential{}, fmt.Errorf("device authorization failed (HTTP %d); use /login to try again", resp.StatusCode)
		}
	}
}

func openBrowser(ctx context.Context, target string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var command string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		command, args = "open", []string{target}
	case windowsOS:
		command, args = "rundll32", []string{"url.dll,FileProtocolHandler", target}
	default:
		command, args = "xdg-open", []string{target}
	}
	return exec.CommandContext(ctx, command, args...).Run()
}
