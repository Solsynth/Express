package sync

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"src.solsynth.dev/solsynth/distribution/internal/database"
	"src.solsynth.dev/solsynth/distribution/internal/service"
)

// RunOnce performs one dispatcher tick: claim due jobs, deliver them, and
// record the outcome. It returns how many jobs it claimed.
func (s *Service) RunOnce(ctx context.Context) (int, error) {
	if s == nil {
		return 0, ErrSyncDisabled
	}
	jobs, err := s.claim(ctx)
	if err != nil {
		return 0, err
	}
	processed := 0
	for _, job := range jobs {
		processed++
		s.deliver(ctx, job)
	}
	return processed, nil
}

// claim selects due jobs and locks them one row at a time so two dispatchers
// never take the same job.
func (s *Service) claim(ctx context.Context) ([]database.SyncJob, error) {
	now := time.Now().UTC()
	var candidates []database.SyncJob
	err := s.db.WithContext(ctx).
		Where("state IN ? AND next_attempt <= ?", []string{JobStatePending, JobStateRunning}, now).
		Where("locked_until IS NULL OR locked_until < ?", now).
		Order("next_attempt").Limit(s.cfg.BatchSize).
		Find(&candidates).Error
	if err != nil {
		return nil, fmt.Errorf("claim sync jobs: %w", err)
	}
	locked := make([]database.SyncJob, 0, len(candidates))
	lockUntil := now.Add(jobLock)
	for _, job := range candidates {
		result := s.db.WithContext(ctx).Model(&database.SyncJob{}).
			Where("id = ? AND state IN ? AND (locked_until IS NULL OR locked_until < ?)", job.ID, []string{JobStatePending, JobStateRunning}, now).
			Updates(map[string]any{"state": JobStateRunning, "locked_until": lockUntil, "updated_at": now})
		if result.Error != nil {
			return nil, fmt.Errorf("lock sync job %s: %w", job.ID, result.Error)
		}
		if result.RowsAffected != 1 {
			continue
		}
		job.State, job.LockedUntil = JobStateRunning, &lockUntil
		locked = append(locked, job)
	}
	return locked, nil
}

// deliver executes one claimed job and records its outcome.
func (s *Service) deliver(ctx context.Context, job database.SyncJob) {
	result, err := s.execute(ctx, job)
	if err == nil {
		s.markDone(ctx, job, result)
		return
	}
	s.markFailure(ctx, job, err)
}

func (s *Service) execute(ctx context.Context, job database.SyncJob) (Result, error) {
	var link database.SyncLink
	if err := s.db.WithContext(ctx).Where("id = ?", job.LinkID).First(&link).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Result{}, Permanent(errors.New("link removed before delivery"))
		}
		return Result{}, fmt.Errorf("load sync link: %w", err)
	}
	var connection database.SyncConnection
	if err := s.db.WithContext(ctx).Where("id = ?", link.ConnectionID).First(&connection).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Result{}, Permanent(errors.New("connection removed before delivery"))
		}
		return Result{}, fmt.Errorf("load sync connection: %w", err)
	}
	secret, err := s.sealer.Open(connection.Secret)
	if err != nil {
		return Result{}, Permanent(err)
	}
	var payload map[string]string
	if err := decodeSecret(secret, &payload); err != nil {
		return Result{}, Permanent(err)
	}
	if !s.providerAvailable(connection.Provider) {
		return Result{}, fmt.Errorf("%w: provider %s is not enabled", errProviderDisabled, connection.Provider)
	}
	target, ok := s.target(connection.Provider)
	if !ok {
		return Result{}, fmt.Errorf("%w: provider %s is not enabled", errProviderDisabled, connection.Provider)
	}
	releasePayload, err := s.payloadFor(ctx, job, link, connection, payload)
	if err != nil {
		return Result{}, err
	}
	conn := Connection{ID: connection.ID, Name: connection.Name, BaseURL: connection.BaseURL, Secret: payload}
	switch job.Action {
	case JobActionPublish:
		return target.Publish(ctx, conn, releasePayload)
	case JobActionYank:
		previous, err := s.previousResult(ctx, job)
		if err != nil {
			return Result{}, err
		}
		if err := target.Yank(ctx, conn, releasePayload, previous); err != nil {
			return Result{}, err
		}
		return previous, nil
	default:
		return Result{}, Permanent(fmt.Errorf("unknown sync action %q", job.Action))
	}
}

