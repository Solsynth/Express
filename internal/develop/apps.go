package develop

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"src.solsynth.dev/solsynth/distribution/internal/database"
	"src.solsynth.dev/solsynth/distribution/internal/service"
)

// CustomAppInput is the create/update payload for a custom app. Pointer fields
// distinguish "leave unchanged" from "clear".
type CustomAppInput struct {
	Slug         *string                `json:"slug"`
	Name         *string                `json:"name"`
	Description  *string                `json:"description"`
	PictureID    *string                `json:"picture_id"`
	BackgroundID *string                `json:"background_id"`
	Status       *string                `json:"status"`
	Links        *CustomAppLinks        `json:"links"`
	OauthConfig  *CustomAppOauthConfig  `json:"oauth_config"`
	BoardWidgets *[]BoardWidgetManifest `json:"board_widgets"`
}

// SecretInput is the create/rotate payload for an app secret.
type SecretInput struct {
	Description string `json:"description"`
	// ExpiresIn is a Go duration string, matching the identity service's
	// TimeSpan request field.
	ExpiresIn string `json:"expires_in"`
	Type      string `json:"type"`
}

// ListApps lists the custom apps of a project.
func (s *Service) ListApps(projectID string) ([]CustomApp, error) {
	var apps []CustomApp
	if err := s.DB.Where("project_id = ?", projectID).Order("name ASC").Find(&apps).Error; err != nil {
		return nil, fmt.Errorf("list apps: %w", err)
	}
	if apps == nil {
		apps = []CustomApp{}
	}
	return apps, nil
}

