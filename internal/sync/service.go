package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"src.solsynth.dev/solsynth/distribution/internal/database"
	"src.solsynth.dev/solsynth/distribution/internal/module"
	"src.solsynth.dev/solsynth/distribution/internal/service"
)

// ErrSyncDisabled reports a call against a deployment where the sync module is
// not enabled. Every method on a nil *Service returns it.
var ErrSyncDisabled = errors.New("sync module is not enabled")

const (
	JobStatePending = "pending"
	JobStateRunning = "running"
	JobStateDone    = "done"
	JobStateFailed  = "failed"

	JobActionPublish = "publish"
	JobActionYank    = "yank"

	// jobLock is how long a claimed job stays invisible to other dispatchers.
	jobLock = 2 * time.Minute
)

// errProviderDisabled marks a delivery whose destination adapter is not part of
// the resolved module set. The job stays pending instead of failing, so it
// resumes when the adapter is re-enabled.
var errProviderDisabled = errors.New("sync provider disabled")

// Options are the validated sync tunables from the process config.
type Options struct {
	PollInterval  time.Duration
	RetryBase     time.Duration
	RetryCap      time.Duration
	MaxAttempts   int
	MaxAssetBytes int64
	BatchSize     int

	// Releases is optional. When set the dispatcher reads localized product
	// and release rows and the configured download origin through it.
	Releases *service.ReleaseService
	// Artifacts streams release artifacts to destinations that accept uploads.
	Artifacts service.ArtifactReader
	// DownloadBaseURL is the public origin used to build artifact download
	// URLs embedded in destination releases.
	DownloadBaseURL string
}

// ConnectionInput creates or updates a destination credential. An empty Secret
// on update keeps the stored credential.
type ConnectionInput struct {
	Provider string
	Name     string
	BaseURL  string
	Secret   map[string]string
}

// LinkInput creates or updates a product-to-destination link.
type LinkInput struct {
	ConnectionID string
	Repository   string
	Channels     ChannelFilter
	Draft        bool
	Prerelease   bool
	// Enabled defaults to true on create and keeps the stored value on update
	// when nil.
	Enabled *bool
}

// ChannelFilter selects which release channels a link mirrors. An empty
// Include list means every channel.
type ChannelFilter struct {
	Include []string `json:"include,omitempty"`
	Exclude []string `json:"exclude,omitempty"`
}

// SyncConnectionView is the API representation of a connection. It never
// carries the sealed or plaintext secret.
type SyncConnectionView struct {
	ID          string    `json:"id"`
	PublisherID string    `json:"publisher_id"`
	Provider    string    `json:"provider"`
	Name        string    `json:"name"`
	BaseURL     string    `json:"base_url,omitempty"`
	HasSecret   bool      `json:"has_secret"`
	CreatedBy   string    `json:"created_by,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// SyncLinkView is the API representation of a link.
type SyncLinkView struct {
	ID           string        `json:"id"`
	ProductID    string        `json:"product_id"`
	ConnectionID string        `json:"connection_id"`
	Provider     string        `json:"provider"`
	Connection   string        `json:"connection"`
	Repository   string        `json:"repository"`
	Channels     ChannelFilter `json:"channels"`
	Draft        bool          `json:"draft"`
	Prerelease   bool          `json:"prerelease"`
	Enabled      bool          `json:"enabled"`
	LastSyncedAt *time.Time    `json:"last_synced_at,omitempty"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
}