// payloadFor returns the payload a job delivers. A delete enqueues its yank in
// the same transaction that removes the release, so the job carries a snapshot;
// every other job reads the live release.
func (s *Service) payloadFor(ctx context.Context, job database.SyncJob, link database.SyncLink, connection database.SyncConnection, _ map[string]string) (ReleasePayload, error) {
	if snapshot, err := decodeSnapshot(job.Payload); err != nil {
		return ReleasePayload{}, Permanent(err)
	} else if snapshot.ProductID != "" || snapshot.Version != "" {
		return s.payloadFromSnapshot(job.ReleaseID, snapshot, link, connection), nil
	}
	return s.buildPayload(ctx, job, link, connection, nil)
}

// payloadFromSnapshot rebuilds the destination payload without the release row.
func (s *Service) payloadFromSnapshot(releaseID string, snapshot PayloadSnapshot, link database.SyncLink, connection database.SyncConnection) ReleasePayload {
	payload := ReleasePayload{
		ProductSlug: snapshot.ProductSlug,
		ProductName: snapshot.ProductName,
		ProductID:   snapshot.ProductID,
		ReleaseID:   releaseID,
		Version:     snapshot.Version,
		Title:       snapshot.Title,
		Notes:       snapshot.Notes,
		Channels:    snapshot.Channels,
		Prerelease:  snapshot.Prerelease,
		Draft:       snapshot.Draft,
		Artifacts:   snapshot.Artifacts,
		Link:        Repository{Repository: link.Repository, BaseURL: connection.BaseURL},
	}
	if len(snapshot.Artifacts) > 0 {
		payload.AssetURL = snapshot.Artifacts[0].URL
	}
	return payload
}

// buildPayload assembles the destination-neutral release description.
func (s *Service) buildPayload(ctx context.Context, job database.SyncJob, link database.SyncLink, connection database.SyncConnection, _ map[string]string) (ReleasePayload, error) {
	if s.cfg.Releases == nil {
		return ReleasePayload{}, Permanent(errors.New("release service is unavailable"))
	}
	release, err := s.cfg.Releases.GetRelease(link.ProductID, job.ReleaseID)
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			return ReleasePayload{}, Permanent(errors.New("release removed before delivery"))
		}
		return ReleasePayload{}, err
	}
	product, err := s.cfg.Releases.GetProductForSync(ctx, link.ProductID)
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			return ReleasePayload{}, Permanent(errors.New("product removed before delivery"))
		}
		return ReleasePayload{}, err
	}
	payload := ReleasePayload{
		ProductSlug: product.Slug,
		ProductName: product.Name,
		ProductID:   product.ID,
		ReleaseID:   release.ID,
		Version:     release.Version,
		Title:       releaseTitle(release, product),
		Notes:       releaseNotes(release),
		Channels:    channelNames(release),
		Prerelease:  link.Prerelease,
		Draft:       link.Draft,
		Link:        Repository{Repository: link.Repository, BaseURL: connection.BaseURL},
	}
	if !payload.Prerelease {
		for _, name := range payload.Channels {
			if name != string(database.ReleaseChannelStable) {
				payload.Prerelease = true
				break
			}
		}
	}
	// Every downloadable artifact is listed in the notes table; only one is
	// uploaded, because a destination release expresses one download.
	payload.Artifacts = s.artifactLinks(release)
	if len(payload.Artifacts) > 0 {
		payload.AssetURL = payload.Artifacts[0].URL
	}
	artifact, hasAsset := selectAsset(release)
	if !hasAsset || s.cfg.MaxAssetBytes == 0 || s.cfg.Artifacts == nil {
		return payload, nil
	}
	if artifact.Size > s.cfg.MaxAssetBytes {
		return payload, nil
	}
	objectKey := artifact.ObjectKey
	store := s.cfg.Artifacts
	payload.Asset = &Asset{
		Name:     artifact.FileName,
		MimeType: artifact.MimeType,
		Size:     artifact.Size,
		Hash:     artifact.Hash,
		Open: func(ctx context.Context) (io.ReadCloser, error) {
			reader, _, err := store.Open(ctx, objectKey)
			return reader, err
		},
	}
	return payload, nil
}

