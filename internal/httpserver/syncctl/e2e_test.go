package syncctl_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	gen "src.solsynth.dev/sosys/go/proto"

	"src.solsynth.dev/solsynth/distribution/internal/config"
	"src.solsynth.dev/solsynth/distribution/internal/database"
	"src.solsynth.dev/solsynth/distribution/internal/httpserver"
	"src.solsynth.dev/solsynth/distribution/internal/httpserver/syncctl"
	"src.solsynth.dev/solsynth/distribution/internal/module"
	"src.solsynth.dev/solsynth/distribution/internal/service"
	syncengine "src.solsynth.dev/solsynth/distribution/internal/sync"
)

// directory is a publisher directory fake. The e2e test cannot live in package
// httpserver because that would import the module package that imports it.
type directory struct {
	accountID   string
	publisherID string
}

func (d directory) Authenticate(_ context.Context, token string) (string, error) {
	if token != "sphere-token" {
		return "", service.ErrUnauthorized
	}
	return d.accountID, nil
}

func (d directory) AuthenticateAccount(ctx context.Context, token string) (service.AuthenticatedAccount, error) {
	accountID, err := d.Authenticate(ctx, token)
	if err != nil {
		return service.AuthenticatedAccount{}, err
	}
	return service.AuthenticatedAccount{ID: accountID, IsSuperuser: true}, nil
}

func (d directory) GetPublisher(context.Context, string) (*gen.DyPublisher, error) {
	return &gen.DyPublisher{Id: d.publisherID, Name: "Example"}, nil
}

func (d directory) IsPublisherMember(context.Context, string, string, gen.DyPublisherMemberRole) (bool, error) {
	return true, nil
}

type fakeTarget struct {
	published []syncengine.ReleasePayload
	yanked    []syncengine.ReleasePayload
	previous  []syncengine.Result
}

func (f *fakeTarget) Provider() string { return syncengine.GitHubProviderID }

func (f *fakeTarget) Validate(syncengine.Connection) error { return nil }

func (f *fakeTarget) Publish(_ context.Context, _ syncengine.Connection, payload syncengine.ReleasePayload) (syncengine.Result, error) {
	f.published = append(f.published, payload)
	return syncengine.Result{ExternalID: "42", ExternalURL: "https://github.test/Solsynth/SolsynthExpress/releases/42"}, nil
}

func (f *fakeTarget) Yank(_ context.Context, _ syncengine.Connection, payload syncengine.ReleasePayload, previous syncengine.Result) error {
	f.yanked = append(f.yanked, payload)
	f.previous = append(f.previous, previous)
	return nil
}

