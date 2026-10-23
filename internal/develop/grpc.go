package develop

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
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

// CustomAppGRPC serves DyCustomAppService: the contract Passport (board
// widgets) and Wallet (app products, developer attribution) already call.
type CustomAppGRPC struct {
	gen.UnimplementedDyCustomAppServiceServer

	Service *Service
}

// GetCustomApp resolves an app by id or slug.
func (g *CustomAppGRPC) GetCustomApp(ctx context.Context, request *gen.DyGetCustomAppRequest) (*gen.DyGetCustomAppResponse, error) {
	if g == nil || g.Service == nil {
		return nil, status.Error(codes.Unavailable, "developer service is unavailable")
	}
	var app CustomApp
	switch query := request.GetQuery().(type) {
	case *gen.DyGetCustomAppRequest_Id:
		if !validUUID(query.Id) {
			return nil, status.Error(codes.InvalidArgument, "invalid id")
		}
		if err := g.Service.DB.Preload("Project").Where("id = ?", query.Id).First(&app).Error; err != nil {
			return nil, loadAppError(err)
		}
	case *gen.DyGetCustomAppRequest_Slug:
		if strings.TrimSpace(query.Slug) == "" {
			return nil, status.Error(codes.InvalidArgument, "id or slug required")
		}
		if err := g.Service.DB.Preload("Project").Where("slug = ?", query.Slug).First(&app).Error; err != nil {
			return nil, loadAppError(err)
		}
	default:
		return nil, status.Error(codes.InvalidArgument, "id or slug required")
	}
	return &gen.DyGetCustomAppResponse{App: g.Service.AppToProto(&app)}, nil
}

// CheckCustomAppSecret validates an app secret by app id or secret id.
func (g *CustomAppGRPC) CheckCustomAppSecret(ctx context.Context, request *gen.DyCheckCustomAppSecretRequest) (*gen.DyCheckCustomAppSecretResponse, error) {
	if g == nil || g.Service == nil {
		return nil, status.Error(codes.Unavailable, "developer service is unavailable")
	}
	if strings.TrimSpace(request.GetSecret()) == "" {
		return nil, status.Error(codes.InvalidArgument, "secret required")
	}
	var valid bool
	var err error
	switch identifier := request.GetSecretIdentifier().(type) {
	case *gen.DyCheckCustomAppSecretRequest_SecretId:
		if !validUUID(identifier.SecretId) {
			return nil, status.Error(codes.InvalidArgument, "invalid secret_id")
		}
		valid, err = g.Service.ValidateSecretByID(ctx, identifier.SecretId, request.GetSecret())
	case *gen.DyCheckCustomAppSecretRequest_AppId:
		if !validUUID(identifier.AppId) {
			return nil, status.Error(codes.InvalidArgument, "invalid app_id")
		}
		valid, err = g.Service.ValidateSecret(ctx, identifier.AppId, request.GetSecret())
	default:
		return nil, status.Error(codes.InvalidArgument, "secret_id or app_id required")
	}
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	// An explicit is_oidc flag narrows the accepted secret kind.
	if request.IsOidc != nil && valid {
		valid, err = g.Service.secretMatchesKind(request, *request.IsOidc)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
	}
	return &gen.DyCheckCustomAppSecretResponse{Valid: valid}, nil
}