func (s *Service) artifactURL(artifact database.ReleaseArtifact) string {
	if artifact.DownloadURL != "" {
		return artifact.DownloadURL
	}
	base := strings.TrimRight(s.cfg.DownloadBaseURL, "/")
	if base == "" && s.cfg.Releases != nil {
		base = strings.TrimRight(s.cfg.Releases.DownloadEndpointBaseURL(), "/")
	}
	if base == "" {
		return ""
	}
	return base + "/artifacts/" + artifact.ID + "/download"
}

// artifactLinks returns every non-expired downloadable artifact, ordered by
// platform then architecture, with its public download URL. Artifacts without a
// usable URL are skipped so the notes table never renders a dead link.
func (s *Service) artifactLinks(release *database.Release) []ArtifactLink {
	artifacts := make([]database.ReleaseArtifact, 0, len(release.Artifacts))
	for _, artifact := range release.Artifacts {
		if artifact.ExpiredAt != nil {
			continue
		}
		if strings.TrimSpace(artifact.ObjectKey) == "" && strings.TrimSpace(artifact.DownloadURL) == "" {
			continue
		}
		artifacts = append(artifacts, artifact)
	}
	sort.SliceStable(artifacts, func(i, j int) bool {
		if artifacts[i].Platform != artifacts[j].Platform {
			return artifacts[i].Platform < artifacts[j].Platform
		}
		if artifacts[i].Architecture != artifacts[j].Architecture {
			return artifacts[i].Architecture < artifacts[j].Architecture
		}
		return artifacts[i].FileName < artifacts[j].FileName
	})
	links := make([]ArtifactLink, 0, len(artifacts))
	for _, artifact := range artifacts {
		url := s.artifactURL(artifact)
		if url == "" {
			continue
		}
		links = append(links, ArtifactLink{
			Name:         artifact.FileName,
			Platform:     artifact.Platform,
			Architecture: artifact.Architecture,
			Size:         artifact.Size,
			URL:          url,
		})
	}
	return links
}

// selectAsset picks the artifact a destination can upload: the first by
// platform then architecture that is object-backed and not expired.
func selectAsset(release *database.Release) (database.ReleaseArtifact, bool) {
	candidates := make([]database.ReleaseArtifact, 0, len(release.Artifacts))
	for _, artifact := range release.Artifacts {
		if artifact.ExpiredAt != nil || strings.TrimSpace(artifact.ObjectKey) == "" {
			continue
		}
		candidates = append(candidates, artifact)
	}
	if len(candidates) == 0 {
		return database.ReleaseArtifact{}, false
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Platform != candidates[j].Platform {
			return candidates[i].Platform < candidates[j].Platform
		}
		return candidates[i].Architecture < candidates[j].Architecture
	})
	return candidates[0], true
}

// previousResult returns the recorded result of the publish job for a yank.
func (s *Service) previousResult(ctx context.Context, job database.SyncJob) (Result, error) {
	var previous database.SyncJob
	err := s.db.WithContext(ctx).
		Where("link_id = ? AND release_id = ? AND action = ? AND state = ?", job.LinkID, job.ReleaseID, JobActionPublish, JobStateDone).
		Order("updated_at DESC").First(&previous).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Result{}, nil
		}
		return Result{}, fmt.Errorf("load previous sync result: %w", err)
	}
	return Result{ExternalID: previous.ExternalID, ExternalURL: previous.ExternalURL}, nil
}