// SyncJobView is the API representation of one outbox delivery.
type SyncJobView struct {
	ID                string    `json:"id"`
	LinkID            string    `json:"link_id"`
	ReleaseID         string    `json:"release_id"`
	Version           string    `json:"version"`
	Action            string    `json:"action"`
	State             string    `json:"state"`
	Attempts          int       `json:"attempts"`
	NextAttempt       time.Time `json:"next_attempt_at"`
	ExternalURL       string    `json:"external_url,omitempty"`
	LastError         string    `json:"last_error,omitempty"`
	ProviderAvailable bool      `json:"provider_available"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// JobQuery filters a job listing.
type JobQuery struct {
	State  string
	Limit  int
	Offset int
}

// JobPage is one page of jobs.
type JobPage struct {
	Data   []SyncJobView `json:"data"`
	Total  int64         `json:"total"`
	Limit  int           `json:"limit"`
	Offset int           `json:"offset"`
}

// Service owns the sync outbox, credentials, and the dispatcher loop.
type Service struct {
	db      *gorm.DB
	sealer  *Sealer
	targets *module.PortRegistry
	cfg     Options
	log     *slog.Logger

	mu      sync.RWMutex
	enabled map[string]bool
	live    []string

	startMu sync.Mutex
	cancel  context.CancelFunc
	done    chan struct{}
}

// NewService builds the engine. targets may be nil in tests that never
// dispatch.
func NewService(db *gorm.DB, sealer *Sealer, targets *module.PortRegistry, opts Options, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	if opts.BatchSize < 1 {
		opts.BatchSize = 16
	}
	if opts.MaxAttempts < 1 {
		opts.MaxAttempts = 8
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = 5 * time.Second
	}
	if opts.RetryBase <= 0 {
		opts.RetryBase = 30 * time.Second
	}
	if opts.RetryCap <= 0 {
		opts.RetryCap = time.Hour
	}
	return &Service{db: db, sealer: sealer, targets: targets, cfg: opts, log: log, enabled: map[string]bool{}}
}

// Configure records which provider module IDs are live in this deployment.
func (s *Service) Configure(enabled map[string]bool) {
	if s == nil {
		return
	}
	live := make([]string, 0, len(enabled))
	if s.targets != nil {
		for _, provider := range s.targets.Providers(PortSyncTargets) {
			if enabled[provider.ModuleID] {
				live = append(live, provider.ModuleID)
			}
		}
	}
	sort.Strings(live)
	s.mu.Lock()
	s.enabled = make(map[string]bool, len(enabled))
	for id, on := range enabled {
		s.enabled[id] = on
	}
	s.live = live
	s.mu.Unlock()
}

// Providers returns the sorted live syncTargets provider IDs.
func (s *Service) Providers() []string {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, len(s.live))
	copy(out, s.live)
	return out
}

func (s *Service) providerAvailable(provider string) bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.enabled[provider] {
		return false
	}
	for _, id := range s.live {
		if id == provider {
			return true
		}
	}
	return false
}

func (s *Service) target(provider string) (Target, bool) {
	if s == nil || s.targets == nil {
		return nil, false
	}
	return module.Select[Target](s.targets, PortSyncTargets, provider)
}

// EnqueueForRelease inserts one outbox row per matching link. It must run
// inside the caller's transaction so a release status change and its
// deliveries commit together.
func (s *Service) EnqueueForRelease(tx *gorm.DB, productID, releaseID, action string, channelNames []string) error {
	if s == nil {
		return ErrSyncDisabled
	}
	if tx == nil {
		tx = s.db
	}
	productID, releaseID = strings.TrimSpace(productID), strings.TrimSpace(releaseID)
	if productID == "" || releaseID == "" {
		return fmt.Errorf("%w: product and release ids are required", service.ErrValidation)
	}
	if action != JobActionPublish && action != JobActionYank {
		return fmt.Errorf("%w: unknown sync action %q", service.ErrValidation, action)
	}
	var links []database.SyncLink
	if err := tx.Where("product_id = ? AND enabled = ?", productID, true).Find(&links).Error; err != nil {
		return fmt.Errorf("load sync links: %w", err)
	}
	if len(links) == 0 {
		return nil
	}
	connectionIDs := make([]string, 0, len(links))
	for _, link := range links {
		connectionIDs = append(connectionIDs, link.ConnectionID)
	}
	var connections []database.SyncConnection
	if err := tx.Where("id IN ?", connectionIDs).Find(&connections).Error; err != nil {
		return fmt.Errorf("load sync connections: %w", err)
	}
	providers := make(map[string]string, len(connections))
	for _, connection := range connections {
		providers[connection.ID] = connection.Provider
	}
	now := time.Now().UTC()
	for _, link := range links {
		provider, ok := providers[link.ConnectionID]
		if !ok || !s.providerAvailable(provider) {
			// A destination whose adapter is disabled keeps its rows and
			// resumes when the adapter is re-enabled.
			continue
		}
		filter, err := decodeChannelFilter(link.Channels)
		if err != nil {
			return err
		}
		if !filter.matches(channelNames) {
			continue
		}
		job := database.SyncJob{
			ID:          uuid.NewString(),
			LinkID:      link.ID,
			ReleaseID:   releaseID,
			Action:      action,
			State:       JobStatePending,
			NextAttempt: now,
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&job).Error; err != nil {
			return fmt.Errorf("enqueue sync job: %w", err)
		}
	}
	return nil
}

// CreateConnection stores a publisher-wide destination credential.
func (s *Service) CreateConnection(ctx context.Context, publisherID string, in ConnectionInput) (*SyncConnectionView, error) {
	if s == nil {
		return nil, ErrSyncDisabled
	}
	publisherID = strings.TrimSpace(publisherID)
	if publisherID == "" {
		return nil, fmt.Errorf("%w: publisher is required", service.ErrValidation)
	}
	provider := strings.TrimSpace(in.Provider)
	if !s.providerAvailable(provider) {
		return nil, fmt.Errorf("%w: provider %s is not enabled", service.ErrValidation, provider)
	}
	name := strings.TrimSpace(in.Name)
	if name == "" || len(name) > 128 {
		return nil, fmt.Errorf("%w: connection name must be 1-128 characters", service.ErrValidation)
	}
	secret := make(map[string]string, len(in.Secret))
	for key, value := range in.Secret {
		if value = strings.TrimSpace(value); value != "" {
			secret[key] = value
		}
	}
	if err := s.validateConnection(provider, name, in.BaseURL, secret); err != nil {
		return nil, err
	}
	sealed, err := s.sealSecret(secret)
	if err != nil {
		return nil, err
	}
	connection := database.SyncConnection{
		ID:          uuid.NewString(),
		PublisherID: publisherID,
		Provider:    provider,
		Name:        name,
		BaseURL:     strings.TrimSpace(in.BaseURL),
		Secret:      sealed,
		CreatedBy:   service.AccountID(ctx),
	}
	if err := s.db.WithContext(ctx).Create(&connection).Error; err != nil {
		if isUniqueConstraint(err) {
			return nil, fmt.Errorf("%w: connection already exists", service.ErrConflict)
		}
		return nil, fmt.Errorf("create sync connection: %w", err)
	}
	view := connectionView(connection)
	return &view, nil
}

// UpdateConnection replaces a connection. An empty Secret keeps the stored
// credential.
func (s *Service) UpdateConnection(ctx context.Context, publisherID, id string, in ConnectionInput) (*SyncConnectionView, error) {
	if s == nil {
		return nil, ErrSyncDisabled
	}
	connection, err := s.loadConnection(ctx, publisherID, id)
	if err != nil {
		return nil, err
	}
	provider := strings.TrimSpace(in.Provider)
	if provider == "" {
		provider = connection.Provider
	}
	if !s.providerAvailable(provider) {
		return nil, fmt.Errorf("%w: provider %s is not enabled", service.ErrValidation, provider)
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = connection.Name
	}
	if len(name) > 128 {
		return nil, fmt.Errorf("%w: connection name must be 1-128 characters", service.ErrValidation)
	}
	secret, err := s.decryptSecret(connection.Secret)
	if err != nil {
		return nil, err
	}
	replaced := false
	for key, value := range in.Secret {
		if value = strings.TrimSpace(value); value != "" {
			secret[key] = value
			replaced = true
		}
	}
	if !replaced {
		// No usable new credential was supplied; keep the stored one.
		if secret, err = s.decryptSecret(connection.Secret); err != nil {
			return nil, err
		}
	}
	baseURL := strings.TrimSpace(in.BaseURL)
	if baseURL == "" && provider == connection.Provider {
		baseURL = connection.BaseURL
	}
	if err := s.validateConnection(provider, name, baseURL, secret); err != nil {
		return nil, err
	}
	sealed, err := s.sealSecret(secret)
	if err != nil {
		return nil, err
	}
	updates := map[string]any{
		"provider":   provider,
		"name":       name,
		"base_url":   baseURL,
		"secret":     sealed,
		"updated_at": time.Now().UTC(),
	}
	result := s.db.WithContext(ctx).Model(&database.SyncConnection{}).
		Where("id = ? AND publisher_id = ?", connection.ID, connection.PublisherID).
		Updates(updates)
	if result.Error != nil {
		if isUniqueConstraint(result.Error) {
			return nil, fmt.Errorf("%w: connection already exists", service.ErrConflict)
		}
		return nil, fmt.Errorf("update sync connection: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return nil, fmt.Errorf("%w: connection changed concurrently", service.ErrConflict)
	}
	connection.Provider, connection.Name, connection.BaseURL, connection.Secret = provider, name, baseURL, sealed
	connection.UpdatedAt = time.Now().UTC()
	view := connectionView(*connection)
	return &view, nil
}

// DeleteConnection removes a credential that no link references.
func (s *Service) DeleteConnection(ctx context.Context, publisherID, id string) error {
	if s == nil {
		return ErrSyncDisabled
	}
	connection, err := s.loadConnection(ctx, publisherID, id)
	if err != nil {
		return err
	}
	var links int64
	if err := s.db.WithContext(ctx).Model(&database.SyncLink{}).Where("connection_id = ?", connection.ID).Count(&links).Error; err != nil {
		return fmt.Errorf("count sync links: %w", err)
	}
	if links > 0 {
		return fmt.Errorf("%w: connection still has %d links; delete them first", service.ErrConflict, links)
	}
	if err := s.db.WithContext(ctx).Where("id = ?", connection.ID).Delete(&database.SyncConnection{}).Error; err != nil {
		return fmt.Errorf("delete sync connection: %w", err)
	}
	return nil
}

// ListConnections returns a publisher's connections.
func (s *Service) ListConnections(ctx context.Context, publisherID string) ([]SyncConnectionView, error) {
	if s == nil {
		return nil, ErrSyncDisabled
	}
	var connections []database.SyncConnection
	if err := s.db.WithContext(ctx).Where("publisher_id = ?", strings.TrimSpace(publisherID)).Order("name").Find(&connections).Error; err != nil {
		return nil, fmt.Errorf("list sync connections: %w", err)
	}
	views := make([]SyncConnectionView, 0, len(connections))
	for _, connection := range connections {
		views = append(views, connectionView(connection))
	}
	return views, nil
}

// CreateLink maps a product to one destination repository.
func (s *Service) CreateLink(ctx context.Context, productID string, in LinkInput) (*SyncLinkView, error) {
	if s == nil {
		return nil, ErrSyncDisabled
	}
	product, connection, filter, repository, err := s.validateLink(ctx, productID, in)
	if err != nil {
		return nil, err
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	link := database.SyncLink{
		ID:           uuid.NewString(),
		ProductID:    product.ID,
		ConnectionID: connection.ID,
		Repository:   repository,
		Channels:     encodeChannelFilter(filter),
		Draft:        in.Draft,
		Prerelease:   in.Prerelease,
		Enabled:      enabled,
	}
	if err := s.db.WithContext(ctx).Create(&link).Error; err != nil {
		if isUniqueConstraint(err) {
			return nil, fmt.Errorf("%w: link already exists", service.ErrConflict)
		}
		return nil, fmt.Errorf("create sync link: %w", err)
	}
	view := linkView(link, *connection)
	return &view, nil
}

// UpdateLink replaces a link. A nil Enabled keeps the stored value.
func (s *Service) UpdateLink(ctx context.Context, productID, id string, in LinkInput) (*SyncLinkView, error) {
	if s == nil {
		return nil, ErrSyncDisabled
	}
	link, err := s.loadLink(ctx, strings.TrimSpace(productID), id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.ConnectionID) == "" {
		in.ConnectionID = link.ConnectionID
	}
	if strings.TrimSpace(in.Repository) == "" && strings.TrimSpace(link.Repository) != "" {
		in.Repository = link.Repository
	}
	if in.Channels.Include == nil && in.Channels.Exclude == nil {
		stored, err := decodeChannelFilter(link.Channels)
		if err != nil {
			return nil, err
		}
		in.Channels = stored
	}
	product, connection, filter, repository, err := s.validateLink(ctx, productID, in)
	if err != nil {
		return nil, err
	}
	enabled := link.Enabled
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	updates := map[string]any{
		"connection_id": connection.ID,
		"repository":    repository,
		"channels":      encodeChannelFilter(filter),
		"draft":         in.Draft,
		"prerelease":    in.Prerelease,
		"enabled":       enabled,
		"updated_at":    time.Now().UTC(),
	}
	result := s.db.WithContext(ctx).Model(&database.SyncLink{}).
		Where("id = ? AND product_id = ?", link.ID, product.ID).
		Updates(updates)
	if result.Error != nil {
		if isUniqueConstraint(result.Error) {
			return nil, fmt.Errorf("%w: link already exists", service.ErrConflict)
		}
		return nil, fmt.Errorf("update sync link: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return nil, fmt.Errorf("%w: link changed concurrently", service.ErrConflict)
	}
	link.ConnectionID, link.Repository, link.Channels = connection.ID, repository, encodeChannelFilter(filter)
	link.Draft, link.Prerelease, link.Enabled = in.Draft, in.Prerelease, enabled
	link.UpdatedAt = time.Now().UTC()
	view := linkView(*link, *connection)
	return &view, nil
}

// DeleteLink removes a link. Its jobs stay as delivery history.
func (s *Service) DeleteLink(ctx context.Context, productID, id string) error {
	if s == nil {
		return ErrSyncDisabled
	}
	link, err := s.loadLink(ctx, strings.TrimSpace(productID), id)
	if err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Where("id = ?", link.ID).Delete(&database.SyncLink{}).Error; err != nil {
		return fmt.Errorf("delete sync link: %w", err)
	}
	return nil
}

// ListLinks returns a product's links with their destination provider.
func (s *Service) ListLinks(ctx context.Context, productID string) ([]SyncLinkView, error) {
	if s == nil {
		return nil, ErrSyncDisabled
	}
	var links []database.SyncLink
	if err := s.db.WithContext(ctx).Where("product_id = ?", strings.TrimSpace(productID)).Order("created_at").Find(&links).Error; err != nil {
		return nil, fmt.Errorf("list sync links: %w", err)
	}
	connections, err := s.connectionsByID(ctx, links)
	if err != nil {
		return nil, err
	}
	views := make([]SyncLinkView, 0, len(links))
	for _, link := range links {
		connection, ok := connections[link.ConnectionID]
		if !ok {
			connection = database.SyncConnection{ID: link.ConnectionID}
		}
		views = append(views, linkView(link, connection))
	}
	return views, nil
}

// ListJobs returns a page of a product's deliveries.
func (s *Service) ListJobs(ctx context.Context, productID string, query JobQuery) (*JobPage, error) {
	if s == nil {
		return nil, ErrSyncDisabled
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	offset := query.Offset
	if offset < 0 {
		offset = 0
	}
	base := s.db.WithContext(ctx).Table("sync_jobs").
		Joins("JOIN sync_links ON sync_links.id = sync_jobs.link_id").
		Where("sync_links.product_id = ?", strings.TrimSpace(productID))
	if state := strings.TrimSpace(query.State); state != "" {
		base = base.Where("sync_jobs.state = ?", state)
	}
	var total int64
	if err := base.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, fmt.Errorf("count sync jobs: %w", err)
	}
	var jobs []database.SyncJob
	if err := base.Session(&gorm.Session{}).Select("sync_jobs.*").
		Order("sync_jobs.created_at DESC").Limit(limit).Offset(offset).
		Find(&jobs).Error; err != nil {
		return nil, fmt.Errorf("list sync jobs: %w", err)
	}
	versions, err := s.releaseVersions(ctx, jobs)
	if err != nil {
		return nil, err
	}
	data := make([]SyncJobView, 0, len(jobs))
	for _, job := range jobs {
		data = append(data, s.jobView(job, versions[job.ReleaseID]))
	}
	return &JobPage{Data: data, Total: total, Limit: limit, Offset: offset}, nil
}

// RetryJob re-arms a failed delivery.
func (s *Service) RetryJob(ctx context.Context, productID, jobID string) (*SyncJobView, error) {
	if s == nil {
		return nil, ErrSyncDisabled
	}
	job, err := s.loadProductJob(ctx, strings.TrimSpace(productID), jobID)
	if err != nil {
		return nil, err
	}
	if job.State == JobStateRunning {
		return nil, fmt.Errorf("%w: delivery is in progress", service.ErrConflict)
	}
	now := time.Now().UTC()
	result := s.db.WithContext(ctx).Model(&database.SyncJob{}).
		Where("id = ?", job.ID).
		Updates(map[string]any{
			"state":        JobStatePending,
			"attempts":     0,
			"last_error":   "",
			"next_attempt": now,
			"locked_until": nil,
			"updated_at":   now,
		})
	if result.Error != nil {
		return nil, fmt.Errorf("retry sync job: %w", result.Error)
	}
	job.State, job.Attempts, job.LastError, job.NextAttempt, job.LockedUntil = JobStatePending, 0, "", now, nil
	job.UpdatedAt = now
	versions, err := s.releaseVersions(ctx, []database.SyncJob{*job})
	if err != nil {
		return nil, err
	}
	view := s.jobView(*job, versions[job.ReleaseID])
	return &view, nil
}

func (s *Service) validateConnection(provider, name, baseURL string, secret map[string]string) error {
	target, ok := s.target(provider)
	if !ok {
		return fmt.Errorf("%w: provider %s is not enabled", service.ErrValidation, provider)
	}
	conn := Connection{Name: name, BaseURL: strings.TrimSpace(baseURL), Secret: secret}
	if err := target.Validate(conn); err != nil {
		return err
	}
	return nil
}

func (s *Service) validateLink(ctx context.Context, productID string, in LinkInput) (*database.Product, *database.SyncConnection, ChannelFilter, string, error) {
	productID = strings.TrimSpace(productID)
	var product database.Product
	if err := s.db.WithContext(ctx).Where("id = ?", productID).First(&product).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ChannelFilter{}, "", fmt.Errorf("%w: product", service.ErrNotFound)
		}
		return nil, nil, ChannelFilter{}, "", fmt.Errorf("load product: %w", err)
	}
	connectionID := strings.TrimSpace(in.ConnectionID)
	if connectionID == "" {
		return nil, nil, ChannelFilter{}, "", fmt.Errorf("%w: connection_id is required", service.ErrValidation)
	}
	var connection database.SyncConnection
	if err := s.db.WithContext(ctx).Where("id = ?", connectionID).First(&connection).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ChannelFilter{}, "", fmt.Errorf("%w: connection", service.ErrNotFound)
		}
		return nil, nil, ChannelFilter{}, "", fmt.Errorf("load sync connection: %w", err)
	}
	if connection.PublisherID != product.PublisherID {
		return nil, nil, ChannelFilter{}, "", fmt.Errorf("%w: connection belongs to another publisher", service.ErrForbidden)
	}
	if !s.providerAvailable(connection.Provider) {
		return nil, nil, ChannelFilter{}, "", fmt.Errorf("%w: provider %s is not enabled", service.ErrValidation, connection.Provider)
	}
	filter, err := normalizeChannelFilter(in.Channels)
	if err != nil {
		return nil, nil, ChannelFilter{}, "", err
	}
	if err := s.validateChannelNames(ctx, product.ID, filter); err != nil {
		return nil, nil, ChannelFilter{}, "", err
	}
	repository := strings.TrimSpace(in.Repository)
	if connection.Provider == WebhookProviderID {
		if repository != "" {
			return nil, nil, ChannelFilter{}, "", fmt.Errorf("%w: webhook links have no repository", service.ErrValidation)
		}
	} else if err := validateRepository(repository); err != nil {
		return nil, nil, ChannelFilter{}, "", err
	}
	return &product, &connection, filter, repository, nil
}

func (s *Service) validateChannelNames(ctx context.Context, productID string, filter ChannelFilter) error {
	names := append(append([]string{}, filter.Include...), filter.Exclude...)
	if len(names) == 0 {
		return nil
	}
	var existing []string
	if err := s.db.WithContext(ctx).Model(&database.Channel{}).Where("app_id = ?", productID).Pluck("name", &existing).Error; err != nil {
		return fmt.Errorf("list product channels: %w", err)
	}
	known := make(map[string]bool, len(existing))
	for _, name := range existing {
		known[name] = true
	}
	for _, name := range names {
		if !known[name] {
			return fmt.Errorf("%w: channel %q does not exist on this product", service.ErrValidation, name)
		}
	}
	return nil
}

func (s *Service) loadConnection(ctx context.Context, publisherID, id string) (*database.SyncConnection, error) {
	var connection database.SyncConnection
	query := s.db.WithContext(ctx).Where("id = ?", strings.TrimSpace(id))
	if publisher := strings.TrimSpace(publisherID); publisher != "" {
		query = query.Where("publisher_id = ?", publisher)
	}
	if err := query.First(&connection).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: connection", service.ErrNotFound)
		}
		return nil, fmt.Errorf("load sync connection: %w", err)
	}
	return &connection, nil
}

func (s *Service) loadLink(ctx context.Context, productID, id string) (*database.SyncLink, error) {
	var link database.SyncLink
	query := s.db.WithContext(ctx).Where("id = ?", strings.TrimSpace(id))
	if productID != "" {
		query = query.Where("product_id = ?", productID)
	}
	if err := query.First(&link).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: link", service.ErrNotFound)
		}
		return nil, fmt.Errorf("load sync link: %w", err)
	}
	return &link, nil
}

func (s *Service) loadProductJob(ctx context.Context, productID, id string) (*database.SyncJob, error) {
	var job database.SyncJob
	err := s.db.WithContext(ctx).Model(&database.SyncJob{}).
		Joins("JOIN sync_links ON sync_links.id = sync_jobs.link_id").
		Where("sync_jobs.id = ? AND sync_links.product_id = ?", strings.TrimSpace(id), productID).
		Select("sync_jobs.*").First(&job).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: job", service.ErrNotFound)
		}
		return nil, fmt.Errorf("load sync job: %w", err)
	}
	return &job, nil
}

func (s *Service) connectionsByID(ctx context.Context, links []database.SyncLink) (map[string]database.SyncConnection, error) {
	out := make(map[string]database.SyncConnection, len(links))
	if len(links) == 0 {
		return out, nil
	}
	ids := make([]string, 0, len(links))
	for _, link := range links {
		ids = append(ids, link.ConnectionID)
	}
	var connections []database.SyncConnection
	if err := s.db.WithContext(ctx).Where("id IN ?", ids).Find(&connections).Error; err != nil {
		return nil, fmt.Errorf("load sync connections: %w", err)
	}
	for _, connection := range connections {
		out[connection.ID] = connection
	}
	return out, nil
}

func (s *Service) releaseVersions(ctx context.Context, jobs []database.SyncJob) (map[string]string, error) {
	out := make(map[string]string, len(jobs))
	if len(jobs) == 0 {
		return out, nil
	}
	ids := make([]string, 0, len(jobs))
	for _, job := range jobs {
		ids = append(ids, job.ReleaseID)
	}
	var releases []struct {
		ID      string
		Version string
	}
	if err := s.db.WithContext(ctx).Model(&database.Release{}).Select("id, version").Where("id IN ?", ids).Scan(&releases).Error; err != nil {
		return nil, fmt.Errorf("load release versions: %w", err)
	}
	for _, release := range releases {
		out[release.ID] = release.Version
	}
	return out, nil
}

func (s *Service) jobView(job database.SyncJob, version string) SyncJobView {
	return SyncJobView{
		ID:                job.ID,
		LinkID:            job.LinkID,
		ReleaseID:         job.ReleaseID,
		Version:           version,
		Action:            job.Action,
		State:             job.State,
		Attempts:          job.Attempts,
		NextAttempt:       job.NextAttempt,
		ExternalURL:       job.ExternalURL,
		LastError:         job.LastError,
		ProviderAvailable: s.jobProviderAvailable(job),
		CreatedAt:         job.CreatedAt,
		UpdatedAt:         job.UpdatedAt,
	}
}

func (s *Service) jobProviderAvailable(job database.SyncJob) bool {
	if s == nil {
		return false
	}
	var provider string
	if err := s.db.Model(&database.SyncLink{}).
		Select("sync_connections.provider").
		Joins("JOIN sync_connections ON sync_connections.id = sync_links.connection_id").
		Where("sync_links.id = ?", job.LinkID).Scan(&provider).Error; err != nil {
		return false
	}
	if provider == "" {
		return false
	}
	return s.providerAvailable(provider)
}

func connectionView(connection database.SyncConnection) SyncConnectionView {
	return SyncConnectionView{
		ID:          connection.ID,
		PublisherID: connection.PublisherID,
		Provider:    connection.Provider,
		Name:        connection.Name,
		BaseURL:     connection.BaseURL,
		HasSecret:   strings.TrimSpace(connection.Secret) != "",
		CreatedBy:   connection.CreatedBy,
		CreatedAt:   connection.CreatedAt,
		UpdatedAt:   connection.UpdatedAt,
	}
}

func linkView(link database.SyncLink, connection database.SyncConnection) SyncLinkView {
	filter, err := decodeChannelFilter(link.Channels)
	if err != nil {
		filter = ChannelFilter{}
	}
	return SyncLinkView{
		ID:           link.ID,
		ProductID:    link.ProductID,
		ConnectionID: link.ConnectionID,
		Provider:     connection.Provider,
		Connection:   connection.Name,
		Repository:   link.Repository,
		Channels:     filter,
		Draft:        link.Draft,
		Prerelease:   link.Prerelease,
		Enabled:      link.Enabled,
		LastSyncedAt: link.LastSyncedAt,
		CreatedAt:    link.CreatedAt,
		UpdatedAt:    link.UpdatedAt,
	}
}

func (s *Service) sealSecret(secret map[string]string) (string, error) {
	encoded, err := json.Marshal(secret)
	if err != nil {
		return "", fmt.Errorf("encode sync secret: %w", err)
	}
	sealed, err := s.sealer.Seal(encoded)
	if err != nil {
		return "", fmt.Errorf("seal sync secret: %w", err)
	}
	return sealed, nil
}

func (s *Service) decryptSecret(sealed string) (map[string]string, error) {
	if strings.TrimSpace(sealed) == "" {
		return map[string]string{}, nil
	}
	plain, err := s.sealer.Open(sealed)
	if err != nil {
		return nil, fmt.Errorf("%w: re-enter the connection secret", service.ErrDependency)
	}
	secret := map[string]string{}
	if err := json.Unmarshal(plain, &secret); err != nil {
		return nil, fmt.Errorf("%w: re-enter the connection secret", service.ErrDependency)
	}
	return secret, nil
}

func normalizeChannelFilter(filter ChannelFilter) (ChannelFilter, error) {
	out := ChannelFilter{Include: normalizeNames(filter.Include), Exclude: normalizeNames(filter.Exclude)}
	if len(out.Include) == 0 {
		out.Include = nil
	}
	if len(out.Exclude) == 0 {
		out.Exclude = nil
	}
	for _, name := range out.Include {
		for _, excluded := range out.Exclude {
			if name == excluded {
				return ChannelFilter{}, fmt.Errorf("%w: channel %q cannot be both included and excluded", service.ErrValidation, name)
			}
		}
	}
	return out, nil
}

func normalizeNames(names []string) []string {
	seen := make(map[string]bool, len(names))
	out := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// matches reports whether any release channel passes the filter.
func (f ChannelFilter) matches(channelNames []string) bool {
	if len(channelNames) == 0 {
		return len(f.Include) == 0
	}
	excluded := make(map[string]bool, len(f.Exclude))
	for _, name := range f.Exclude {
		excluded[name] = true
	}
	included := make(map[string]bool, len(f.Include))
	for _, name := range f.Include {
		included[name] = true
	}
	for _, name := range channelNames {
		if excluded[name] {
			continue
		}
		if len(included) == 0 || included[name] {
			return true
		}
	}
	return false
}

func encodeChannelFilter(filter ChannelFilter) database.JSONMap {
	encoded := database.JSONMap{}
	if len(filter.Include) > 0 {
		encoded["include"] = stringSlice(filter.Include)
	}
	if len(filter.Exclude) > 0 {
		encoded["exclude"] = stringSlice(filter.Exclude)
	}
	if len(encoded) == 0 {
		return nil
	}
	return encoded
}

func decodeChannelFilter(stored database.JSONMap) (ChannelFilter, error) {
	if len(stored) == 0 {
		return ChannelFilter{}, nil
	}
	encoded, err := json.Marshal(stored)
	if err != nil {
		return ChannelFilter{}, fmt.Errorf("%w: link channel filter is unreadable", service.ErrDependency)
	}
	var filter ChannelFilter
	if err := json.Unmarshal(encoded, &filter); err != nil {
		return ChannelFilter{}, fmt.Errorf("%w: link channel filter is unreadable", service.ErrDependency)
	}
	return ChannelFilter{Include: normalizeNames(filter.Include), Exclude: normalizeNames(filter.Exclude)}, nil
}

func stringSlice(values []string) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}

// validateRepository accepts "owner/name" with exactly one slash.
func validateRepository(repository string) error {
	repository = strings.TrimSpace(repository)
	if repository == "" {
		return fmt.Errorf("%w: repository is required", service.ErrValidation)
	}
	parts := strings.Split(repository, "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return fmt.Errorf("%w: repository must be owner/name", service.ErrValidation)
	}
	if strings.ContainsAny(repository, " \t\r\n") {
		return fmt.Errorf("%w: repository must not contain whitespace", service.ErrValidation)
	}
	return nil
}

func isUniqueConstraint(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique constraint") || strings.Contains(message, "duplicate key") || strings.Contains(message, "unique index")
}
