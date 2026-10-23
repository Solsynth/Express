package webui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func testEngine(t *testing.T, dist string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	handler, err := New(dist)
	if err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	engine.Use(handler.ServeStatic)
	engine.NoRoute(handler.Serve)
	engine.GET("/api/products/:productID", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"product": c.Param("productID")})
	})
	return engine
}

func testDist(t *testing.T) string {
	t.Helper()
	dist := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dist, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte("<html>console</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dist, "assets", "index-abc123.js"), []byte("console.log(1)"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dist
}

func TestServesAssetsAndFallsBackToIndex(t *testing.T) {
	engine := testEngine(t, testDist(t))

	asset := httptest.NewRecorder()
	engine.ServeHTTP(asset, httptest.NewRequest(http.MethodGet, "/assets/index-abc123.js", nil))
	if asset.Code != http.StatusOK || asset.Body.String() != "console.log(1)" {
		t.Fatalf("asset status = %d, body = %q", asset.Code, asset.Body.String())
	}
	if cache := asset.Header().Get("Cache-Control"); !strings.Contains(cache, "immutable") {
		t.Fatalf("asset cache-control = %q, want immutable", cache)
	}

	// A client-side route is not a file, so a reload has to land on the shell.
	route := httptest.NewRecorder()
	engine.ServeHTTP(route, httptest.NewRequest(http.MethodGet, "/workspace/abc/releases/def", nil))
	if route.Code != http.StatusOK || !strings.Contains(route.Body.String(), "console") {
		t.Fatalf("spa route status = %d, body = %q", route.Code, route.Body.String())
	}
	if cache := route.Header().Get("Cache-Control"); cache != "no-cache" {
		t.Fatalf("index cache-control = %q, want no-cache", cache)
	}
}

func TestReservedAndMissingPathsStayServerOwned(t *testing.T) {
	engine := testEngine(t, testDist(t))

	cases := []struct {
		target string
		want   int
		body   string
	}{
		// The API answers for itself; the shell must never shadow it.
		{"/api/products/abc", http.StatusOK, `"product":"abc"`},
		{"/api/marketplace/apps", http.StatusNotFound, "not found"},
		// A missing fingerprinted asset is a 404, not the shell: serving HTML
		// for a script request turns a deploy mistake into a parse error.
		{"/assets/missing.js", http.StatusNotFound, "not found"},
		{"/health", http.StatusNotFound, "not found"},
		{"/artifacts/abc/download", http.StatusNotFound, "not found"},
	}
	for _, testCase := range cases {
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, testCase.target, nil))
		if response.Code != testCase.want || !strings.Contains(response.Body.String(), testCase.body) {
			t.Fatalf("%s status = %d, body = %q", testCase.target, response.Code, response.Body.String())
		}
	}
}

func TestRejectsTraversal(t *testing.T) {
	engine := testEngine(t, testDist(t))

	for _, target := range []string{"/../config.example.toml", "/assets/../../etc/passwd"} {
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404", target, response.Code)
		}
		if strings.Contains(response.Body.String(), "root:") {
			t.Fatalf("%s leaked a file outside dist", target)
		}
	}
}

func TestDisabledHandlerAnswersOnlyTheJSON404(t *testing.T) {
	handler, err := New("")
	if err != nil {
		t.Fatal(err)
	}
	if handler.Enabled() {
		t.Fatal("an empty dist path must leave the handler disabled")
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(handler.ServeStatic)
	engine.NoRoute(handler.Serve)

	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/marketplace", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", response.Code)
	}
}

func TestNewRejectsADirectoryWithoutIndex(t *testing.T) {
	if _, err := New(t.TempDir()); err == nil {
		t.Fatal("New() error = nil, want a missing index.html error")
	}
}
