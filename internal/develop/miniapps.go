package develop

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"

	"gorm.io/gorm"

	"src.solsynth.dev/solsynth/distribution/internal/service"
)

// manifestIDPattern is the reverse-domain form a manifest id must take.
var manifestIDPattern = regexp.MustCompile(`^[A-Za-z0-9]+(?:[._-][A-Za-z0-9]+)+$`)

// MiniAppInput is the create/update payload for a mini app.
type MiniAppInput struct {
	Slug         string           `json:"slug"`
	Stage        string           `json:"stage"`
	Manifest     *MiniAppManifest `json:"manifest"`
	IconID       string           `json:"icon_id"`
	BackgroundID string           `json:"background_id"`
}

// ListMiniApps lists a project's mini apps with their developer hydrated.
func (s *Service) ListMiniApps(ctx context.Context, projectID string) ([]MiniApp, error) {
	var apps []MiniApp
	if err := s.DB.Preload("Project").Where("project_id = ?", projectID).Order("created_at ASC").Find(&apps).Error; err != nil {
		return nil, fmt.Errorf("list mini apps: %w", err)
	}
	if apps == nil {
		apps = []MiniApp{}
	}
	for index := range apps {
		if apps[index].Project != nil {
			apps[index].Project.Developer = s.developerFor(ctx, apps[index].Project.DeveloperID)
		}
	}
	return apps, nil
}

// LoadMiniApp resolves one mini app of a project.
func (s *Service) LoadMiniApp(projectID, miniAppID string) (*MiniApp, error) {
	var app MiniApp
	if err := s.DB.Preload("Project").Where("id = ? AND project_id = ?", miniAppID, projectID).First(&app).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: mini app", service.ErrNotFound)
		}
		return nil, fmt.Errorf("load mini app: %w", err)
	}
	return &app, nil
}

// CreateMiniApp registers a mini app under a project.
func (s *Service) CreateMiniApp(ctx context.Context, projectID string, input MiniAppInput) (*MiniApp, error) {
	slug := strings.TrimSpace(input.Slug)
	if slug == "" {
		return nil, fmt.Errorf("%w: slug is required", service.ErrValidation)
	}
	if !isSlug(slug) {
		return nil, fmt.Errorf("%w: slug can only contain letters, numbers, underscores, and hyphens", service.ErrValidation)
	}
	stage, ok := ParseMiniAppStage(input.Stage)
	if !ok {
		return nil, fmt.Errorf("%w: unknown mini app stage %q", service.ErrValidation, input.Stage)
	}
	var existing int64
	if err := s.DB.Model(&MiniApp{}).Where("LOWER(slug) = ?", strings.ToLower(slug)).Count(&existing).Error; err != nil {
		return nil, fmt.Errorf("check mini app slug: %w", err)
	}
	if existing > 0 {
		return nil, fmt.Errorf("%w: a mini app with this slug already exists", service.ErrConflict)
	}
	if input.Manifest != nil {
		if err := validateManifest(input.Manifest); err != nil {
			return nil, err
		}
		if err := s.assertPluginIDFree(input.Manifest.ID, ""); err != nil {
			return nil, err
		}
	}

	app := &MiniApp{ID: newUUID(), Slug: slug, Stage: stage, Manifest: input.Manifest, ProjectID: projectID}
	applyManifest(app, input.Manifest)
	var err error
	if app.Icon, err = s.ResolveFile(ctx, input.IconID, "icon"); err != nil {
		return nil, err
	}
	if app.Background, err = s.ResolveFile(ctx, input.BackgroundID, "background"); err != nil {
		return nil, err
	}
	if err := s.DB.Create(app).Error; err != nil {
		if isUniqueConstraint(err) {
			return nil, fmt.Errorf("%w: a mini app with this slug already exists", service.ErrConflict)
		}
		return nil, fmt.Errorf("create mini app: %w", err)
	}
	return app, nil
}

