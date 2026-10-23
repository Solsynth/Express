package httpserver

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"src.solsynth.dev/solsynth/distribution/internal/config"
)

func TestHealthAndReadiness(t *testing.T) {
	server := New(config.Default())

	request := httptest.NewRequest("GET", "/health", nil)
	response := httptest.NewRecorder()
	server.Engine.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatalf("/health status = %d, want 200", response.Code)
	}

	request = httptest.NewRequest("GET", "/ready", nil)
	response = httptest.NewRecorder()
	server.Engine.ServeHTTP(response, request)
	if response.Code != 503 {
		t.Fatalf("/ready before startup = %d, want 503", response.Code)
	}

	server.SetReady(true)
	request = httptest.NewRequest("GET", "/ready", nil)
	response = httptest.NewRecorder()
	server.Engine.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatalf("/ready after startup = %d, want 200", response.Code)
	}
}

// A configured distPath that cannot be opened must be reported, not silently
// skipped: the process otherwise serves the API and 404s every console route,
// which reads as a routing bug instead of a missing build.
func TestServerReportsAnUnusableDistPath(t *testing.T) {
	cfg := config.Default()
	cfg.Web.DistPath = t.TempDir() // a directory without index.html

	server := New(cfg)
	if server.WebError == nil {
		t.Fatal("WebError = nil, want an error for a dist path without index.html")
	}

	response := httptest.NewRecorder()
	server.Engine.ServeHTTP(response, httptest.NewRequest("GET", "/marketplace", nil))
	if response.Code != 404 {
		t.Fatalf("console route status = %d, want 404 while unmounted", response.Code)
	}
}

// A valid distPath mounts the console, and the API keeps answering beside it.
func TestServerMountsTheConsoleWhenDistExists(t *testing.T) {
	dist := t.TempDir()
	if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte("<html>console</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Web.DistPath = dist

	server := New(cfg)
	if server.WebError != nil {
		t.Fatalf("WebError = %v, want nil", server.WebError)
	}

	response := httptest.NewRecorder()
	server.Engine.ServeHTTP(response, httptest.NewRequest("GET", "/workspace/abc", nil))
	if response.Code != 200 || !strings.Contains(response.Body.String(), "console") {
		t.Fatalf("console route status = %d, body = %q", response.Code, response.Body.String())
	}
}