// secretMatchesKind reports whether the matched secret is of the requested kind.
func (s *Service) secretMatchesKind(request *gen.DyCheckCustomAppSecretRequest, isOidc bool) (bool, error) {
	var count int64
	query := s.DB.Model(&CustomAppSecret{}).Where("secret = ? AND is_oidc = ?", request.GetSecret(), isOidc)
	switch identifier := request.GetSecretIdentifier().(type) {
	case *gen.DyCheckCustomAppSecretRequest_SecretId:
		query = query.Where("id = ?", identifier.SecretId)
	case *gen.DyCheckCustomAppSecretRequest_AppId:
		query = query.Where("app_id = ?", identifier.AppId)
	default:
		return false, nil
	}
	if err := query.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// GetAppProduct resolves a product by its "<publisher>.<app>.<sku>" identifier.
func (g *CustomAppGRPC) GetAppProduct(ctx context.Context, request *gen.DyGetAppProductRequest) (*gen.DyGetAppProductResponse, error) {
	if g == nil || g.Service == nil {
		return nil, status.Error(codes.Unavailable, "developer service is unavailable")
	}
	identifier := strings.TrimSpace(request.GetIdentifier())
	if identifier == "" {
		return nil, status.Error(codes.InvalidArgument, "identifier required")
	}
	// The SKU may contain dots, so only the first two segments are structural.
	parts := strings.Split(identifier, ".")
	if len(parts) < 3 {
		return nil, status.Error(codes.InvalidArgument, "invalid product identifier format")
	}
	publisherName, appSlug, sku := parts[0], parts[1], strings.Join(parts[2:], ".")

	app, err := g.Service.appForPublisher(ctx, publisherName, appSlug)
	if err != nil {
		return nil, err
	}
	var product AppProduct
	if err := g.Service.DB.Preload("State").
		Where("app_id = ? AND identifier = ?", app.ID, sku).
		First(&product).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, status.Error(codes.NotFound, "product not found")
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &gen.DyGetAppProductResponse{Product: productToProto(&product)}, nil
}

// GetAppDeveloper resolves the app and the developer that published it.
func (g *CustomAppGRPC) GetAppDeveloper(ctx context.Context, request *gen.DyGetAppDeveloperRequest) (*gen.DyGetAppDeveloperResponse, error) {
	if g == nil || g.Service == nil {
		return nil, status.Error(codes.Unavailable, "developer service is unavailable")
	}
	appID := strings.TrimSpace(request.GetAppId())
	if appID == "" {
		return nil, status.Error(codes.InvalidArgument, "app_id required")
	}
	if !validUUID(appID) {
		return nil, status.Error(codes.InvalidArgument, "invalid app_id")
	}
	var app CustomApp
	if err := g.Service.DB.Preload("Project").Where("id = ?", appID).First(&app).Error; err != nil {
		return nil, loadAppError(err)
	}
	if app.Project == nil {
		return nil, status.Error(codes.NotFound, "app project not found")
	}
	developer := &gen.DyDeveloper{Id: app.Project.DeveloperID}
	if enrolled, err := g.Service.LoadDeveloperByID(ctx, app.Project.DeveloperID); err == nil {
		developer.PublisherId = enrolled.PublisherID
		if enrolled.Publisher != nil {
			developer.PublisherName = enrolled.Publisher.Name
		}
	}
	return &gen.DyGetAppDeveloperResponse{App: g.Service.AppToProto(&app), Developer: developer}, nil
}

// GetBoardWidget resolves one widget of an app for board rendering.
func (g *CustomAppGRPC) GetBoardWidget(ctx context.Context, request *gen.DyGetBoardWidgetRequest) (*gen.DyGetBoardWidgetResponse, error) {
	if g == nil || g.Service == nil {
		return nil, status.Error(codes.Unavailable, "developer service is unavailable")
	}
	appID := strings.TrimSpace(request.GetAppId())
	if !validUUID(appID) {
		return nil, status.Error(codes.InvalidArgument, "invalid app_id")
	}
	if strings.TrimSpace(request.GetWidgetKey()) == "" {
		return nil, status.Error(codes.InvalidArgument, "widget_key required")
	}
	app, err := g.Service.LoadApp("", appID)
	if err != nil {
		return nil, serviceToStatus(err)
	}
	if !app.OauthConfig.HasScope("accounts.profile.board") {
		return nil, status.Error(codes.FailedPrecondition, "custom app must declare the accounts.profile.board scope to provide board widgets")
	}
	widget, err := g.Service.LoadWidget(appID, request.GetWidgetKey())
	if err != nil {
		return nil, serviceToStatus(err)
	}
	return &gen.DyGetBoardWidgetResponse{App: g.Service.AppToProto(app), Widget: widgetManifestToProto(widget.Manifest())}, nil
}

// ValidateBoardWidgetPayload validates a payload against a widget manifest.
func (g *CustomAppGRPC) ValidateBoardWidgetPayload(ctx context.Context, request *gen.DyValidateBoardWidgetPayloadRequest) (*gen.DyValidateBoardWidgetPayloadResponse, error) {
	if g == nil || g.Service == nil {
		return nil, status.Error(codes.Unavailable, "developer service is unavailable")
	}
	appID := strings.TrimSpace(request.GetAppId())
	if !validUUID(appID) {
		return nil, status.Error(codes.InvalidArgument, "invalid app_id")
	}
	if strings.TrimSpace(request.GetWidgetKey()) == "" {
		return nil, status.Error(codes.InvalidArgument, "widget_key required")
	}
	app, err := g.Service.LoadApp("", appID)
	if err != nil {
		return nil, serviceToStatus(err)
	}
	payload := request.GetPayload().AsMap()
	result := g.Service.ValidateBoardPayload(app, request.GetWidgetKey(), payload)
	normalized, err := structpb.NewValue(result.NormalizedPayload)
	if err != nil {
		return nil, status.Error(codes.Internal, "encode normalized payload")
	}
	response := &gen.DyValidateBoardWidgetPayloadResponse{
		Valid:             result.Valid,
		Message:           result.Message,
		Widget:            widgetManifestToProto(result.Widget),
		NormalizedPayload: normalized.GetStructValue(),
	}
	return response, nil
}

// appForPublisher resolves an app by publisher name and slug.
func (s *Service) appForPublisher(ctx context.Context, publisherName, slug string) (*CustomApp, error) {
	publisher, err := s.GetPublisher(ctx, publisherName)
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "publisher not found")
		}
		return nil, serviceToStatus(err)
	}
	var app CustomApp
	err = s.DB.Preload("Project").
		Where("slug = ?", slug).
		Where("project_id IN (?)", s.DB.Model(&DevProject{}).Select("id").Where("developer_id IN (?)",
			s.DB.Model(&Developer{}).Select("id").Where("publisher_id = ?", publisher.GetId()))).
		First(&app).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, status.Error(codes.NotFound, "app not found for this publisher")
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &app, nil
}