// UpdateMiniApp patches a mini app. Empty fields are left unchanged.
func (s *Service) UpdateMiniApp(ctx context.Context, app *MiniApp, input MiniAppInput) (*MiniApp, error) {
	if slug := strings.TrimSpace(input.Slug); slug != "" && slug != app.Slug {
		if !isSlug(slug) {
			return nil, fmt.Errorf("%w: slug can only contain letters, numbers, underscores, and hyphens", service.ErrValidation)
		}
		var existing int64
		if err := s.DB.Model(&MiniApp{}).
			Where("LOWER(slug) = ? AND id <> ?", strings.ToLower(slug), app.ID).
			Count(&existing).Error; err != nil {
			return nil, fmt.Errorf("check mini app slug: %w", err)
		}
		if existing > 0 {
			return nil, fmt.Errorf("%w: a mini app with this slug already exists", service.ErrConflict)
		}
		app.Slug = slug
	}
	if strings.TrimSpace(input.Stage) != "" {
		stage, ok := ParseMiniAppStage(input.Stage)
		if !ok {
			return nil, fmt.Errorf("%w: unknown mini app stage %q", service.ErrValidation, input.Stage)
		}
		app.Stage = stage
	}
	if input.Manifest != nil {
		if err := validateManifest(input.Manifest); err != nil {
			return nil, err
		}
		if err := s.assertPluginIDFree(input.Manifest.ID, app.ID); err != nil {
			return nil, err
		}
		app.Manifest = input.Manifest
		applyManifest(app, input.Manifest)
	}
	var err error
	if app.Icon, err = s.ResolveFile(ctx, input.IconID, "icon"); err != nil {
		return nil, err
	}
	if app.Background, err = s.ResolveFile(ctx, input.BackgroundID, "background"); err != nil {
		return nil, err
	}
	if err := s.DB.Save(app).Error; err != nil {
		if isUniqueConstraint(err) {
			return nil, fmt.Errorf("%w: a mini app with this slug already exists", service.ErrConflict)
		}
		return nil, fmt.Errorf("update mini app: %w", err)
	}
	return app, nil
}

// DeleteMiniApp removes a mini app and its stored package.
func (s *Service) DeleteMiniApp(ctx context.Context, app *MiniApp) error {
	if app.PackageStorageKey != nil && s.Packages != nil {
		if err := s.Packages.Delete(ctx, *app.PackageStorageKey); err != nil {
			return fmt.Errorf("%w: delete mini app package: %v", service.ErrDependency, err)
		}
	}
	if err := s.DB.Delete(app).Error; err != nil {
		return fmt.Errorf("delete mini app: %w", err)
	}
	return nil
}

// UploadPackage stores a mini app archive and applies its manifest. The archive
// must be a ZIP containing exactly one manifest.json.
func (s *Service) UploadPackage(ctx context.Context, app *MiniApp, fileName string, content []byte, maxBytes int64) (PackageUpload, error) {
	if s.Packages == nil {
		return PackageUpload{}, fmt.Errorf("%w: package storage is not configured", service.ErrDependency)
	}
	manifest, err := readPackageManifest(content, maxBytes)
	if err != nil {
		return PackageUpload{}, err
	}
	if err := validateManifest(manifest); err != nil {
		return PackageUpload{}, err
	}
	if err := s.assertPluginIDFree(manifest.ID, app.ID); err != nil {
		return PackageUpload{}, err
	}
	upload, err := s.Packages.Save(ctx, app.ID, fileName, content)
	if err != nil {
		return PackageUpload{}, fmt.Errorf("%w: store mini app package: %v", service.ErrDependency, err)
	}

	previous := app.PackageStorageKey
	app.Manifest = manifest
	applyManifest(app, manifest)
	app.PackageStorageKey = &upload.Key
	app.PackageSha256 = &upload.Sha256
	size := upload.Size
	app.PackageSize = &size
	if upload.URL != "" {
		app.PackageUrl = &upload.URL
	} else {
		app.PackageUrl = nil
	}
	if err := s.DB.Save(app).Error; err != nil {
		_ = s.Packages.Delete(ctx, upload.Key)
		return PackageUpload{}, fmt.Errorf("save mini app package: %w", err)
	}
	if previous != nil && *previous != upload.Key {
		_ = s.Packages.Delete(ctx, *previous)
	}
	return upload, nil
}

// OpenPackage streams a mini app's stored package.
func (s *Service) OpenPackage(ctx context.Context, app *MiniApp) ([]byte, error) {
	if app.PackageStorageKey == nil || *app.PackageStorageKey == "" {
		return nil, fmt.Errorf("%w: mini app has no published package", service.ErrNotFound)
	}
	if s.Packages == nil {
		return nil, fmt.Errorf("%w: package storage is not configured", service.ErrDependency)
	}
	content, err := s.Packages.Open(ctx, *app.PackageStorageKey)
	if err != nil {
		return nil, fmt.Errorf("%w: read mini app package: %v", service.ErrDependency, err)
	}
	return content, nil
}

