package develop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"gorm.io/gorm"
	gen "src.solsynth.dev/sosys/go/proto"

	"src.solsynth.dev/solsynth/distribution/internal/database"
	"src.solsynth.dev/solsynth/distribution/internal/service"
)

// PublisherDirectory is the Sphere contract the portal needs: publisher
// identity, membership roles, and the membership listing used to enroll.
type PublisherDirectory interface {
	GetPublisher(ctx context.Context, ref string) (*gen.DyPublisher, error)
	GetPublisherBatch(ctx context.Context, ids []string) ([]*gen.DyPublisher, error)
	IsPublisherMember(ctx context.Context, publisherID, accountID string, role gen.DyPublisherMemberRole) (bool, error)
	ListPublishers(ctx context.Context, accountID string) ([]*gen.DyPublisher, error)
}

// FileDirectory resolves cloud-file references for app pictures and packages.
type FileDirectory interface {
	GetFile(ctx context.Context, id string) (*gen.DyCloudFile, error)
}

// AccountDirectory reads accounts for quota tiers and bot transparency.
type AccountDirectory interface {
	GetAccount(ctx context.Context, id string) (*gen.DyAccount, error)
	GetBotAccountBatch(ctx context.Context, automatedIDs []string) ([]*gen.DyAccount, error)
}

// RealmDirectory resolves realm ownership for organisation publishers.
type RealmDirectory interface {
	GetRealmBatch(ctx context.Context, ids []string) ([]*gen.DyRealm, error)
}

// SubscriptionCatalog registers recurring app products with Wallet and opens
// ad-hoc orders on a developer's behalf.
type SubscriptionCatalog interface {
	RegisterAppSubscriptionDefinition(ctx context.Context, req *gen.DyRegisterAppSubscriptionDefinitionRequest) error
	CreateOrder(ctx context.Context, req *gen.DyCreateOrderRequest) (*gen.DyOrder, error)
}

// ActionLogger records developer-program audit entries in Stargate.
type ActionLogger interface {
	CreateActionLog(ctx context.Context, req *gen.DyCreateActionLogRequest) error
}

// Service is the shared developer-portal domain service. Module packages
// construct one per process and mount their own routes on top of it.
type Service struct {
	DB            *gorm.DB
	Publishers    PublisherDirectory
	Files         FileDirectory
	Accounts      AccountDirectory
	Realms        RealmDirectory
	Bots          BotDirectory
	Subscriptions SubscriptionCatalog
	Actions       ActionLogger
	// Profiles writes board payloads and answers board consent. Nil disables
	// the board-payload push surface.
	Profiles ProfileDirectory

	// Permissions answers global permission-node checks. A nil checker treats
	// every check as allowed, matching a deployment without Stargate.
	Permissions PermissionChecker
	// Packages stores uploaded mini app archives.
	Packages PackageStore
}

// PermissionChecker is the narrow Stargate permission-node contract.
type PermissionChecker interface {
	HasPermission(ctx context.Context, accountID, key string) (bool, error)
}

// PackageStore persists mini app archives and hands back their public URL.
type PackageStore interface {
	// Save stores a package and returns its storage key, public URL (empty
	// when the bucket is not publicly readable), size and SHA-256.
	Save(ctx context.Context, miniAppID, fileName string, content []byte) (PackageUpload, error)
	// Open streams a stored package.
	Open(ctx context.Context, key string) ([]byte, error)
	// Delete removes a stored package.
	Delete(ctx context.Context, key string) error
}

// PackageUpload is the result of storing a mini app archive.
type PackageUpload struct {
	Key      string
	URL      string
	Size     int64
	Sha256   string
	FileName string
}

// New builds the domain service.
func New(db *gorm.DB, publishers PublisherDirectory) *Service {
	return &Service{DB: db, Publishers: publishers}
}

// dependencyError converts a transport failure into the service error the HTTP
// layer maps to 503.
func dependencyError(err error) error {
	if err == nil {
		return service.ErrDependency
	}
	return fmt.Errorf("%w: %v", service.ErrDependency, err)
}