func TestSyncEndToEnd(t *testing.T) {
	db, err := database.OpenDSN("file:sync-e2e?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	if err := module.MigrateSchema(context.Background(), db.DB, []module.Factory{{
		ID:     "app.solian.distribution.sync",
		Models: []any{&database.SyncConnection{}, &database.SyncLink{}, &database.SyncJob{}},
	}}); err != nil {
		t.Fatal(err)
	}

	publisherID, productID := uuid.NewString(), uuid.NewString()
	if err := db.Create(&database.Product{ID: productID, PublisherID: publisherID, Slug: "desktop", Name: "Desktop"}).Error; err != nil {
		t.Fatal(err)
	}
	dir := directory{accountID: uuid.NewString(), publisherID: publisherID}
	releases := service.NewPublisherReleaseService(db.DB, dir, nil, nil)
	server := httpserver.New(config.Default())
	httpserver.RegisterPublisherRoutes(server.Engine, releases, dir, config.Default())

	sealer, err := syncengine.NewSealer(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	registry := module.NewPortRegistry()
	target := &fakeTarget{}
	module.Provide[syncengine.Target](registry, syncengine.PortSyncTargets, syncengine.GitHubProviderID, module.KindIntegration, target)
	svc := syncengine.NewService(db.DB, sealer, registry, syncengine.Options{
		BatchSize: 16, MaxAttempts: 3, RetryBase: time.Millisecond, RetryCap: time.Second, PollInterval: time.Hour,
		Releases: releases,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc.Configure(map[string]bool{syncengine.GitHubProviderID: true})
	releases.SetSyncEnqueuer(svc)
	syncctl.Register(server.Engine.Group("/api"), syncctl.Deps{Service: svc, Releases: releases, Publishers: dir})

	do := func(method, path, body string) *httptest.ResponseRecorder {
		var reader io.Reader
		if body != "" {
			reader = strings.NewReader(body)
		}
		request := httptest.NewRequest(method, path, reader)
		request.Header.Set("Authorization", "Bearer sphere-token")
		response := httptest.NewRecorder()
		server.Engine.ServeHTTP(response, request)
		return response
	}

	created := do(http.MethodPost, "/api/publishers/Example/sync/connections",
		`{"provider":"`+syncengine.GitHubProviderID+`","name":"release-bot","secret":{"kind":"token","token":"ghp_test"}}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create connection status = %d, body = %s", created.Code, created.Body.String())
	}
	if strings.Contains(created.Body.String(), "ghp_test") || strings.Contains(created.Body.String(), `"secret"`) {
		t.Fatalf("create connection leaked the secret: %s", created.Body.String())
	}
	var connection struct {
		ID        string `json:"id"`
		HasSecret bool   `json:"has_secret"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &connection); err != nil {
		t.Fatal(err)
	}
	if !connection.HasSecret {
		t.Fatal("create connection reported has_secret = false")
	}
	channel := do(http.MethodPost, "/api/products/"+productID+"/channels", `{"name":"stable","display_name":"Stable"}`)
	if channel.Code != http.StatusCreated {
		t.Fatalf("create channel status = %d, body = %s", channel.Code, channel.Body.String())
	}

	linked := do(http.MethodPost, "/api/products/"+productID+"/sync/links",
		`{"connection_id":"`+connection.ID+`","repository":"Solsynth/SolsynthExpress","channels":{"include":["stable"]}}`)
	if linked.Code != http.StatusCreated {
		t.Fatalf("create link status = %d, body = %s", linked.Code, linked.Body.String())
	}

	releaseResponse := do(http.MethodPost, "/api/products/"+productID+"/releases",
		`{"version":"1.4.0","channels":["stable"],"release_notes":"notes","title":"Desktop 1.4.0"}`)
	if releaseResponse.Code != http.StatusCreated {
		t.Fatalf("create release status = %d, body = %s", releaseResponse.Code, releaseResponse.Body.String())
	}
	var release struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(releaseResponse.Body.Bytes(), &release); err != nil {
		t.Fatal(err)
	}
	artifactResponse := do(http.MethodPost, "/api/products/"+productID+"/releases/"+release.ID+"/artifacts",
		`{"download_url":"https://downloads.test/app.tar.gz","file_name":"app.tar.gz","mime_type":"application/gzip","size":12,"platform":"macos","architecture":"arm64"}`)
	if artifactResponse.Code != http.StatusCreated {
		t.Fatalf("attach artifact status = %d, body = %s", artifactResponse.Code, artifactResponse.Body.String())
	}
	published := do(http.MethodPost, "/api/products/"+productID+"/releases/"+release.ID+"/publish", "")
	if published.Code != http.StatusOK {
		t.Fatalf("publish status = %d, body = %s", published.Code, published.Body.String())
	}

	var pending int64
	if err := db.Model(&database.SyncJob{}).Where("state = ?", syncengine.JobStatePending).Count(&pending).Error; err != nil {
		t.Fatal(err)
	}
	if pending != 1 {
		t.Fatalf("pending jobs = %d, want 1", pending)
	}

	processed, err := svc.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if processed != 1 {
		t.Fatalf("RunOnce() processed = %d, want 1", processed)
	}
	if len(target.published) != 1 {
		t.Fatalf("Publish() calls = %d, want 1", len(target.published))
	}
	payload := target.published[0]
	if payload.Version != "1.4.0" || payload.Link.Repository != "Solsynth/SolsynthExpress" || payload.Notes != "notes" {
		t.Fatalf("payload = %+v", payload)
	}

	jobs := do(http.MethodGet, "/api/products/"+productID+"/sync/jobs", "")
	if jobs.Code != http.StatusOK {
		t.Fatalf("list jobs status = %d, body = %s", jobs.Code, jobs.Body.String())
	}
	var page struct {
		Data []struct {
			State       string `json:"state"`
			Action      string `json:"action"`
			ExternalURL string `json:"external_url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(jobs.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Data) != 1 || page.Data[0].State != syncengine.JobStateDone || page.Data[0].ExternalURL == "" {
		t.Fatalf("jobs page = %+v", page.Data)
	}

	yanked := do(http.MethodPost, "/api/products/"+productID+"/releases/"+release.ID+"/yank", "")
	if yanked.Code != http.StatusOK {
		t.Fatalf("yank status = %d, body = %s", yanked.Code, yanked.Body.String())
	}
	if _, err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() after yank error = %v", err)
	}
	if len(target.yanked) != 1 {
		t.Fatalf("Yank() calls = %d, want 1", len(target.yanked))
	}
	if target.previous[0].ExternalID != "42" {
		t.Fatalf("yank previous = %+v, want the publish result", target.previous[0])
	}
	jobs = do(http.MethodGet, "/api/products/"+productID+"/sync/jobs", "")
	if err := json.Unmarshal(jobs.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Data) != 2 {
		t.Fatalf("jobs = %d, want publish and yank rows", len(page.Data))
	}
	actions := map[string]bool{}
	for _, job := range page.Data {
		actions[job.Action] = true
	}
	if !actions[syncengine.JobActionPublish] || !actions[syncengine.JobActionYank] {
		t.Fatalf("jobs = %+v, want publish and yank", page.Data)
	}
}