// IncrementDownloads bumps a mini app's download counter.
func (s *Service) IncrementDownloads(appID string) error {
	if err := s.DB.Model(&MiniApp{}).Where("id = ?", appID).
		UpdateColumn("download_count", gorm.Expr("download_count + 1")).Error; err != nil {
		return fmt.Errorf("increment mini app downloads: %w", err)
	}
	return nil
}

// FindPublishedMiniApp resolves a production mini app by slug.
func (s *Service) FindPublishedMiniApp(slug string) (*MiniApp, error) {
	var app MiniApp
	if err := s.DB.Preload("Project").
		Where("stage = ? AND LOWER(slug) = ?", MiniAppProduction, strings.ToLower(strings.TrimSpace(slug))).
		First(&app).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: mini app", service.ErrNotFound)
		}
		return nil, fmt.Errorf("load mini app: %w", err)
	}
	return &app, nil
}

// DiscoverMiniApps lists production mini apps, newest name order first.
func (s *Service) DiscoverMiniApps(ctx context.Context, limit, offset int, search string) ([]MiniApp, int64, error) {
	query := s.DB.Model(&MiniApp{}).Where("stage = ?", MiniAppProduction)
	if search = strings.TrimSpace(search); search != "" {
		probe := "%" + strings.ToLower(search) + "%"
		query = query.Where(
			"LOWER(COALESCE(plugin_id, '')) LIKE ? OR LOWER(COALESCE(name, '')) LIKE ? OR LOWER(COALESCE(description, '')) LIKE ?",
			probe, probe, probe)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count mini apps: %w", err)
	}
	var apps []MiniApp
	if err := query.Preload("Project").Order("name ASC, slug ASC").Limit(limit).Offset(offset).Find(&apps).Error; err != nil {
		return nil, 0, fmt.Errorf("list mini apps: %w", err)
	}
	if apps == nil {
		apps = []MiniApp{}
	}
	for index := range apps {
		if apps[index].Project != nil {
			apps[index].Project.Developer = s.developerFor(ctx, apps[index].Project.DeveloperID)
		}
	}
	return apps, total, nil
}

// developerFor hydrates a developer with its publisher projection.
func (s *Service) developerFor(ctx context.Context, developerID string) *Developer {
	if strings.TrimSpace(developerID) == "" {
		return nil
	}
	developer, err := s.LoadDeveloperByID(ctx, developerID)
	if err != nil {
		return nil
	}
	return developer
}

// assertPluginIDFree rejects a manifest id already used by another mini app.
func (s *Service) assertPluginIDFree(pluginID, excludeID string) error {
	if strings.TrimSpace(pluginID) == "" {
		return nil
	}
	query := s.DB.Model(&MiniApp{}).Where("plugin_id = ?", pluginID)
	if excludeID != "" {
		query = query.Where("id <> ?", excludeID)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return fmt.Errorf("check manifest id: %w", err)
	}
	if count > 0 {
		return fmt.Errorf("%w: a plugin with this manifest id already exists", service.ErrConflict)
	}
	return nil
}

// applyManifest copies the manifest's metadata onto the mini app row.
func applyManifest(app *MiniApp, manifest *MiniAppManifest) {
	if manifest == nil {
		return
	}
	app.PluginID = &manifest.ID
	app.Name = &manifest.Name
	if manifest.Version != "" {
		app.Version = manifest.Version
	}
	app.Author = stringOrNil(manifest.Author)
	app.Description = stringOrNil(manifest.Description)
	app.EntryUrl = stringOrNil(manifest.EntryUrl)
	app.Homepage = stringOrNil(manifest.Homepage)
}

func stringOrNil(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}

// validateManifest checks the fields a manifest must carry.
func validateManifest(manifest *MiniAppManifest) error {
	if manifest == nil {
		return nil
	}
	if !manifestIDPattern.MatchString(strings.TrimSpace(manifest.ID)) {
		return fmt.Errorf("%w: manifest id must be a reverse-domain identifier", service.ErrValidation)
	}
	if strings.TrimSpace(manifest.Name) == "" {
		return fmt.Errorf("%w: manifest name is required", service.ErrValidation)
	}
	return nil
}