// rpcErrorToService maps gRPC status codes onto the service sentinels so a
// missing remote record reads as 404 instead of 503.
func rpcErrorToService(err error, notFound string) error {
	if err == nil {
		return nil
	}
	switch status.Code(err) {
	case codes.NotFound:
		return fmt.Errorf("%w: %s", service.ErrNotFound, notFound)
	case codes.InvalidArgument, codes.FailedPrecondition:
		return fmt.Errorf("%w: %v", service.ErrValidation, err)
	case codes.PermissionDenied:
		return service.ErrForbidden
	case codes.Unauthenticated:
		return service.ErrUnauthorized
	default:
		return dependencyError(err)
	}
}

// RequirePermission enforces one global permission node.
func (s *Service) RequirePermission(ctx context.Context, accountID, key string) error {
	accountID = strings.TrimSpace(accountID)
	key = strings.TrimSpace(key)
	if accountID == "" || key == "" {
		return fmt.Errorf("%w: account id and permission key are required", service.ErrForbidden)
	}
	if s == nil || s.Permissions == nil {
		return nil
	}
	allowed, err := s.Permissions.HasPermission(ctx, accountID, key)
	if err != nil {
		return fmt.Errorf("%w: check permission %q: %v", service.ErrDependency, key, err)
	}
	if !allowed {
		return fmt.Errorf("%w: %s is required", service.ErrForbidden, key)
	}
	return nil
}

// PublisherView resolves a publisher into the wire projection. A missing
// publisher yields nil rather than an error so listings degrade gracefully.
func (s *Service) PublisherView(ctx context.Context, publisherID string) *PublisherView {
	publisher, err := s.GetPublisher(ctx, publisherID)
	if err != nil || publisher == nil {
		return nil
	}
	return PublisherViewFromProto(publisher)
}

// GetPublisher resolves a publisher by ID or name.
func (s *Service) GetPublisher(ctx context.Context, ref string) (*gen.DyPublisher, error) {
	if s == nil || s.Publishers == nil {
		return nil, fmt.Errorf("%w: publisher directory unavailable", service.ErrDependency)
	}
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, fmt.Errorf("%w: publisher is required", service.ErrValidation)
	}
	publisher, err := s.Publishers.GetPublisher(ctx, ref)
	if err != nil {
		return nil, rpcErrorToService(err, "publisher")
	}
	if publisher == nil || publisher.GetId() == "" {
		return nil, fmt.Errorf("%w: publisher", service.ErrNotFound)
	}
	return publisher, nil
}

// PublisherViewFromProto projects a publisher onto the wire shape.
func PublisherViewFromProto(publisher *gen.DyPublisher) *PublisherView {
	if publisher == nil {
		return nil
	}
	view := &PublisherView{ID: publisher.GetId(), Name: publisher.GetName(), Nick: publisher.GetNick(), Bio: publisher.GetBio().GetValue()}
	if picture := publisher.GetPicture(); picture != nil && picture.GetId() != "" {
		view.Picture = CloudFileFromProto(picture)
	}
	return view
}

// CloudFileFromProto projects a file onto the cached reference shape.
func CloudFileFromProto(file *gen.DyCloudFile) *database.CloudFileReference {
	if file == nil || file.GetId() == "" {
		return nil
	}
	ref := &database.CloudFileReference{
		Id:             file.GetId(),
		Name:           file.GetName(),
		FileMeta:       decodeMeta(file.GetFileMeta()),
		UserMeta:       decodeMeta(file.GetUserMeta()),
		MimeType:       file.GetMimeType(),
		Hash:           file.GetHash(),
		Size:           file.GetSize(),
		HasCompression: file.GetHasCompression(),
		Url:            file.GetUrl(),
	}
	if file.Object != nil {
		ref.FileMeta = decodeMeta(file.Object.GetMeta())
	}
	if file.Width != nil {
		value := file.GetWidth()
		ref.Width = &value
	}
	if file.Height != nil {
		value := file.GetHeight()
		ref.Height = &value
	}
	if file.Blurhash != nil {
		ref.Blurhash = file.GetBlurhash()
	}
	if file.Usage != nil {
		ref.Usage = file.GetUsage()
	}
	if file.ApplicationType != nil {
		ref.ApplicationType = file.GetApplicationType()
	}
	return ref
}

