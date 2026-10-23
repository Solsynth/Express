package integrations

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	syncengine "src.solsynth.dev/solsynth/distribution/internal/sync"
)

func githubConnection(server *httptest.Server) syncengine.Connection {
	return syncengine.Connection{
		ID:      "connection-1",
		Name:    "release-bot",
		BaseURL: server.URL,
		Secret:  map[string]string{"kind": "token", "token": "ghp_test"},
	}
}

func githubPayload() syncengine.ReleasePayload {
	return syncengine.ReleasePayload{
		ProductSlug: "desktop",
		ProductName: "Desktop",
		ProductID:   "11111111-1111-1111-1111-111111111111",
		ReleaseID:   "66666666-6666-6666-6666-666666666666",
		Version:     "1.4.0",
		Title:       "Desktop 1.4.0",
		Notes:       "notes",
		Channels:    []string{"stable"},
		Link:        syncengine.Repository{Repository: "Solsynth/SolsynthExpress"},
	}
}

func TestGitHubPublishUploadsAsset(t *testing.T) {
	var uploaded []byte
	var releaseBody map[string]any
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/repos/Solsynth/SolsynthExpress/releases":
			if got := r.Header.Get("Authorization"); got != "Bearer ghp_test" {
				t.Errorf("authorization = %q", got)
			}
			if err := json.NewDecoder(r.Body).Decode(&releaseBody); err != nil {
				t.Errorf("decode release body: %v", err)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":42,"html_url":"https://github.test/Solsynth/SolsynthExpress/releases/42","upload_url":"`+server.URL+`/uploads/42{?name,label}"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/uploads/42":
			if name := r.URL.Query().Get("name"); name != "app.tar.gz" {
				t.Errorf("asset name = %q", name)
			}
			if contentType := r.Header.Get("Content-Type"); contentType != "application/gzip" {
				t.Errorf("content type = %q", contentType)
			}
			uploaded, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":7}`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	payload := githubPayload()
	payload.Asset = &syncengine.Asset{
		Name:     "app.tar.gz",
		MimeType: "application/gzip",
		Size:     11,
		Open: func(context.Context) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("hello world")), nil
		},
	}
	target := NewGitHub(server.Client())
	result, err := target.Publish(context.Background(), githubConnection(server), payload)
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if result.ExternalID != "42" || result.ExternalURL != "https://github.test/Solsynth/SolsynthExpress/releases/42" {
		t.Fatalf("Result = %+v", result)
	}
	want := map[string]any{"tag_name": "1.4.0", "name": "Desktop 1.4.0", "body": "notes", "draft": false, "prerelease": false}
	for key, value := range want {
		if releaseBody[key] != value {
			t.Fatalf("release body %s = %v, want %v", key, releaseBody[key], value)
		}
	}
	if string(uploaded) != "hello world" {
		t.Fatalf("uploaded asset = %q", uploaded)
	}
}

func TestGitHubRejectsMissingToken(t *testing.T) {
	target := NewGitHub(nil)
	err := target.Validate(syncengine.Connection{Secret: map[string]string{"kind": "token"}})
	if err == nil || !strings.Contains(err.Error(), "token") {
		t.Fatalf("Validate() error = %v, want a token complaint", err)
	}
	if err := target.Validate(syncengine.Connection{Secret: map[string]string{"kind": "token", "token": "ghp_test"}}); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestGitHubRateLimitIsRetryable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"message":"API rate limit exceeded"}`)
	}))
	defer server.Close()

	target := NewGitHub(server.Client())
	_, err := target.Publish(context.Background(), githubConnection(server), githubPayload())
	var rateLimited *syncengine.RateLimitError
	if !errors.As(err, &rateLimited) {
		t.Fatalf("Publish() error = %v, want *RateLimitError", err)
	}
	if rateLimited.RetryAfter != 30*time.Second {
		t.Fatalf("RetryAfter = %s, want 30s", rateLimited.RetryAfter)
	}
}

func TestGitHubYankHidesRelease(t *testing.T) {
	var patched map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/repos/Solsynth/SolsynthExpress/releases/42" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&patched); err != nil {
			t.Errorf("decode patch body: %v", err)
		}
		_, _ = io.WriteString(w, `{"id":42}`)
	}))
	defer server.Close()

	target := NewGitHub(server.Client())
	if err := target.Yank(context.Background(), githubConnection(server), githubPayload(), syncengine.Result{ExternalID: "42"}); err != nil {
		t.Fatalf("Yank() error = %v", err)
	}
	if patched["draft"] != true {
		t.Fatalf("patch body = %v, want draft true", patched)
	}
}

func TestGitHubYankFallsBackToTag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/Solsynth/SolsynthExpress/releases/tags/1.4.0":
			_, _ = io.WriteString(w, `{"id":77}`)
		case r.Method == http.MethodPatch && r.URL.Path == "/repos/Solsynth/SolsynthExpress/releases/77":
			_, _ = io.WriteString(w, `{"id":77}`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	target := NewGitHub(server.Client())
	if err := target.Yank(context.Background(), githubConnection(server), githubPayload(), syncengine.Result{}); err != nil {
		t.Fatalf("Yank() error = %v", err)
	}
}

func TestGitHubYankPermanentWhenNeverSynced(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"message":"Not Found"}`)
	}))
	defer server.Close()

	target := NewGitHub(server.Client())
	err := target.Yank(context.Background(), githubConnection(server), githubPayload(), syncengine.Result{})
	if !errors.Is(err, syncengine.ErrPermanent) {
		t.Fatalf("Yank() error = %v, want a permanent failure", err)
	}
}