// CreateApp adds a custom app to a project.
func (s *Service) CreateApp(ctx context.Context, projectID string, input CustomAppInput) (*CustomApp, error) {
	slug := strings.TrimSpace(deref(input.Slug))
	name := strings.TrimSpace(deref(input.Name))
	if slug == "" || name == "" {
		return nil, fmt.Errorf("%w: name and slug are required", service.ErrValidation)
	}
	if err := validateAppLengths(slug, name, deref(input.Description)); err != nil {
		return nil, err
	}
	app := &CustomApp{
		ID:          newUUID(),
		Slug:        slug,
		Name:        name,
		Description: deref(input.Description),
		Status:      CustomAppDeveloping,
		Links:       input.Links,
		OauthConfig: input.OauthConfig,
		ProjectID:   projectID,
	}
	if status := strings.TrimSpace(deref(input.Status)); status != "" {
		parsed, ok := ParseCustomAppStatus(status)
		if !ok {
			return nil, fmt.Errorf("%w: unknown app status %q", service.ErrValidation, status)
		}
		app.Status = parsed
	}
	var err error
	if app.Picture, err = s.ResolveFile(ctx, deref(input.PictureID), "picture"); err != nil {
		return nil, err
	}
	if app.Background, err = s.ResolveFile(ctx, deref(input.BackgroundID), "background"); err != nil {
		return nil, err
	}
	if err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(app).Error; err != nil {
			return fmt.Errorf("create app: %w", err)
		}
		if input.BoardWidgets != nil {
			for _, manifest := range *input.BoardWidgets {
				widget, err := widgetFromManifest(app.ID, manifest)
				if err != nil {
					return err
				}
				if err := tx.Create(widget).Error; err != nil {
					return fmt.Errorf("create board widget: %w", err)
				}
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return app, nil
}

// UpdateApp patches a custom app. A widget list replaces the existing set.
func (s *Service) UpdateApp(ctx context.Context, app *CustomApp, input CustomAppInput) (*CustomApp, error) {
	oldStatus := app.Status
	if input.Slug != nil {
		app.Slug = strings.TrimSpace(*input.Slug)
	}
	if input.Name != nil {
		app.Name = strings.TrimSpace(*input.Name)
	}
	if input.Description != nil {
		app.Description = *input.Description
	}
	if input.Links != nil {
		app.Links = input.Links
	}
	if input.OauthConfig != nil {
		app.OauthConfig = input.OauthConfig
	}
	if status := strings.TrimSpace(deref(input.Status)); status != "" {
		parsed, ok := ParseCustomAppStatus(status)
		if !ok {
			return nil, fmt.Errorf("%w: unknown app status %q", service.ErrValidation, status)
		}
		app.Status = parsed
	}
	if app.Slug == "" || app.Name == "" {
		return nil, fmt.Errorf("%w: name and slug are required", service.ErrValidation)
	}
	if err := validateAppLengths(app.Slug, app.Name, app.Description); err != nil {
		return nil, err
	}
	var err error
	if app.Picture, err = s.ResolveFile(ctx, deref(input.PictureID), "picture"); err != nil {
		return nil, err
	}
	if app.Background, err = s.ResolveFile(ctx, deref(input.BackgroundID), "background"); err != nil {
		return nil, err
	}

	if err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(app).Error; err != nil {
			return fmt.Errorf("update app: %w", err)
		}
		if input.BoardWidgets != nil {
			if err := tx.Where("app_id = ?", app.ID).Delete(&BoardWidget{}).Error; err != nil {
				return fmt.Errorf("replace board widgets: %w", err)
			}
			for _, manifest := range *input.BoardWidgets {
				widget, err := widgetFromManifest(app.ID, manifest)
				if err != nil {
					return err
				}
				if err := tx.Create(widget).Error; err != nil {
					return fmt.Errorf("create board widget: %w", err)
				}
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}

	// Leaving production makes the app's art private again.
	if oldStatus == CustomAppProduction && app.Status != CustomAppProduction {
		s.unsetFilePublic(ctx, app.Picture)
		s.unsetFilePublic(ctx, app.Background)
	}
	return app, nil
}

// unsetFilePublic revokes public read access on a previously published file.
// Failures are non-fatal: the app is already saved, and the file service
// retries its own consistency.
func (s *Service) unsetFilePublic(ctx context.Context, file *database.CloudFileReference) {
	if file == nil || file.Id == "" {
		return
	}
	if unseter, ok := s.Files.(interface {
		UnsetFilePublic(ctx context.Context, fileID string) error
	}); ok {
		_ = unseter.UnsetFilePublic(ctx, file.Id)
	}
}

// DeleteApp removes an app with its secrets, widgets and products.
func (s *Service) DeleteApp(app *CustomApp) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var productIDs []string
		if err := tx.Model(&AppProduct{}).Where("app_id = ?", app.ID).Pluck("id", &productIDs).Error; err != nil {
			return fmt.Errorf("list app products: %w", err)
		}
		if len(productIDs) > 0 {
			if err := tx.Where("product_id IN ?", productIDs).Delete(&AppProductState{}).Error; err != nil {
				return fmt.Errorf("delete app product states: %w", err)
			}
		}
		for _, model := range []any{&AppProduct{}, &CustomAppSecret{}, &BoardWidget{}} {
			if err := tx.Where("app_id = ?", app.ID).Delete(model).Error; err != nil {
				return fmt.Errorf("delete app children: %w", err)
			}
		}
		if err := tx.Delete(app).Error; err != nil {
			return fmt.Errorf("delete app: %w", err)
		}
		return nil
	})
}

// FindAppBySlug resolves an app by slug, hydrating its project and developer.
func (s *Service) FindAppBySlug(slug string) (*CustomApp, error) {
	var app CustomApp
	if err := s.DB.Preload("Project").Where("slug = ?", strings.TrimSpace(slug)).First(&app).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: app", service.ErrNotFound)
		}
		return nil, fmt.Errorf("load app: %w", err)
	}
	return &app, nil
}

