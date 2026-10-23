package sync

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"src.solsynth.dev/solsynth/distribution/internal/database"
	"src.solsynth.dev/solsynth/distribution/internal/module"
	"src.solsynth.dev/solsynth/distribution/internal/service"
	gen "src.solsynth.dev/sosys/go/proto"
)

const testProvider = "app.solian.sync.github"

func testKey() string {
	return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
}

func TestSealerRoundTripAndTamper(t *testing.T) {
	sealer, err := NewSealer(testKey())
	if err != nil {
		t.Fatalf("NewSealer() error = %v", err)
	}
	sealed, err := sealer.Seal([]byte(`{"token":"ghp_test"}`))
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	plain, err := sealer.Open(sealed)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if string(plain) != `{"token":"ghp_test"}` {
		t.Fatalf("Open() = %q", plain)
	}

	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-1] ^= 0xff
	if _, err := sealer.Open(base64.StdEncoding.EncodeToString(raw)); !errors.Is(err, ErrSealedSecret) {
		t.Fatalf("Open(tampered) error = %v, want ErrSealedSecret", err)
	}

	if _, err := NewSealer(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 16))); err == nil {
		t.Fatal("NewSealer() accepted a 16-byte key")
	}
	if _, err := NewSealer(""); err == nil {
		t.Fatal("NewSealer() accepted an empty key")
	}
}

// fakeTarget records the payloads it receives and returns a configurable
// error.
type fakeTarget struct {
	publishes []ReleasePayload
	yanks     []ReleasePayload
	previous  []Result
	err       error
}

func (f *fakeTarget) Provider() string { return testProvider }

func (f *fakeTarget) Validate(Connection) error { return nil }

func (f *fakeTarget) Publish(_ context.Context, _ Connection, payload ReleasePayload) (Result, error) {
	f.publishes = append(f.publishes, payload)
	if f.err != nil {
		return Result{}, f.err
	}
	return Result{ExternalID: "42", ExternalURL: "https://github.test/releases/42"}, nil
}

func (f *fakeTarget) Yank(_ context.Context, _ Connection, payload ReleasePayload, previous Result) error {
	f.yanks = append(f.yanks, payload)
	f.previous = append(f.previous, previous)
	return f.err
}

type fixtureDirectory struct{}

func (fixtureDirectory) Authenticate(context.Context, string) (string, error) {
	return "account-id", nil
}

func (fixtureDirectory) GetPublisher(context.Context, string) (*gen.DyPublisher, error) {
	return &gen.DyPublisher{Id: "22222222-2222-2222-2222-222222222222", Name: "Example"}, nil
}

func (fixtureDirectory) IsPublisherMember(context.Context, string, string, gen.DyPublisherMemberRole) (bool, error) {
	return true, nil
}

type fixture struct {
	db      *gorm.DB
	service *Service
	target  *fakeTarget
	product database.Product
	conn    database.SyncConnection
	release database.Release
	linkSeq int
	linkIDs []string
}