// readPackageManifest validates an archive and returns its manifest. Every
// entry path is checked so a crafted archive cannot escape the extraction root.
func readPackageManifest(content []byte, maxBytes int64) (*MiniAppManifest, error) {
	if len(content) == 0 {
		return nil, fmt.Errorf("%w: plugin package cannot be empty", service.ErrValidation)
	}
	if maxBytes > 0 && int64(len(content)) > maxBytes {
		return nil, fmt.Errorf("%w: plugin package is too large. Maximum allowed is %d bytes", service.ErrValidation, maxBytes)
	}
	reader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return nil, fmt.Errorf("%w: the uploaded file is not a valid ZIP package", service.ErrValidation)
	}
	if len(reader.File) == 0 {
		return nil, fmt.Errorf("%w: plugin package cannot be empty", service.ErrValidation)
	}
	var manifests []*zip.File
	for _, entry := range reader.File {
		normalized := strings.ReplaceAll(entry.Name, "\\", "/")
		if strings.HasPrefix(normalized, "/") || strings.Contains("/"+normalized+"/", "/../") {
			return nil, fmt.Errorf("%w: plugin package contains an unsafe path", service.ErrValidation)
		}
		if strings.EqualFold(path.Base(normalized), "manifest.json") {
			manifests = append(manifests, entry)
		}
	}
	switch len(manifests) {
	case 0:
		return nil, fmt.Errorf("%w: plugin package must contain manifest.json", service.ErrValidation)
	case 1:
	default:
		return nil, fmt.Errorf("%w: plugin package must contain only one manifest.json", service.ErrValidation)
	}
	file, err := manifests[0].Open()
	if err != nil {
		return nil, fmt.Errorf("%w: read manifest.json: %v", service.ErrValidation, err)
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 1<<20))
	decoder.DisallowUnknownFields()
	var manifest MiniAppManifest
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("%w: manifest.json is not valid JSON", service.ErrValidation)
	}
	return &manifest, nil
}

// isSlug reports whether a value is a safe slug.
func isSlug(value string) bool {
	if value == "" || len(value) > 1024 {
		return false
	}
	for _, char := range value {
		switch {
		case char >= 'a' && char <= 'z', char >= 'A' && char <= 'Z', char >= '0' && char <= '9', char == '_', char == '-':
		default:
			return false
		}
	}
	return true
}

// MiniAppView is the public mini app representation. It credits the owning
// publisher as the author, matching the identity service.
type MiniAppView struct {
	MiniApp
	DeveloperPublisher *PublisherView `json:"developer_publisher,omitempty"`
}

// PublicMiniApp builds the public view of a mini app. The author is overridden
// on the copy, never on the stored record.
func (s *Service) PublicMiniApp(ctx context.Context, app *MiniApp) MiniAppView {
	view := MiniAppView{}
	if app == nil {
		return view
	}
	view.MiniApp = *app
	developer := app.Project.Developer
	if developer == nil && app.Project != nil {
		developer = s.developerFor(ctx, app.Project.DeveloperID)
	}
	if developer != nil && developer.Publisher != nil {
		view.DeveloperPublisher = developer.Publisher
		author := developer.Publisher.Nick
		if strings.TrimSpace(author) == "" {
			author = developer.Publisher.Name
		}
		if author != "" {
			view.Author = &author
		}
	}
	return view
}

// MiniAppFileReference is a helper for package download responses.
type MiniAppFileReference struct {
	Name        string
	ContentType string
	Content     []byte
}

// DownloadPackage resolves a published mini app's package for HTTP delivery.
func (s *Service) DownloadPackage(ctx context.Context, slug string) (*MiniAppFileReference, string, error) {
	app, err := s.FindPublishedMiniApp(slug)
	if err != nil {
		return nil, "", err
	}
	if app.PackageStorageKey == nil || *app.PackageStorageKey == "" {
		return nil, "", fmt.Errorf("%w: mini app has no published package", service.ErrNotFound)
	}
	if err := s.IncrementDownloads(app.ID); err != nil {
		return nil, "", err
	}
	if app.PackageUrl != nil && *app.PackageUrl != "" {
		return nil, *app.PackageUrl, nil
	}
	content, err := s.OpenPackage(ctx, app)
	if err != nil {
		return nil, "", err
	}
	return &MiniAppFileReference{Name: app.Slug + ".zip", ContentType: "application/zip", Content: content}, "", nil
}