// DiscoveryApp is one row of the public app catalog.
type DiscoveryApp struct {
	ID            string `json:"id"`
	Slug          string `json:"slug"`
	Title         string `json:"title"`
	Description   string `json:"description,omitempty"`
	ProductsCount int64  `json:"products_count"`
	WidgetsCount  int64  `json:"widgets_count"`
}

// DiscoverApps lists production apps with their product and widget counts.
func (s *Service) DiscoverApps(limit, offset int, search string) ([]DiscoveryApp, int64, error) {
	query := s.DB.Model(&CustomApp{}).Where("status = ?", CustomAppProduction)
	if search = strings.TrimSpace(search); search != "" {
		probe := "%" + strings.ToLower(search) + "%"
		query = query.Where("LOWER(slug) LIKE ? OR LOWER(name) LIKE ? OR LOWER(COALESCE(description, '')) LIKE ?", probe, probe, probe)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count apps: %w", err)
	}
	var apps []CustomApp
	if err := query.Order("name ASC, slug ASC").Limit(limit).Offset(offset).Find(&apps).Error; err != nil {
		return nil, 0, fmt.Errorf("list apps: %w", err)
	}
	out := make([]DiscoveryApp, 0, len(apps))
	for _, app := range apps {
		row := DiscoveryApp{ID: app.ID, Slug: app.Slug, Title: app.Name, Description: app.Description}
		if err := s.DB.Model(&AppProduct{}).Where("app_id = ?", app.ID).Count(&row.ProductsCount).Error; err != nil {
			return nil, 0, fmt.Errorf("count app products: %w", err)
		}
		if err := s.DB.Model(&BoardWidget{}).Where("app_id = ?", app.ID).Count(&row.WidgetsCount).Error; err != nil {
			return nil, 0, fmt.Errorf("count app widgets: %w", err)
		}
		out = append(out, row)
	}
	return out, total, nil
}

// --- Secrets ---

// ListSecrets lists an app's secrets newest first.
func (s *Service) ListSecrets(appID string) ([]CustomAppSecret, error) {
	var secrets []CustomAppSecret
	if err := s.DB.Where("app_id = ?", appID).Order("created_at DESC").Find(&secrets).Error; err != nil {
		return nil, fmt.Errorf("list app secrets: %w", err)
	}
	if secrets == nil {
		secrets = []CustomAppSecret{}
	}
	return secrets, nil
}

// LoadSecret resolves one secret of an app.
func (s *Service) LoadSecret(appID, secretID string) (*CustomAppSecret, error) {
	var secret CustomAppSecret
	if err := s.DB.Where("id = ? AND app_id = ?", secretID, appID).First(&secret).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: secret", service.ErrNotFound)
		}
		return nil, fmt.Errorf("load app secret: %w", err)
	}
	return &secret, nil
}

// CreateSecret mints a new app secret. The plaintext is returned once.
func (s *Service) CreateSecret(appID string, input SecretInput) (*CustomAppSecret, error) {
	expiresAt, err := parseExpiry(input.ExpiresIn)
	if err != nil {
		return nil, err
	}
	secretType, err := parseSecretType(input.Type)
	if err != nil {
		return nil, err
	}
	value, err := generateSecret(64)
	if err != nil {
		return nil, fmt.Errorf("generate app secret: %w", err)
	}
	secret := &CustomAppSecret{
		ID:          newUUID(),
		Secret:      value,
		Description: input.Description,
		ExpiredAt:   expiresAt,
		IsOidc:      secretType,
		AppID:       appID,
	}
	if err := s.DB.Create(secret).Error; err != nil {
		return nil, fmt.Errorf("create app secret: %w", err)
	}
	return secret, nil
}