func decodeMeta(encoded []byte) map[string]any {
	if len(encoded) == 0 {
		return map[string]any{}
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil || decoded == nil {
		return map[string]any{}
	}
	return decoded
}

// ResolveFile fetches a cloud file and returns its cached reference. An
// unknown id is a validation error, matching the identity service.
func (s *Service) ResolveFile(ctx context.Context, fileID, field string) (*database.CloudFileReference, error) {
	fileID = strings.TrimSpace(fileID)
	if fileID == "" {
		return nil, nil
	}
	if s == nil || s.Files == nil {
		return nil, fmt.Errorf("%w: file directory unavailable", service.ErrDependency)
	}
	file, err := s.Files.GetFile(ctx, fileID)
	if err != nil {
		return nil, rpcErrorToService(err, "file")
	}
	if file == nil || file.GetId() == "" {
		return nil, fmt.Errorf("%w: invalid %s file id, unable to find the file on cloud", service.ErrValidation, field)
	}
	return CloudFileFromProto(file), nil
}

// LoadDeveloper resolves an enrolled developer by publisher name.
func (s *Service) LoadDeveloper(ctx context.Context, publisherName string) (*Developer, error) {
	publisher, err := s.GetPublisher(ctx, publisherName)
	if err != nil {
		return nil, err
	}
	var developer Developer
	if err := s.DB.Where("publisher_id = ?", publisher.GetId()).First(&developer).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: developer", service.ErrNotFound)
		}
		return nil, fmt.Errorf("load developer: %w", err)
	}
	developer.Publisher = PublisherViewFromProto(publisher)
	return &developer, nil
}

// LoadDeveloperByID resolves an enrolled developer by its record id.
func (s *Service) LoadDeveloperByID(ctx context.Context, id string) (*Developer, error) {
	var developer Developer
	if err := s.DB.Where("id = ?", strings.TrimSpace(id)).First(&developer).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: developer", service.ErrNotFound)
		}
		return nil, fmt.Errorf("load developer: %w", err)
	}
	developer.Publisher = s.PublisherView(ctx, developer.PublisherID)
	return &developer, nil
}

// HydratePublishers fills the publisher projection for a batch of developers
// with one Sphere call.
func (s *Service) HydratePublishers(ctx context.Context, developers []Developer) error {
	if len(developers) == 0 || s == nil || s.Publishers == nil {
		return nil
	}
	ids := make([]string, 0, len(developers))
	seen := make(map[string]bool, len(developers))
	for _, developer := range developers {
		if developer.PublisherID == "" || seen[developer.PublisherID] {
			continue
		}
		seen[developer.PublisherID] = true
		ids = append(ids, developer.PublisherID)
	}
	if len(ids) == 0 {
		return nil
	}
	publishers, err := s.Publishers.GetPublisherBatch(ctx, ids)
	if err != nil {
		return rpcErrorToService(err, "publisher")
	}
	byID := make(map[string]*PublisherView, len(publishers))
	for _, publisher := range publishers {
		byID[publisher.GetId()] = PublisherViewFromProto(publisher)
	}
	for index := range developers {
		developers[index].Publisher = byID[developers[index].PublisherID]
	}
	return nil
}

// RequireMember enforces a Sphere publisher membership role for the account.
func (s *Service) RequireMember(ctx context.Context, publisherID, accountID string, role gen.DyPublisherMemberRole) error {
	if s == nil || s.Publishers == nil {
		return fmt.Errorf("%w: publisher directory unavailable", service.ErrDependency)
	}
	if strings.TrimSpace(accountID) == "" {
		return service.ErrUnauthorized
	}
	valid, err := s.Publishers.IsPublisherMember(ctx, publisherID, accountID, role)
	if err != nil {
		return rpcErrorToService(err, "publisher membership")
	}
	if !valid {
		return fmt.Errorf("%w: publisher membership role %s is required", service.ErrForbidden, roleName(role))
	}
	return nil
}