// AppToProto projects a stored app onto the gRPC contract.
func (s *Service) AppToProto(app *CustomApp) *gen.DyCustomApp {
	if app == nil {
		return nil
	}
	proto := &gen.DyCustomApp{
		Id:          app.ID,
		Slug:        app.Slug,
		Name:        app.Name,
		Description: app.Description,
		Status:      appStatusToProto(app.Status),
		ProjectId:   app.ProjectID,
	}
	if app.Picture != nil {
		proto.Picture = cloudFileToProto(app.Picture)
	}
	if app.Background != nil {
		proto.Background = cloudFileToProto(app.Background)
	}
	if app.Verification != nil {
		proto.Verification = &gen.DyVerificationMark{
			Type:        verificationTypeToProto(app.Verification.Type),
			Title:       app.Verification.Title,
			Description: app.Verification.Description,
			VerifiedBy:  app.Verification.VerifiedBy,
		}
	}
	if app.Links != nil {
		proto.Links = &gen.DyCustomAppLinks{
			HomePage:       app.Links.HomePage,
			PrivacyPolicy:  app.Links.PrivacyPolicy,
			TermsOfService: app.Links.TermsOfService,
		}
	}
	if app.OauthConfig != nil {
		proto.OauthConfig = &gen.DyCustomAppOauthConfig{
			ClientUri:              app.OauthConfig.ClientUri,
			RedirectUris:           app.OauthConfig.RedirectUris,
			PostLogoutRedirectUris: app.OauthConfig.PostLogoutRedirectUris,
			AllowedScopes:          app.OauthConfig.AllowedScopes,
			AllowedGrantTypes:      app.OauthConfig.AllowedGrantTypes,
			RequirePkce:            app.OauthConfig.RequirePkce,
			AllowOfflineAccess:     app.OauthConfig.AllowOfflineAccess,
			IsPublicClient:         app.OauthConfig.IsPublicClient,
		}
	}
	if app.PaymentWalletID != nil {
		proto.PaymentWalletId = *app.PaymentWalletID
	}
	if widgets, err := s.ListWidgets(app.ID); err == nil {
		for _, widget := range widgets {
			proto.BoardWidgets = append(proto.BoardWidgets, widgetManifestToProto(widget.Manifest()))
		}
	}
	return proto
}

func appStatusToProto(status CustomAppStatus) gen.DyCustomAppStatus {
	switch status {
	case CustomAppStaging:
		return gen.DyCustomAppStatus_DY_STAGING
	case CustomAppProduction:
		return gen.DyCustomAppStatus_DY_PRODUCTION
	case CustomAppSuspended:
		return gen.DyCustomAppStatus_DY_SUSPENDED
	default:
		return gen.DyCustomAppStatus_DY_DEVELOPING
	}
}

func verificationTypeToProto(value string) gen.DyVerificationMarkType {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "official":
		return gen.DyVerificationMarkType_DY_OFFICIAL
	case "individual":
		return gen.DyVerificationMarkType_DY_INDIVIDUAL
	case "organization":
		return gen.DyVerificationMarkType_DY_ORGANIZATION
	case "government":
		return gen.DyVerificationMarkType_DY_GOVERNMENT
	case "creator":
		return gen.DyVerificationMarkType_DY_CREATOR
	case "developer":
		return gen.DyVerificationMarkType_DY_DEVELOPER
	case "parody":
		return gen.DyVerificationMarkType_DY_PARODY
	default:
		return gen.DyVerificationMarkType_DY_VERIFICATION_MARK_TYPE_UNSPECIFIED
	}
}