func (s *Service) markDone(ctx context.Context, job database.SyncJob, result Result) {
	now := time.Now().UTC()
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&database.SyncJob{}).Where("id = ?", job.ID).Updates(map[string]any{
			"state":        JobStateDone,
			"external_id":  result.ExternalID,
			"external_url": result.ExternalURL,
			"last_error":   "",
			"locked_until": nil,
			"updated_at":   now,
		}).Error; err != nil {
			return err
		}
		if job.Action != JobActionPublish {
			return nil
		}
		return tx.Model(&database.SyncLink{}).Where("id = ?", job.LinkID).
			Updates(map[string]any{"last_synced_at": now}).Error
	})
	if err != nil {
		s.log.Warn("record sync delivery", "job_id", job.ID, "error", err)
	}
}

func (s *Service) markFailure(ctx context.Context, job database.SyncJob, failure error) {
	now := time.Now().UTC()
	attempts := job.Attempts + 1
	updates := map[string]any{
		"attempts":     attempts,
		"last_error":   truncate(failure.Error(), 1000),
		"locked_until": nil,
		"updated_at":   now,
	}
	switch {
	case errors.Is(failure, errProviderDisabled):
		// The adapter is disabled, not broken: keep the job pending without
		// consuming attempts so it resumes when the adapter is re-enabled.
		updates["attempts"] = job.Attempts
		updates["state"] = JobStatePending
		updates["next_attempt"] = now.Add(s.cfg.PollInterval)
	case errors.Is(failure, ErrPermanent):
		updates["state"] = JobStateFailed
		updates["next_attempt"] = now
	case attempts >= s.cfg.MaxAttempts:
		updates["state"] = JobStateFailed
		updates["next_attempt"] = now
	default:
		backoff := s.backoff(attempts)
		var rateLimited *RateLimitError
		if errors.As(failure, &rateLimited) && rateLimited.RetryAfter > backoff {
			backoff = rateLimited.RetryAfter
		}
		if backoff > s.cfg.RetryCap {
			backoff = s.cfg.RetryCap
		}
		updates["state"] = JobStatePending
		updates["next_attempt"] = now.Add(backoff)
	}
	if err := s.db.WithContext(ctx).Model(&database.SyncJob{}).Where("id = ?", job.ID).Updates(updates).Error; err != nil {
		s.log.Warn("record sync failure", "job_id", job.ID, "error", err)
	}
}

func (s *Service) backoff(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	delay := s.cfg.RetryBase
	for i := 1; i < attempts; i++ {
		delay *= 2
		if delay >= s.cfg.RetryCap {
			return s.cfg.RetryCap
		}
	}
	return delay
}

// Start runs the dispatcher until ctx is done or Stop is called.
func (s *Service) Start(ctx context.Context) {
	if s == nil || s.db == nil {
		return
	}
	s.startMu.Lock()
	if s.cancel != nil {
		s.startMu.Unlock()
		return
	}
	loopCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	s.cancel, s.done = cancel, done
	s.startMu.Unlock()

	go func() {
		defer close(done)
		ticker := time.NewTicker(s.cfg.PollInterval)
		defer ticker.Stop()
		for {
			processed, err := s.RunOnce(loopCtx)
			if err != nil && !errors.Is(err, context.Canceled) {
				s.log.Warn("sync dispatcher tick failed", "error", err)
			} else if processed == 0 {
				s.log.Debug("sync dispatcher tick idle")
			}
			select {
			case <-loopCtx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// Stop cancels the dispatcher loop and waits for the in-flight tick.
func (s *Service) Stop(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.startMu.Lock()
	cancel, done := s.cancel, s.done
	s.cancel, s.done = nil, nil
	s.startMu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