// RotateSecret replaces a secret's value in place and returns the new plaintext.
func (s *Service) RotateSecret(appID, secretID string, input SecretInput) (*CustomAppSecret, error) {
	secret, err := s.LoadSecret(appID, secretID)
	if err != nil {
		return nil, err
	}
	value, err := generateSecret(64)
	if err != nil {
		return nil, fmt.Errorf("generate app secret: %w", err)
	}
	secret.Secret = value
	if input.Description != "" {
		secret.Description = input.Description
	}
	if strings.TrimSpace(input.ExpiresIn) != "" {
		expiresAt, err := parseExpiry(input.ExpiresIn)
		if err != nil {
			return nil, err
		}
		secret.ExpiredAt = expiresAt
	}
	if strings.TrimSpace(input.Type) != "" {
		secretType, err := parseSecretType(input.Type)
		if err != nil {
			return nil, err
		}
		secret.IsOidc = secretType
	}
	if err := s.DB.Save(secret).Error; err != nil {
		return nil, fmt.Errorf("rotate app secret: %w", err)
	}
	return secret, nil
}

// DeleteSecret removes an app secret.
func (s *Service) DeleteSecret(appID, secretID string) error {
	secret, err := s.LoadSecret(appID, secretID)
	if err != nil {
		return err
	}
	if err := s.DB.Delete(secret).Error; err != nil {
		return fmt.Errorf("delete app secret: %w", err)
	}
	return nil
}

// ValidateSecret reports whether a plaintext secret belongs to the app and has
// not expired. Only API-key secrets are accepted; OIDC secrets are validated by
// the issuer.
func (s *Service) ValidateSecret(ctx context.Context, appID, secret string) (bool, error) {
	if strings.TrimSpace(secret) == "" {
		return false, nil
	}
	var count int64
	err := s.DB.Model(&CustomAppSecret{}).
		Where("app_id = ? AND is_oidc = ? AND secret = ? AND (expired_at IS NULL OR expired_at > ?)", appID, false, secret, time.Now().UTC()).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("validate app secret: %w", err)
	}
	return count > 0, nil
}

// ValidateSecretByID is the secret-id form of ValidateSecret.
func (s *Service) ValidateSecretByID(ctx context.Context, secretID, secret string) (bool, error) {
	if strings.TrimSpace(secret) == "" {
		return false, nil
	}
	var count int64
	err := s.DB.Model(&CustomAppSecret{}).
		Where("id = ? AND secret = ? AND (expired_at IS NULL OR expired_at > ?)", secretID, secret, time.Now().UTC()).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("validate app secret: %w", err)
	}
	return count > 0, nil
}

func parseSecretType(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "api_key", "apikey":
		return false, nil
	case "oidc":
		return true, nil
	default:
		return false, fmt.Errorf("%w: unknown secret type %q", service.ErrValidation, value)
	}
}

func parseExpiry(value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return nil, fmt.Errorf("%w: expires_in must be a Go duration such as 24h", service.ErrValidation)
	}
	if duration <= 0 {
		return nil, fmt.Errorf("%w: expires_in must be greater than zero", service.ErrValidation)
	}
	expiresAt := time.Now().UTC().Add(duration)
	return &expiresAt, nil
}

// --- Board widgets ---

// ListWidgets lists an app's widgets ordered by key.
func (s *Service) ListWidgets(appID string) ([]BoardWidget, error) {
	var widgets []BoardWidget
	if err := s.DB.Where("app_id = ?", appID).Order("key ASC").Find(&widgets).Error; err != nil {
		return nil, fmt.Errorf("list board widgets: %w", err)
	}
	if widgets == nil {
		widgets = []BoardWidget{}
	}
	return widgets, nil
}

// CreateWidget adds one board widget.
func (s *Service) CreateWidget(appID string, manifest BoardWidgetManifest) (*BoardWidget, error) {
	var count int64
	if err := s.DB.Model(&BoardWidget{}).Where("app_id = ? AND LOWER(key) = ?", appID, strings.ToLower(manifest.Key)).Count(&count).Error; err != nil {
		return nil, fmt.Errorf("check board widget: %w", err)
	}
	if count > 0 {
		return nil, fmt.Errorf("%w: a board widget with this key already exists", service.ErrConflict)
	}
	widget, err := widgetFromManifest(appID, manifest)
	if err != nil {
		return nil, err
	}
	if err := s.DB.Create(widget).Error; err != nil {
		if isUniqueConstraint(err) {
			return nil, fmt.Errorf("%w: a board widget with this key already exists", service.ErrConflict)
		}
		return nil, fmt.Errorf("create board widget: %w", err)
	}
	return widget, nil
}

