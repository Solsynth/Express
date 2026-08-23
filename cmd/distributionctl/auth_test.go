package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuthLoginStoresTokenAndAPIUsesIt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer stored-token" {
			t.Fatalf("authorization = %q", got)
		}
		_, _ = io.WriteString(w, `{"data":[],"total":0}`)
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "auth.json")
	var loginOutput strings.Builder
	if err := run([]string{"--config", configPath, "auth", "login", "--base-url", server.URL, "--token", "stored-token"}, &loginOutput, io.Discard); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("auth config permissions = %o", info.Mode().Perm())
	}

	var output strings.Builder
	if err := run([]string{"--config", configPath, "apps", "list"}, &output, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"total": 0`) {
		t.Fatalf("output = %s", output.String())
	}

	var status strings.Builder
	if err := run([]string{"--config", configPath, "auth", "status"}, &status, io.Discard); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(status.String(), "stored-token") || !strings.Contains(status.String(), `"token_source": "config"`) {
		t.Fatalf("status = %s", status.String())
	}

	if err := run([]string{"--config", configPath, "auth", "logout"}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("auth config still exists, stat error = %v", err)
	}
}
