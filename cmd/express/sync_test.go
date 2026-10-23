package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSyncConnectionCreateSendsBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/publishers/Example/sync/connections" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer sphere-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		body, _ := io.ReadAll(r.Body)
		want := `{"name":"release-bot","provider":"app.solian.sync.github","secret":{"kind":"token","token":"ghp_test"}}`
		if string(body) != want {
			t.Fatalf("body = %s, want %s", body, want)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"connection-1","provider":"app.solian.sync.github","name":"release-bot","has_secret":true}`)
	}))
	defer server.Close()

	var output strings.Builder
	err := run([]string{
		"--base-url", server.URL, "--token", "sphere-token",
		"sync", "connection", "create",
		"--publisher", "Example",
		"--provider", "app.solian.sync.github",
		"--name", "release-bot",
		"--secret", "kind=token",
		"--secret", "token=ghp_test",
	}, &output, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "release-bot") || !strings.Contains(output.String(), "Secret:   set") {
		t.Fatalf("output = %s", output.String())
	}
}

func TestSyncProvidersRendersModules(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/modules" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"modules":[{"id":"app.solian.distribution.sync","kind":"feature","capabilities":["sync"]},{"id":"app.solian.sync.github","kind":"integration","capabilities":["github"]}],"capabilities":["github","sync"]}`)
	}))
	defer server.Close()

	var output strings.Builder
	if err := run([]string{"--base-url", server.URL, "--token", "sphere-token", "sync", "providers"}, &output, io.Discard); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
		if strings.Contains(line, "app.solian.sync.github") && strings.Contains(line, "integration") {
			found = true
		}
		if strings.Contains(line, "app.solian.distribution.sync") {
			t.Fatalf("output listed the feature module: %s", output.String())
		}
	}
	if !found {
		t.Fatalf("output = %s, want a github integration row", output.String())
	}
}

func TestSyncLinkListRendersChannelFilter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/products/product-1/sync/links" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"data":[{"id":"link-1","provider":"app.solian.sync.github","connection":"release-bot","repository":"Solsynth/SolsynthExpress","channels":{"include":["stable"]},"enabled":true}]}`)
	}))
	defer server.Close()

	var output strings.Builder
	if err := run([]string{"--base-url", server.URL, "--token", "sphere-token", "sync", "link", "list", "--product", "product-1"}, &output, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Solsynth/SolsynthExpress") || !strings.Contains(output.String(), "include:stable") {
		t.Fatalf("output = %s", output.String())
	}
}
