package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestDeveloperPortalRoutesCompose proves the portal modules mount without a
// route collision and answer with the fleet's ApiError shape before a handler
// runs. A duplicate registration panics inside gin, so composing is the check.
func TestDeveloperPortalRoutesCompose(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := gatingConfig(t)
	app, err := New(cfg, log)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = app.Stop(context.Background()) })

	modules, capabilities := listModules(t, app)
	for _, id := range []string{
		"app.solian.develop.developers",
		"app.solian.develop.apps",
		"app.solian.develop.bots",
		"app.solian.develop.miniapps",
		"app.solian.develop.products",
	} {
		if _, ok := modules[id]; !ok {
			t.Fatalf("modules = %v, want %s", modules, id)
		}
	}
	for _, capability := range []string{"developers", "custom-apps", "bots", "miniapps", "app-products"} {
		found := false
		for _, candidate := range capabilities {
			if candidate == capability {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("capabilities = %v, want %s", capabilities, capability)
		}
	}

	cases := []struct {
		method string
		path   string
		want   int
	}{
		{http.MethodGet, "/api/developers", http.StatusUnauthorized},
		{http.MethodGet, "/api/private/apps", http.StatusUnauthorized},
		{http.MethodGet, "/api/private/bots", http.StatusUnauthorized},
		{http.MethodGet, "/api/private/miniapps", http.StatusUnauthorized},
		{http.MethodGet, "/api/private/apps/11111111-1111-1111-1111-111111111111/products", http.StatusUnauthorized},
		// Public catalogs stay anonymous.
		{http.MethodGet, "/api/custom-apps", http.StatusOK},
		{http.MethodGet, "/api/custom-apps/board", http.StatusOK},
		{http.MethodGet, "/api/miniapps", http.StatusOK},
		// The marketplace keeps /api/apps: the portal never claims it.
		{http.MethodGet, "/api/apps", http.StatusOK},
	}
	for _, testCase := range cases {
		response := httptest.NewRecorder()
		app.HTTPServer.Engine.ServeHTTP(response, httptest.NewRequest(testCase.method, testCase.path, nil))
		if response.Code != testCase.want {
			t.Fatalf("%s %s status = %d, want %d, body = %s", testCase.method, testCase.path, response.Code, testCase.want, response.Body.String())
		}
	}
}

// TestDeveloperPortalUnauthorizedShape checks that an unauthenticated portal
// call returns the ApiError body the fleet's clients parse.
func TestDeveloperPortalUnauthorizedShape(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	app, err := New(gatingConfig(t), log)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = app.Stop(context.Background()) })

	response := httptest.NewRecorder()
	app.HTTPServer.Engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/private/apps", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload struct {
		Code    string `json:"code"`
		Status  int    `json:"status"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode body: %v (%s)", err, response.Body.String())
	}
	if payload.Code != "UNAUTHORIZED" || payload.Status != http.StatusUnauthorized {
		t.Fatalf("payload = %+v", payload)
	}
	if !strings.Contains(strings.ToLower(payload.Message), "authentication") {
		t.Fatalf("message = %q", payload.Message)
	}
}

// TestDeveloperPortalDisabled proves disabling the portal removes its routes
// and its schema, so a deployment can opt out entirely.
func TestDeveloperPortalDisabled(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := gatingConfig(t)
	cfg.Modules.Disabled = []string{
		"app.solian.develop.developers",
		"app.solian.develop.apps",
		"app.solian.develop.bots",
		"app.solian.develop.miniapps",
		"app.solian.develop.products",
	}
	app, err := New(cfg, log)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = app.Stop(context.Background()) })

	if app.DevelopService != nil {
		t.Fatal("DevelopService composed although every portal module is disabled")
	}
	for _, table := range []string{"developers", "dev_projects", "custom_apps", "mini_apps", "bot_accounts"} {
		if app.Database.Migrator().HasTable(table) {
			t.Fatalf("%s exists although the portal is disabled", table)
		}
	}
	response := httptest.NewRecorder()
	app.HTTPServer.Engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/custom-apps", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 when the portal is disabled", response.Code)
	}
}
