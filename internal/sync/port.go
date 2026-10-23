// Package sync mirrors DistributionCenter releases to external destinations.
//
// The engine owns the durable outbox, publisher credentials, and the
// dispatcher. Adapters (GitHub, GitLab, Gitea, webhook) implement Target and
// publish it on the PortSyncTargets port, so a new destination is a new module
// and never a change here.
package sync

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"src.solsynth.dev/solsynth/distribution/internal/module"
)

// PortSyncTargets is the port every destination adapter provides.
const PortSyncTargets module.PortID = "syncTargets"

// Provider IDs are module IDs; they double as the connection provider value.
const (
	GitHubProviderID  = "app.solian.sync.github"
	GitLabProviderID  = "app.solian.sync.gitlab"
	GiteaProviderID   = "app.solian.sync.gitea"
	WebhookProviderID = "app.solian.sync.webhook"
)

// ModuleID is the feature module that owns the outbox. Destination adapters
// require it, so disabling sync also removes the adapters that would have no
// consumer.
const ModuleID = "app.solian.distribution.sync"

// Repository names the destination a link mirrors to. Repository is
// "owner/name" for GitHub/Gitea, a project path for GitLab, and empty for
// webhook destinations.
type Repository struct {
	Repository string
	BaseURL    string
}

// Asset is the single downloadable artifact attached to a synced release. A
// release may have many platform artifacts; the dispatcher picks one, because
// a destination release expresses one download, not a matrix.
type Asset struct {
	Name     string
	MimeType string
	Size     int64
	Hash     string
	Open     func(ctx context.Context) (io.ReadCloser, error)
}

// ReleasePayload is the destination-neutral description of one release.
type ReleasePayload struct {
	ProductSlug string
	ProductName string
	ProductID   string
	ReleaseID   string
	Version     string
	Title       string
	Notes       string
	Channels    []string
	Prerelease  bool
	Draft       bool
	// Asset is nil when the release has no object-backed artifact or the
	// configured asset limit forbids uploading one.
	Asset *Asset
	// AssetURL is the absolute public download URL ("" when no artifact).
	// Adapters without asset upload append it to the notes.
	AssetURL string
	Link     Repository
}

// Result records where a destination put the release.
type Result struct {
	ExternalID  string
	ExternalURL string
}

// Target is one destination adapter.
type Target interface {
	// Provider returns the module ID that supplies this adapter.
	Provider() string
	// Validate rejects a connection whose base URL or secret shape this
	// adapter cannot use. It never performs network I/O.
	Validate(conn Connection) error
	Publish(ctx context.Context, conn Connection, payload ReleasePayload) (Result, error)
	// Yank receives the same payload as Publish plus the Result recorded when
	// the release was published, so an adapter can delete by external ID and
	// fall back to the version tag.
	Yank(ctx context.Context, conn Connection, payload ReleasePayload, previous Result) error
}

// Connection is the decrypted, adapter-facing view of a sync connection.
type Connection struct {
	ID      string
	Name    string
	BaseURL string
	Secret  map[string]string
}

// RateLimitError marks a destination throttle. The dispatcher retries no
// sooner than RetryAfter.
type RateLimitError struct {
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("rate limited; retry after %s", e.RetryAfter)
}

// ErrPermanent marks a failure that will never succeed on retry.
var ErrPermanent = errors.New("permanent sync failure")

// Permanent wraps err as a terminal failure.
func Permanent(err error) error {
	return fmt.Errorf("%w: %v", ErrPermanent, err)
}
