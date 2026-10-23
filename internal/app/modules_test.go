package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"src.solsynth.dev/solsynth/distribution/internal/config"
)

// gatingConfig builds a config that composes the real application against a
// temporary SQLite file. gRPC targets are never dialed by grpc.NewClient, so
// dummy addresses are enough.
func gatingConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg := config.Default()
	cfg.Database.DSN = "sqlite://" + filepath.Join(t.TempDir(), "dc.db")
	cfg.Auth.Target = "127.0.0.1:1"
	cfg.Sphere.Target = "127.0.0.1:1"
	cfg.S3.Endpoint = "https://s3.test"
	cfg.S3.AccessKey = "k"
	cfg.S3.SecretKey = "s"
	cfg.S3.Bucket = "b"
	cfg.Sync.EncryptionKey = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	return cfg
}

func listModules(t *testing.T, app *App) (map[string]string, []string) {
	t.Helper()
	response := httptest.NewRecorder()
	app.HTTPServer.Engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/modules", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("GET /api/modules status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload struct {
		Modules []struct {
			ID           string   `json:"id"`
			Kind         string   `json:"kind"`
			Capabilities []string `json:"capabilities"`
		} `json:"modules"`
		Capabilities []string `json:"capabilities"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	kinds := make(map[string]string, len(payload.Modules))
	for _, module := range payload.Modules {
		kinds[module.ID] = module.Kind
	}
	return kinds, payload.Capabilities
}

func TestModulesGating(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("AdapterDisabled", func(t *testing.T) {
		cfg := gatingConfig(t)
		cfg.Modules.Disabled = []string{"app.solian.sync.github"}
		app, err := New(cfg, log)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		t.Cleanup(func() { _ = app.Stop(context.Background()) })

		modules, capabilities := listModules(t, app)
		if _, ok := modules["app.solian.distribution.sync"]; !ok {
			t.Fatalf("modules = %v, want the sync feature module", modules)
		}
		if _, ok := modules["app.solian.sync.github"]; ok {
			t.Fatalf("modules = %v, want github disabled", modules)
		}
		if _, ok := modules["app.solian.sync.gitlab"]; !ok {
			t.Fatalf("modules = %v, want gitlab still enabled", modules)
		}
		for _, capability := range capabilities {
			if capability == "github" {
				t.Fatalf("capabilities = %v, want github absent", capabilities)
			}
		}
		// The feature module owns the outbox, so its tables exist even when an
		// adapter is disabled.
		if !app.Database.Migrator().HasTable("sync_jobs") {
			t.Fatal("sync_jobs table missing with the feature module enabled")
		}
	})

	t.Run("SyncDisabled", func(t *testing.T) {
		cfg := gatingConfig(t)
		cfg.Modules.Disabled = []string{"app.solian.distribution.sync"}
		cfg.Sync.EncryptionKey = ""
		app, err := New(cfg, log)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		t.Cleanup(func() { _ = app.Stop(context.Background()) })

		modules, _ := listModules(t, app)
		if _, ok := modules["app.solian.distribution.sync"]; ok {
			t.Fatalf("modules = %v, want the sync feature module disabled", modules)
		}
		if _, ok := modules["app.solian.sync.github"]; ok {
			t.Fatalf("modules = %v, want github absent when nothing requires it", modules)
		}
		for _, table := range []string{"sync_jobs", "sync_links", "sync_connections"} {
			if app.Database.Migrator().HasTable(table) {
				t.Fatalf("%s exists although the sync module is disabled", table)
			}
		}
	})

	t.Run("UnknownModule", func(t *testing.T) {
		cfg := gatingConfig(t)
		cfg.Modules.Disabled = []string{"app.example.missing"}
		if _, err := New(cfg, log); err == nil || !strings.Contains(err.Error(), `unknown module "app.example.missing"`) {
			t.Fatalf("New() error = %v, want an unknown module error", err)
		}
	})
}