// UpdateWidget replaces a widget's manifest fields.
func (s *Service) UpdateWidget(appID, key string, manifest BoardWidgetManifest) (*BoardWidget, error) {
	widget, err := s.loadWidget(appID, key)
	if err != nil {
		return nil, err
	}
	widget.Name = manifest.Name
	widget.Description = manifest.Description
	widget.IsEnabled = manifest.IsEnabled
	widget.RendererType = defaultString(manifest.RendererType, "data")
	widget.PayloadType = "object"
	widget.FieldTypes = manifest.FieldTypes
	widget.RequiredFields = manifest.RequiredFields
	widget.MaxPayloadBytes = manifest.MaxPayloadBytes
	widget.AllowMultiple = manifest.AllowMultiple
	if err := s.DB.Save(widget).Error; err != nil {
		return nil, fmt.Errorf("update board widget: %w", err)
	}
	return widget, nil
}

// DeleteWidget removes a board widget.
func (s *Service) DeleteWidget(appID, key string) error {
	widget, err := s.loadWidget(appID, key)
	if err != nil {
		return err
	}
	if err := s.DB.Delete(widget).Error; err != nil {
		return fmt.Errorf("delete board widget: %w", err)
	}
	return nil
}

// LoadWidget resolves one widget of an app.
func (s *Service) LoadWidget(appID, key string) (*BoardWidget, error) {
	return s.loadWidget(appID, key)
}

func (s *Service) loadWidget(appID, key string) (*BoardWidget, error) {
	var widget BoardWidget
	if err := s.DB.Where("app_id = ? AND LOWER(key) = ?", appID, strings.ToLower(strings.TrimSpace(key))).First(&widget).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: board widget", service.ErrNotFound)
		}
		return nil, fmt.Errorf("load board widget: %w", err)
	}
	return &widget, nil
}

// BoardApp is one app that contributes board widgets, with its publisher.
type BoardApp struct {
	ID            string           `json:"id"`
	Slug          string           `json:"slug"`
	Name          string           `json:"name"`
	Description   string           `json:"description,omitempty"`
	PublisherName string           `json:"publisher_name"`
	BoardWidgets  []BoardWidgetDTO `json:"board_widgets"`
}

// BoardWidgetDTO is one widget of a board-capable app, with its derived slug.
type BoardWidgetDTO struct {
	Key             string           `json:"key"`
	Name            string           `json:"name"`
	Description     string           `json:"description"`
	Slug            string           `json:"slug"`
	IsEnabled       bool             `json:"is_enabled"`
	RendererType    string           `json:"renderer_type"`
	FieldTypes      []BoardFieldType `json:"field_types"`
	RequiredFields  []string         `json:"required_fields"`
	MaxPayloadBytes *int             `json:"max_payload_bytes"`
	AllowMultiple   bool             `json:"allow_multiple"`
}

// BoardWidgetSlug builds the canonical "{publisher}.{app}.{key}" widget slug.
func BoardWidgetSlug(publisherName, appSlug, key string) string {
	return publisherName + "." + appSlug + "." + key
}