func newFixture(t *testing.T, opts Options) *fixture {
	t.Helper()
	db, err := database.OpenDSN("file:sync-" + strings.ReplaceAll(t.Name(), "/", "-") + "?mode=memory&cache=shared")
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
	sealer, err := NewSealer(testKey())
	if err != nil {
		t.Fatal(err)
	}
	target := &fakeTarget{}
	registry := module.NewPortRegistry()
	module.Provide[Target](registry, PortSyncTargets, testProvider, module.KindIntegration, target)

	product := database.Product{ID: "11111111-1111-1111-1111-111111111111", PublisherID: "22222222-2222-2222-2222-222222222222", Slug: "desktop", Name: "Desktop"}
	if err := db.Create(&product).Error; err != nil {
		t.Fatal(err)
	}
	for _, channel := range []database.Channel{
		{ID: "33333333-3333-3333-3333-333333333333", AppID: product.ID, Name: "stable"},
		{ID: "44444444-4444-4444-4444-444444444444", AppID: product.ID, Name: "beta"},
	} {
		if err := db.Create(&channel).Error; err != nil {
			t.Fatal(err)
		}
	}
	sealed, err := sealer.Seal([]byte(`{"kind":"token","token":"ghp_test"}`))
	if err != nil {
		t.Fatal(err)
	}
	connection := database.SyncConnection{ID: "55555555-5555-5555-5555-555555555555", PublisherID: product.PublisherID, Provider: testProvider, Name: "release-bot", Secret: sealed}
	if err := db.Create(&connection).Error; err != nil {
		t.Fatal(err)
	}
	release := database.Release{ID: "66666666-6666-6666-6666-666666666666", AppID: product.ID, Version: "1.4.0", Title: "Desktop 1.4.0", ReleaseNotes: "notes", Status: database.ReleaseStatusDraft}
	if err := db.Create(&release).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&database.ReleaseArtifact{ID: "88888888-8888-8888-8888-888888888888", ReleaseID: release.ID, ObjectKey: "artifacts/app.tar.gz", FileName: "app.tar.gz", MimeType: "application/gzip", Size: 12, Platform: "macos", Architecture: "arm64"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO release_channels (release_id, channel_id) VALUES (?, ?)", release.ID, "33333333-3333-3333-3333-333333333333").Error; err != nil {
		t.Fatal(err)
	}

	opts.Releases = service.NewPublisherReleaseService(db.DB, fixtureDirectory{}, nil, nil)
	svc := NewService(db.DB, sealer, registry, opts, slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc.Configure(map[string]bool{testProvider: true})
	return &fixture{db: db.DB, service: svc, target: target, product: product, conn: connection, release: release}
}

func (f *fixture) addLink(t *testing.T, repository string, filter ChannelFilter) database.SyncLink {
	t.Helper()
	f.linkSeq++
	link := database.SyncLink{
		ID:           "77777777-7777-7777-7777-77777777777" + string(rune('0'+f.linkSeq)),
		ProductID:    f.product.ID,
		ConnectionID: f.conn.ID,
		Repository:   repository,
		Channels:     encodeChannelFilter(filter),
		Enabled:      true,
	}
	if err := f.db.Create(&link).Error; err != nil {
		t.Fatal(err)
	}
	f.linkIDs = append(f.linkIDs, link.ID)
	return link
}

func TestEnqueueIsIdempotentAndChannelFiltered(t *testing.T) {
	f := newFixture(t, Options{BatchSize: 8, MaxAttempts: 3, RetryBase: time.Millisecond, RetryCap: time.Second, PollInterval: time.Hour})
	f.addLink(t, "Solsynth/A", ChannelFilter{Include: []string{"stable"}})
	f.addLink(t, "Solsynth/B", ChannelFilter{Exclude: []string{"stable"}})

	for range 2 {
		if err := f.db.Transaction(func(tx *gorm.DB) error {
			return f.service.EnqueueForRelease(tx, f.product.ID, f.release.ID, JobActionPublish, []string{"stable"})
		}); err != nil {
			t.Fatalf("EnqueueForRelease() error = %v", err)
		}
	}

	var jobs []database.SyncJob
	if err := f.db.Find(&jobs).Error; err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Fatalf("jobs = %d, want 1 (link A only, enqueued twice)", len(jobs))
	}
	if jobs[0].LinkID != f.linkIDs[0] {
		t.Fatalf("job targets link %q, want the stable-only link %q", jobs[0].LinkID, f.linkIDs[0])
	}
	if jobs[0].State != JobStatePending {
		t.Fatalf("state = %q, want pending", jobs[0].State)
	}
}

func TestDeleteConnectionRefusesWithLinks(t *testing.T) {
	f := newFixture(t, Options{BatchSize: 8, MaxAttempts: 3, RetryBase: time.Millisecond, RetryCap: time.Second, PollInterval: time.Hour})
	link := f.addLink(t, "Solsynth/A", ChannelFilter{})
	if err := f.service.DeleteConnection(context.Background(), f.conn.PublisherID, f.conn.ID); !errors.Is(err, service.ErrConflict) {
		t.Fatalf("DeleteConnection() error = %v, want ErrConflict", err)
	}
	if err := f.service.DeleteLink(context.Background(), f.product.ID, link.ID); err != nil {
		t.Fatalf("DeleteLink() error = %v", err)
	}
	if err := f.service.DeleteConnection(context.Background(), f.conn.PublisherID, f.conn.ID); err != nil {
		t.Fatalf("DeleteConnection() after deleting the link error = %v", err)
	}
}

func TestDispatchRetriesThenFails(t *testing.T) {
	f := newFixture(t, Options{BatchSize: 8, MaxAttempts: 2, RetryBase: time.Millisecond, RetryCap: time.Millisecond, PollInterval: time.Hour})
	f.addLink(t, "Solsynth/A", ChannelFilter{})
	f.target.err = errors.New("destination unavailable")
	if err := f.service.EnqueueForRelease(f.db, f.product.ID, f.release.ID, JobActionPublish, []string{"stable"}); err != nil {
		t.Fatal(err)
	}

	for range 2 {
		if _, err := f.service.RunOnce(context.Background()); err != nil {
			t.Fatalf("RunOnce() error = %v", err)
		}
		// Rewind the backoff so the next tick is due immediately.
		if err := f.db.Model(&database.SyncJob{}).Where("1 = 1").Update("next_attempt", time.Now().UTC().Add(-time.Second)).Error; err != nil {
			t.Fatal(err)
		}
	}
	var job database.SyncJob
	if err := f.db.First(&job).Error; err != nil {
		t.Fatal(err)
	}
	if job.State != JobStateFailed || job.Attempts != 2 {
		t.Fatalf("job = %s/%d, want failed/2", job.State, job.Attempts)
	}

	processed, err := f.service.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if processed != 0 {
		t.Fatalf("RunOnce() processed = %d, want 0 after terminal failure", processed)
	}
}

