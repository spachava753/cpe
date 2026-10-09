package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
	"unicode"
)

type deviceResponse struct {
	DeviceAuthID string          `json:"device_auth_id"`
	UserCode     string          `json:"user_code"`
	Interval     json.RawMessage `json:"interval"`
}

type deviceCode struct {
	id       string
	userCode string
	interval time.Duration
}

type deviceAuthorization struct {
	Code     string `json:"authorization_code"`
	Verifier string `json:"code_verifier"`
}

func (client openAIClient) deviceLogin(ctx context.Context, output io.Writer) (OAuthCredentials, error) {
	ctx, cancel := context.WithTimeout(
		ctx,
		loginTimeout,
	)
	defer cancel()
	response, err := client.post(
		ctx,
		"/api/accounts/deviceauth/usercode",
		"application/json",
		bytes.NewBufferString(`{"client_id":"`+openAIClientID+`"}`),
	)
	if err != nil {
		return OAuthCredentials{}, err
	}
	var issued deviceResponse
	err = func() error {
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf(
				"OpenAI device login unavailable (HTTP %d); enable device-code authorization in ChatGPT security settings or use browser login",
				response.StatusCode,
			)
		}
		return decodeAuthResponse(
			response.Body,
			&issued,
		)
	}()
	if err != nil {
		return OAuthCredentials{}, err
	}
	device, err := issued.parse()
	if err != nil {
		return OAuthCredentials{}, err
	}
	if _, err := fmt.Fprintf(
		output,
		"Open %s/codex/device in your browser and enter code: %s\nWaiting for ChatGPT authorization (Ctrl+C cancels).\n",
		client.issuer,
		device.userCode,
	); err != nil {
		return OAuthCredentials{}, fmt.Errorf(
			"display device code: %w",
			err,
		)
	}
	grant, err := client.pollDevice(
		ctx,
		device,
	)
	if err != nil {
		return OAuthCredentials{}, err
	}
	return client.exchange(
		ctx,
		grant.Code,
		grant.Verifier,
		client.issuer+"/deviceauth/callback",
	)
}

func (issued deviceResponse) parse() (deviceCode, error) {
	if issued.DeviceAuthID == "" || issued.UserCode == "" {
		return deviceCode{}, errors.New("OpenAI returned an incomplete device code")
	}
	for _, character := range issued.UserCode {
		if unicode.IsControl(character) {
			return deviceCode{}, errors.New("OpenAI returned an invalid device code")
		}
	}
	seconds := int64(5)
	if len(issued.Interval) != 0 {
		var value any
		decoder := json.NewDecoder(bytes.NewReader(issued.Interval))
		decoder.UseNumber()
		if err := decoder.Decode(&value); err != nil {
			return deviceCode{}, errors.New("OpenAI returned an invalid polling interval")
		}
		switch interval := value.(type) {
		case string:
			parsed, err := strconv.ParseInt(
				interval,
				10,
				64,
			)
			if err != nil {
				return deviceCode{}, errors.New("OpenAI returned an invalid polling interval")
			}
			seconds = parsed
		case json.Number:
			parsed, err := interval.Int64()
			if err != nil {
				return deviceCode{}, errors.New("OpenAI returned an invalid polling interval")
			}
			seconds = parsed
		default:
			return deviceCode{}, errors.New("OpenAI returned an invalid polling interval")
		}
	}
	if seconds < 1 || seconds > int64(loginTimeout/time.Second) {
		return deviceCode{}, errors.New("OpenAI returned an invalid polling interval")
	}
	return deviceCode{
		id:       issued.DeviceAuthID,
		userCode: issued.UserCode,
		interval: time.Duration(seconds) * time.Second,
	}, nil
}

func (client openAIClient) pollDevice(ctx context.Context, device deviceCode) (deviceAuthorization, error) {
	body, err := json.Marshal(struct {
		ID   string `json:"device_auth_id"`
		Code string `json:"user_code"`
	}{
		ID:   device.id,
		Code: device.userCode,
	})
	if err != nil {
		return deviceAuthorization{}, errors.New("encode device authorization")
	}
	defer clear(body)
	for {
		response, err := client.post(
			ctx,
			"/api/accounts/deviceauth/token",
			"application/json",
			bytes.NewReader(body),
		)
		if err != nil {
			return deviceAuthorization{}, err
		}
		var grant deviceAuthorization
		status := response.StatusCode
		err = func() error {
			defer response.Body.Close()
			if status != http.StatusOK {
				return nil
			}
			return decodeAuthResponse(
				response.Body,
				&grant,
			)
		}()
		if err != nil {
			return deviceAuthorization{}, err
		}
		switch status {
		case http.StatusOK:
			if grant.Code == "" || grant.Verifier == "" {
				return deviceAuthorization{}, errors.New("OpenAI returned incomplete device authorization")
			}
			return grant, nil
		case http.StatusForbidden, http.StatusNotFound:
			// These statuses mean authorization is still pending in OpenAI's protocol.
		default:
			return deviceAuthorization{}, fmt.Errorf(
				"OpenAI device authorization failed (HTTP %d)",
				status,
			)
		}
		timer := time.NewTimer(device.interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return deviceAuthorization{}, fmt.Errorf(
				"wait for device authorization: %w",
				ctx.Err(),
			)
		case <-timer.C:
		}
	}
}