// roleName is the lowercase word for a membership role.
func roleName(role gen.DyPublisherMemberRole) string {
	switch role {
	case gen.DyPublisherMemberRole_DY_OWNER:
		return "owner"
	case gen.DyPublisherMemberRole_DY_MANAGER:
		return "manager"
	case gen.DyPublisherMemberRole_DY_EDITOR:
		return "editor"
	case gen.DyPublisherMemberRole_DY_VIEWER:
		return "viewer"
	default:
		return "member"
	}
}

// LoadProject resolves a project and asserts it belongs to the developer.
func (s *Service) LoadProject(developerID, projectID string) (*DevProject, error) {
	if s == nil || s.DB == nil {
		return nil, fmt.Errorf("%w: database unavailable", service.ErrDependency)
	}
	var project DevProject
	query := s.DB.Where("id = ?", strings.TrimSpace(projectID))
	if strings.TrimSpace(developerID) != "" {
		query = query.Where("developer_id = ?", developerID)
	}
	if err := query.First(&project).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: project", service.ErrNotFound)
		}
		return nil, fmt.Errorf("load project: %w", err)
	}
	return &project, nil
}

// LoadApp resolves a custom app and asserts it belongs to the project.
func (s *Service) LoadApp(projectID, appID string) (*CustomApp, error) {
	if s == nil || s.DB == nil {
		return nil, fmt.Errorf("%w: database unavailable", service.ErrDependency)
	}
	var app CustomApp
	query := s.DB.Where("id = ?", strings.TrimSpace(appID))
	if strings.TrimSpace(projectID) != "" {
		query = query.Where("project_id = ?", projectID)
	}
	if err := query.First(&app).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: app", service.ErrNotFound)
		}
		return nil, fmt.Errorf("load app: %w", err)
	}
	return &app, nil
}

// DeveloperScope is one authorized (developer, project) pair a request may act
// on. Console routes resolve it once and then use the narrow helpers.
type DeveloperScope struct {
	Developer *Developer
	Project   *DevProject
}

// ResolveScope authenticates one account against a developer name, the
// required publisher role, and an optional project.
func (s *Service) ResolveScope(ctx context.Context, accountID, developerName string, role gen.DyPublisherMemberRole, projectID string) (*DeveloperScope, error) {
	developer, err := s.LoadDeveloper(ctx, developerName)
	if err != nil {
		return nil, err
	}
	if err := s.RequireMember(ctx, developer.PublisherID, accountID, role); err != nil {
		return nil, err
	}
	scope := &DeveloperScope{Developer: developer}
	if strings.TrimSpace(projectID) != "" {
		project, err := s.LoadProject(developer.ID, projectID)
		if err != nil {
			return nil, err
		}
		scope.Project = project
	}
	return scope, nil
}

// Audit records a developer-program action. A nil logger is a no-op.
func (s *Service) Audit(ctx context.Context, accountID, action string, meta map[string]any, userAgent, ipAddress string) {
	if s == nil || s.Actions == nil || strings.TrimSpace(accountID) == "" {
		return
	}
	request := &gen.DyCreateActionLogRequest{AccountId: accountID, Action: action}
	if len(meta) > 0 {
		request.Meta = make(map[string]*structpb.Value, len(meta))
		for key, value := range meta {
			if encoded, err := structpb.NewValue(value); err == nil {
				request.Meta[key] = encoded
			}
		}
	}
	if userAgent = strings.TrimSpace(userAgent); userAgent != "" {
		request.UserAgent = &wrapperspb.StringValue{Value: userAgent}
	}
	if ipAddress = strings.TrimSpace(ipAddress); ipAddress != "" {
		request.IpAddress = &wrapperspb.StringValue{Value: ipAddress}
	}
	_ = s.Actions.CreateActionLog(ctx, request)
}

func equalFold(left, right string) bool {
	return strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right))
}
