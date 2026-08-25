package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeviceLoginUsesWildcardScopeAndPKCE(t *testing.T) {
	var challenge string
	var verifier string
	polls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_, _ = io.WriteString(w, `{"device_authorization_endpoint":"`+"http://"+r.Host+`/device","token_endpoint":"`+"http://"+r.Host+`/token"}`)
		case "/device":
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.Form.Get("scope") != "*" || r.Form.Get("client_id") != "express-test" || r.Form.Get("code_challenge_method") != "S256" {
				t.Fatalf("device form = %s", r.Form.Encode())
			}
			challenge = r.Form.Get("code_challenge")
			_, _ = io.WriteString(w, `{"device_code":"device-1","user_code":"ABCD-EFGH","verification_uri":"https://id.solian.app/device","expires_in":10,"interval":1}`)
		case "/token":
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			verifier = r.Form.Get("code_verifier")
			polls++
			if polls == 1 {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"error":"authorization_pending"}`)
				return
			}
			_, _ = io.WriteString(w, `{"access_token":"access-1","refresh_token":"refresh-1","token_type":"Bearer","expires_in":3600,"scope":"*"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	var prompts strings.Builder
	tokens, err := deviceLogin(context.Background(), http.DefaultClient, server.URL+"/.well-known/openid-configuration", "express-test", "*", &prompts)
	if err != nil {
		t.Fatal(err)
	}
	if tokens.AccessToken != "access-1" || tokens.RefreshToken != "refresh-1" || !strings.Contains(prompts.String(), "ABCD-EFGH") {
		t.Fatalf("tokens=%#v prompts=%q", tokens, prompts.String())
	}
	digest := sha256.Sum256([]byte(verifier))
	if challenge != base64.RawURLEncoding.EncodeToString(digest[:]) {
		t.Fatalf("challenge does not match verifier")
	}
}

func TestAuthLoginStoresDeviceToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_, _ = io.WriteString(w, `{"device_authorization_endpoint":"`+"http://"+r.Host+`/device","token_endpoint":"`+"http://"+r.Host+`/token"}`)
		case "/device":
			_, _ = io.WriteString(w, `{"device_code":"device-1","user_code":"CODE","verification_uri_complete":"https://id.solian.app/device?code=CODE","expires_in":10,"interval":1}`)
		case "/token":
			_, _ = io.WriteString(w, `{"access_token":"access-1","refresh_token":"refresh-1","expires_in":3600}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "auth.json")
	var output strings.Builder
	args := []string{"--config", configPath, "--base-url", "https://distribution.example", "--client-id", "express-test", "--oidc-discovery-url", server.URL + "/.well-known/openid-configuration", "auth", "login"}
	if err := run(args, &output, io.Discard); err != nil {
		t.Fatal(err)
	}
	stored, err := loadStoredAuth(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Token != "access-1" || stored.RefreshToken != "refresh-1" || stored.TokenExpiresAt == 0 {
		t.Fatalf("stored auth = %#v", stored)
	}
	var response map[string]any
	if err := json.Unmarshal([]byte(output.String()), &response); err != nil {
		t.Fatalf("output=%q error=%v", output.String(), err)
	}
	if response["method"] != "device" {
		t.Fatalf("output=%q", output.String())
	}
}