// DiscoverBoardApps lists production apps with enabled widgets and the board
// scope, optionally narrowed to one app slug.
func (s *Service) DiscoverBoardApps(ctx context.Context, limit, offset int, slug string) ([]BoardApp, error) {
	var candidates []CustomApp
	query := s.DB.Preload("Project").Where("status = ?", CustomAppProduction)
	if strings.TrimSpace(slug) != "" {
		query = query.Where("slug = ?", strings.TrimSpace(slug))
	}
	if err := query.Order("name ASC").Find(&candidates).Error; err != nil {
		return nil, fmt.Errorf("list board apps: %w", err)
	}
	out := make([]BoardApp, 0, len(candidates))
	skipped := 0
	for index := range candidates {
		app := &candidates[index]
		if !app.OauthConfig.HasScope("accounts.profile.board") {
			continue
		}
		widgets, err := s.ListWidgets(app.ID)
		if err != nil {
			return nil, err
		}
		enabled := make([]BoardWidget, 0, len(widgets))
		for _, widget := range widgets {
			if widget.IsEnabled {
				enabled = append(enabled, widget)
			}
		}
		if len(enabled) == 0 {
			continue
		}
		if skipped < offset {
			skipped++
			continue
		}
		if len(out) >= limit {
			break
		}
		publisherName := s.developerPublisherName(ctx, app.Project)
		row := BoardApp{ID: app.ID, Slug: app.Slug, Name: app.Name, Description: app.Description, PublisherName: publisherName}
		for _, widget := range enabled {
			manifest := widget.Manifest()
			row.BoardWidgets = append(row.BoardWidgets, BoardWidgetDTO{
				Key:             manifest.Key,
				Name:            manifest.Name,
				Description:     manifest.Description,
				Slug:            BoardWidgetSlug(publisherName, app.Slug, manifest.Key),
				IsEnabled:       manifest.IsEnabled,
				RendererType:    manifest.RendererType,
				FieldTypes:      manifest.FieldTypes,
				RequiredFields:  manifest.RequiredFields,
				MaxPayloadBytes: manifest.MaxPayloadBytes,
				AllowMultiple:   manifest.AllowMultiple,
			})
		}
		out = append(out, row)
	}
	return out, nil
}

// developerPublisherName resolves the publisher name behind a project.
func (s *Service) developerPublisherName(ctx context.Context, project *DevProject) string {
	if project == nil || s == nil || s.DB == nil {
		return ""
	}
	var developer Developer
	if err := s.DB.Where("id = ?", project.DeveloperID).First(&developer).Error; err != nil {
		return ""
	}
	publisher := s.PublisherView(ctx, developer.PublisherID)
	if publisher == nil {
		return ""
	}
	return publisher.Name
}

// widgetFromManifest validates a manifest and builds the stored row.
func widgetFromManifest(appID string, manifest BoardWidgetManifest) (*BoardWidget, error) {
	key := strings.TrimSpace(manifest.Key)
	if key == "" {
		return nil, fmt.Errorf("%w: board widget key is required", service.ErrValidation)
	}
	if len(key) > 128 {
		return nil, fmt.Errorf("%w: board widget key must be at most 128 characters", service.ErrValidation)
	}
	if strings.TrimSpace(manifest.Name) == "" {
		return nil, fmt.Errorf("%w: board widget name is required", service.ErrValidation)
	}
	return &BoardWidget{
		ID:              newUUID(),
		AppID:           appID,
		Key:             key,
		Name:            manifest.Name,
		Description:     manifest.Description,
		IsEnabled:       manifest.IsEnabled,
		RendererType:    defaultString(manifest.RendererType, "data"),
		PayloadType:     "object",
		FieldTypes:      manifest.FieldTypes,
		RequiredFields:  manifest.RequiredFields,
		MaxPayloadBytes: manifest.MaxPayloadBytes,
		AllowMultiple:   manifest.AllowMultiple,
	}, nil
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func validateAppLengths(slug, name, description string) error {
	if len(slug) > 1024 || len(name) > 1024 {
		return fmt.Errorf("%w: slug and name must be at most 1024 characters", service.ErrValidation)
	}
	if len(description) > 4096 {
		return fmt.Errorf("%w: description must be at most 4096 characters", service.ErrValidation)
	}
	return nil
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