func TestDispatchPermanentErrorIsTerminal(t *testing.T) {
	f := newFixture(t, Options{BatchSize: 8, MaxAttempts: 5, RetryBase: time.Millisecond, RetryCap: time.Second, PollInterval: time.Hour})
	f.addLink(t, "Solsynth/A", ChannelFilter{})
	f.target.err = Permanent(errors.New("tag is invalid"))
	if err := f.service.EnqueueForRelease(f.db, f.product.ID, f.release.ID, JobActionPublish, []string{"stable"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	var job database.SyncJob
	if err := f.db.First(&job).Error; err != nil {
		t.Fatal(err)
	}
	if job.State != JobStateFailed || job.Attempts != 1 {
		t.Fatalf("job = %s/%d, want failed/1", job.State, job.Attempts)
	}
	if !strings.Contains(job.LastError, "tag is invalid") {
		t.Fatalf("last_error = %q", job.LastError)
	}
}

func TestDispatchYankUsesPreviousExternalID(t *testing.T) {
	f := newFixture(t, Options{BatchSize: 8, MaxAttempts: 3, RetryBase: time.Millisecond, RetryCap: time.Second, PollInterval: time.Hour})
	f.addLink(t, "Solsynth/A", ChannelFilter{})
	if err := f.service.EnqueueForRelease(f.db, f.product.ID, f.release.ID, JobActionPublish, []string{"stable"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := f.service.EnqueueForRelease(f.db, f.product.ID, f.release.ID, JobActionYank, []string{"stable"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.target.yanks) != 1 {
		t.Fatalf("Yank() calls = %d, want 1", len(f.target.yanks))
	}
	if f.target.previous[0].ExternalID != "42" {
		t.Fatalf("previous.ExternalID = %q, want 42", f.target.previous[0].ExternalID)
	}
	if len(f.target.publishes) != 1 {
		t.Fatalf("Publish() calls = %d, want 1", len(f.target.publishes))
	}
	published := f.target.publishes[0]
	if published.Version != "1.4.0" || published.Title != "Desktop 1.4.0" || published.Notes != "notes" {
		t.Fatalf("payload = %+v", published)
	}
	if published.Link.Repository != "Solsynth/A" {
		t.Fatalf("payload repository = %q", published.Link.Repository)
	}
	if len(published.Channels) != 1 || published.Channels[0] != "stable" {
		t.Fatalf("payload channels = %v, want [stable]", published.Channels)
	}
	if published.Prerelease {
		t.Fatal("stable release marked prerelease")
	}
}

func TestDispatchHoldsJobsWhenProviderIsDisabled(t *testing.T) {
	f := newFixture(t, Options{BatchSize: 8, MaxAttempts: 2, RetryBase: time.Millisecond, RetryCap: time.Second, PollInterval: time.Hour})
	f.addLink(t, "Solsynth/A", ChannelFilter{})
	if err := f.service.EnqueueForRelease(f.db, f.product.ID, f.release.ID, JobActionPublish, []string{"stable"}); err != nil {
		t.Fatal(err)
	}
	// Disabling the adapter must not consume attempts or fail the delivery.
	f.service.Configure(map[string]bool{})
	for range 3 {
		if _, err := f.service.RunOnce(context.Background()); err != nil {
			t.Fatalf("RunOnce() error = %v", err)
		}
		if err := f.db.Model(&database.SyncJob{}).Where("1 = 1").Update("next_attempt", time.Now().UTC().Add(-time.Second)).Error; err != nil {
			t.Fatal(err)
		}
	}
	var job database.SyncJob
	if err := f.db.First(&job).Error; err != nil {
		t.Fatal(err)
	}
	if job.State != JobStatePending || job.Attempts != 0 {
		t.Fatalf("job = %s/%d, want pending/0 while the adapter is disabled", job.State, job.Attempts)
	}
	if len(f.target.publishes) != 0 {
		t.Fatalf("Publish() calls = %d, want 0", len(f.target.publishes))
	}

	// Re-enabling the adapter resumes the held job.
	f.service.Configure(map[string]bool{testProvider: true})
	if _, err := f.service.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := f.db.First(&job).Error; err != nil {
		t.Fatal(err)
	}
	if job.State != JobStateDone {
		t.Fatalf("job state = %s, want done after re-enabling the adapter", job.State)
	}
}
