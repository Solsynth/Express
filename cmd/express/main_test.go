package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunListsAppsWithGlobalAuthAndQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/marketplace/apps" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer sphere-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		if r.URL.Query().Get("sort") != "name" || r.URL.Query().Get("order") != "asc" {
			t.Fatalf("query = %s", r.URL.RawQuery)
		}
		_, _ = io.WriteString(w, `{"data":[{"product":{"slug":"demo"}}],"total":1}`)
	}))
	defer server.Close()

	var output strings.Builder
	if err := run([]string{"--base-url", server.URL, "--token", "sphere-token", "--json", "apps", "list", "--sort", "name", "--order", "asc"}, &output, io.Discard); err != nil {
		t.Fatal(err)
	}
	var response struct {
		Total int `json:"total"`
	}
	if err := json.Unmarshal([]byte(output.String()), &response); err != nil {
		t.Fatal(err)
	}
	if response.Total != 1 {
		t.Fatalf("output = %s", output.String())
	}
}

func TestRunCreatesProductFromJSONBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/publishers/acme/products" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"slug":"demo","name":"Demo"}` {
			t.Fatalf("body = %s", body)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"product-1"}`)
	}))
	defer server.Close()

	dir := t.TempDir()
	bodyPath := filepath.Join(dir, "product.json")
	if err := os.WriteFile(bodyPath, []byte(`{"slug":"demo","name":"Demo"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	if err := run([]string{"--base-url", server.URL, "--token", "token", "--json", "product", "create", "--publisher", "acme", "--body", bodyPath}, &output, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"id": "product-1"`) {
		t.Fatalf("output = %s", output.String())
	}
}

func TestRunUploadHashesPutsAndAttachesArtifact(t *testing.T) {
	var uploaded bool
	var attachedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/products/p1/artifacts/upload-url":
			_, _ = io.WriteString(w, `{"object_key":"artifacts/demo.tar.gz","upload_url":"`+"http://"+r.Host+`/s3-upload","release_id":"r1","version":"1.2.3"}`)
		case r.Method == http.MethodPut && r.URL.Path == "/s3-upload":
			uploaded = true
			if got := r.Header.Get("x-amz-meta-sha256"); got != "4659fc0570122b0e0aa14f4ff7c261b1fe51795a01ba79963f462ebf40d7520d" {
				t.Errorf("sha256 metadata = %q", got)
			}
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/products/p1/releases/r1/artifacts":
			body, _ := io.ReadAll(r.Body)
			attachedBody = string(body)
			_, _ = io.WriteString(w, `{"id":"artifact-1"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "demo.tar.gz")
	if err := os.WriteFile(artifactPath, []byte("artifact bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	if err := run([]string{"--base-url", server.URL, "artifact", "upload", "--product", "p1", "--version", "1.2.3", "--file", artifactPath, "--platform", "macos", "--architecture", "arm64"}, &output, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !uploaded || !strings.Contains(attachedBody, `"object_key":"artifacts/demo.tar.gz"`) || !strings.Contains(attachedBody, `"platform":"macos"`) {
		t.Fatalf("uploaded=%v attached=%s output=%s", uploaded, attachedBody, output.String())
	}
}

func TestRunHumanOutputByDefault(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/marketplace/apps" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"data":[{"product":{"id":"p1","name":"Demo","slug":"demo"},"publisher":{"id":"u1","name":"acme"},"latest":{"id":"r1","version":"1.4.0","status":"published","published_at":"2026-08-01T00:00:00Z"}}],"total":1,"limit":20,"offset":0}`)
	}))
	defer server.Close()

	var human strings.Builder
	if err := run([]string{"--config", filepath.Join(t.TempDir(), "none.json"), "--base-url", server.URL, "apps", "list"}, &human, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(human.String(), "NAME") || !strings.Contains(human.String(), "demo") || !strings.Contains(human.String(), "1.4.0") {
		t.Fatalf("human output = %q", human.String())
	}

	var asJSON strings.Builder
	if err := run([]string{"--config", filepath.Join(t.TempDir(), "none.json"), "--base-url", server.URL, "--json", "apps", "list"}, &asJSON, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(asJSON.String(), `"total": 1`) {
		t.Fatalf("json output = %q", asJSON.String())
	}
}