// cloudFileToProto projects a cached file reference onto the gRPC contract.
func cloudFileToProto(file *database.CloudFileReference) *gen.DyCloudFile {
	if file == nil {
		return nil
	}
	proto := &gen.DyCloudFile{
		Id:             file.Id,
		Name:           file.Name,
		MimeType:       file.MimeType,
		Hash:           file.Hash,
		Size:           file.Size,
		HasCompression: file.HasCompression,
		Url:            file.Url,
	}
	if encoded, err := json.Marshal(file.FileMeta); err == nil {
		proto.FileMeta = encoded
	}
	if encoded, err := json.Marshal(file.UserMeta); err == nil {
		proto.UserMeta = encoded
	}
	if file.Width != nil {
		proto.Width = file.Width
	}
	if file.Height != nil {
		proto.Height = file.Height
	}
	if file.Blurhash != "" {
		proto.Blurhash = &file.Blurhash
	}
	if file.Usage != "" {
		proto.Usage = &file.Usage
	}
	if file.ApplicationType != "" {
		proto.ApplicationType = &file.ApplicationType
	}
	return proto
}

func widgetManifestToProto(manifest BoardWidgetManifest) *gen.DyBoardWidgetManifest {
	proto := &gen.DyBoardWidgetManifest{
		Key:           manifest.Key,
		Name:          manifest.Name,
		Description:   manifest.Description,
		IsEnabled:     manifest.IsEnabled,
		RendererType:  manifest.RendererType,
		PayloadType:   "object",
		AllowMultiple: manifest.AllowMultiple,
		FieldTypes:    map[string]string{},
	}
	for _, field := range manifest.FieldTypes {
		proto.FieldTypes[field.Name] = field.Type
	}
	proto.RequiredFields = append(proto.RequiredFields, manifest.RequiredFields...)
	if manifest.MaxPayloadBytes != nil {
		proto.MaxPayloadBytes = wrapperspb.Int32(int32(*manifest.MaxPayloadBytes))
	}
	return proto
}

// productToProto projects an app product onto the gRPC contract.
func productToProto(product *AppProduct) *gen.DyAppProduct {
	if product == nil {
		return nil
	}
	proto := &gen.DyAppProduct{
		Id:              product.ID,
		Identifier:      product.Identifier,
		DisplayName:     product.DisplayName,
		Description:     product.Description,
		Currency:        product.Currency,
		Price:           strconv.FormatFloat(product.Price, 'f', -1, 64),
		AppId:           product.AppID,
		Recurrence:      product.Recurrence.Name(),
		GroupIdentifier: deref(product.GroupIdentifier),
	}
	if product.Picture != nil {
		proto.Picture = cloudFileToProto(product.Picture)
	}
	if product.Background != nil {
		proto.Background = cloudFileToProto(product.Background)
	}
	if product.Fulfillment != nil {
		if encoded, err := json.Marshal(product.Fulfillment); err == nil {
			proto.FulfillmentJson = string(encoded)
		}
	}
	if product.State != nil {
		if encoded, err := json.Marshal(newProductStateDTO(product.State)); err == nil {
			proto.StateJson = string(encoded)
		}
	}
	return proto
}

// productStateDTO is the JSON shape Wallet reads from state_json.
type productStateDTO struct {
	IsEnabled             bool   `json:"is_enabled"`
	StockMode             int    `json:"stock_mode"`
	StockQuantity         *int   `json:"stock_quantity"`
	LastRestockedAtUnix   *int64 `json:"last_restocked_at_unix_seconds"`
	LastRestockedQuantity *int   `json:"last_restocked_quantity"`
}

func newProductStateDTO(state *AppProductState) productStateDTO {
	dto := productStateDTO{
		IsEnabled:             state.IsEnabled,
		StockMode:             int(state.StockMode),
		StockQuantity:         state.StockQuantity,
		LastRestockedQuantity: state.LastRestockedQuantity,
	}
	if state.LastRestockedAt != nil {
		seconds := state.LastRestockedAt.Unix()
		dto.LastRestockedAtUnix = &seconds
	}
	return dto
}

func loadAppError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return status.Error(codes.NotFound, "app not found")
	}
	return status.Error(codes.Internal, err.Error())
}

// serviceToStatus maps a domain error onto a gRPC status.
func serviceToStatus(err error) error {
	switch {
	case errors.Is(err, service.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, service.ErrValidation):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, service.ErrForbidden):
		return status.Error(codes.PermissionDenied, err.Error())
	case errors.Is(err, service.ErrUnauthorized):
		return status.Error(codes.Unauthenticated, err.Error())
	case errors.Is(err, service.ErrDependency):
		return status.Error(codes.Unavailable, err.Error())
	default:
		return status.Error(codes.Internal, err.Error())
	}
}
