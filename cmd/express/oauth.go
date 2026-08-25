package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultOIDCDiscoveryURL = "https://solian.app/.well-known/openid-configuration"

const defaultOAuthClientID = "solsynth-express"

type oidcConfiguration struct {
	DeviceAuthorizationEndpoint string `json:"device_authorization_endpoint"`
	TokenEndpoint               string `json:"token_endpoint"`
}

type deviceAuthorization struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

type oauthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

type oauthErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

func generatePKCE() (verifier, challenge string, err error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", "", fmt.Errorf("generate PKCE verifier: %w", err)
	}
	verifier = base64.RawURLEncoding.EncodeToString(bytes)
	digest := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(digest[:])
	return verifier, challenge, nil
}

func discoverOIDC(ctx context.Context, client *http.Client, discoveryURL string) (oidcConfiguration, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return oidcConfiguration{}, fmt.Errorf("create OIDC discovery request: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return oidcConfiguration{}, fmt.Errorf("request OIDC discovery: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return oidcConfiguration{}, fmt.Errorf("OIDC discovery returned %s", response.Status)
	}
	var configuration oidcConfiguration
	if err := json.NewDecoder(response.Body).Decode(&configuration); err != nil {
		return oidcConfiguration{}, fmt.Errorf("decode OIDC discovery: %w", err)
	}
	if configuration.DeviceAuthorizationEndpoint == "" || configuration.TokenEndpoint == "" {
		return oidcConfiguration{}, errors.New("OIDC discovery is missing device_authorization_endpoint or token_endpoint")
	}
	return configuration, nil
}

func requestDeviceAuthorization(ctx context.Context, client *http.Client, endpoint, clientID, scope, challenge string) (deviceAuthorization, error) {
	form := url.Values{
		"client_id":             {clientID},
		"scope":                 {scope},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	var authorization deviceAuthorization
	if err := oauthFormRequest(ctx, client, endpoint, form, &authorization); err != nil {
		return deviceAuthorization{}, fmt.Errorf("request device authorization: %w", err)
	}
	if authorization.DeviceCode == "" || authorization.UserCode == "" || authorization.ExpiresIn <= 0 {
		return deviceAuthorization{}, errors.New("device authorization response is missing device_code, user_code, or expires_in")
	}
	return authorization, nil
}

func pollDeviceToken(ctx context.Context, client *http.Client, endpoint, clientID, deviceCode, verifier string, interval, expiresIn int) (oauthTokenResponse, error) {
	if interval < 1 {
		interval = 5
	}
	deadline := time.NewTimer(time.Duration(expiresIn) * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return oauthTokenResponse{}, ctx.Err()
		case <-deadline.C:
			return oauthTokenResponse{}, errors.New("device authorization expired before login completed")
		case <-ticker.C:
		}

		form := url.Values{
			"grant_type":    {"urn:ietf:params:oauth:grant-type:device_code"},
			"client_id":     {clientID},
			"device_code":   {deviceCode},
			"code_verifier": {verifier},
		}
		var token oauthTokenResponse
		err := oauthFormRequest(ctx, client, endpoint, form, &token)
		if err == nil {
			if token.AccessToken == "" {
				return oauthTokenResponse{}, errors.New("token response is missing access_token")
			}
			return token, nil
		}
		var pending *oauthPendingError
		if errors.As(err, &pending) {
			if pending.Code == "slow_down" {
				interval += 5
				ticker.Reset(time.Duration(interval) * time.Second)
			}
			continue
		}
		return oauthTokenResponse{}, err
	}
}

type oauthPendingError struct {
	Code string
}

func (e *oauthPendingError) Error() string { return e.Code }

func oauthFormRequest(ctx context.Context, client *http.Client, endpoint string, form url.Values, output any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("create OAuth request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("request OAuth endpoint: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("read OAuth response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		var oauthError oauthErrorResponse
		if json.Unmarshal(body, &oauthError) == nil && oauthError.Error != "" {
			if oauthError.Error == "authorization_pending" || oauthError.Error == "slow_down" {
				return &oauthPendingError{Code: oauthError.Error}
			}
			description := strings.TrimSpace(oauthError.ErrorDescription)
			if description != "" {
				return fmt.Errorf("OAuth error %s: %s", oauthError.Error, description)
			}
			return fmt.Errorf("OAuth error %s", oauthError.Error)
		}
		return fmt.Errorf("OAuth endpoint returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, output); err != nil {
		return fmt.Errorf("decode OAuth response: %w", err)
	}
	return nil
}

func deviceLogin(ctx context.Context, client *http.Client, discoveryURL, clientID, scope string, prompts io.Writer) (oauthTokenResponse, error) {
	if strings.TrimSpace(clientID) == "" {
		return oauthTokenResponse{}, errors.New("OAuth client ID is required; pass --client-id or set SOL_SYNTH_EXPRESS_CLIENT_ID")
	}
	if strings.TrimSpace(scope) == "" {
		scope = "*"
	}
	configuration, err := discoverOIDC(ctx, client, discoveryURL)
	if err != nil {
		return oauthTokenResponse{}, err
	}
	verifier, challenge, err := generatePKCE()
	if err != nil {
		return oauthTokenResponse{}, err
	}
	authorization, err := requestDeviceAuthorization(ctx, client, configuration.DeviceAuthorizationEndpoint, clientID, scope, challenge)
	if err != nil {
		return oauthTokenResponse{}, err
	}
	if authorization.VerificationURIComplete != "" {
		if err := statusLine(prompts, ansiGreen+ansiBold, "Open this URL to sign in:"); err != nil {
			return oauthTokenResponse{}, err
		}
		if err := statusLine(prompts, ansiCyan, authorization.VerificationURIComplete); err != nil {
			return oauthTokenResponse{}, err
		}
	} else {
		if err := statusLine(prompts, ansiGreen+ansiBold, "Open this URL and enter the code:"); err != nil {
			return oauthTokenResponse{}, err
		}
		if err := statusLine(prompts, ansiCyan, fmt.Sprintf("%s  %s", authorization.VerificationURI, authorization.UserCode)); err != nil {
			return oauthTokenResponse{}, err
		}
	}
	if err := statusLine(prompts, ansiYellow, fmt.Sprintf("Waiting for authorization (expires in %s)...", time.Duration(authorization.ExpiresIn)*time.Second)); err != nil {
		return oauthTokenResponse{}, err
	}
	return pollDeviceToken(ctx, client, configuration.TokenEndpoint, clientID, authorization.DeviceCode, verifier, authorization.Interval, authorization.ExpiresIn)
}

func tokenExpiry(expiresIn int) int64 {
	if expiresIn <= 0 {
		return 0
	}
	return time.Now().Add(time.Duration(expiresIn) * time.Second).Unix()
}
