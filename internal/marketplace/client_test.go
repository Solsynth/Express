package marketplace

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestClientDoSendsBearerQueryAndJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if r.URL.Query().Get("limit") != "2" {
			t.Fatalf("limit = %q", r.URL.Query().Get("limit"))
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Fatalf("authorization = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("content type = %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"name":"demo"}` {
			t.Fatalf("body = %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "token")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := client.Do(context.Background(), http.MethodPost, "/api/test", url.Values{"limit": {"2"}}, []byte(`{"name":"demo"}`))
	if err != nil {
		t.Fatal(err)
	}
	var response map[string]bool
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	if !response["ok"] {
		t.Fatalf("response = %s", raw)
	}
}

func TestClientDoReturnsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"nope"}`, http.StatusForbidden)
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Do(context.Background(), http.MethodGet, "/api/test", nil, nil)
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("error type = %T", err)
	}
	if apiErr.StatusCode != http.StatusForbidden || !strings.Contains(apiErr.Body, "nope") {
		t.Fatalf("api error = %#v", apiErr)
	}
}

func TestPutArtifactSetsS3Metadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Fatalf("method = %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/gzip" || r.Header.Get("x-amz-meta-sha256") != "abc" {
			t.Fatalf("headers = %#v", r.Header)
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != "payload" {
			t.Fatalf("payload = %q", body)
		}
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.PutArtifact(context.Background(), server.URL, "demo.tar.gz", "application/gzip", "abc", strings.NewReader("payload"), int64(len("payload"))); err != nil {
		t.Fatal(err)
	}
}
