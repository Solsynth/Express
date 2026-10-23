package develop

import (
	"context"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
	gen "src.solsynth.dev/sosys/go/proto"

	"src.solsynth.dev/solsynth/distribution/internal/service"
)

// ProfileDirectory is the identity-service board contract: whether an account
// authorized an app for board access, and writing a board item's payload.
type ProfileDirectory interface {
	// AccountAuthorizedApp reports whether the account granted the app the
	// board scope.
	AccountAuthorizedApp(ctx context.Context, accountID, appSlug, appID string) (bool, error)
	// UpdateBoardItemPayload writes a validated payload onto the account's
	// board item, creating or locating it by app and widget key.
	UpdateBoardItemPayload(ctx context.Context, update BoardItemUpdate) (map[string]any, error)
}

// BoardItemUpdate is one payload write for an account board item.
type BoardItemUpdate struct {
	AccountID   string
	BoardItemID string
	AppID       string
	WidgetKey   string
	Payload     map[string]any
}

// authorizedAppsDirectory is the narrow Stargate contract for board consent.
type authorizedAppsDirectory struct {
	client gen.DyAuthorizedAppServiceClient
}

// NewAuthorizedAppsDirectory wraps Stargate's DyAuthorizedAppService.
func NewAuthorizedAppsDirectory(client gen.DyAuthorizedAppServiceClient) ProfileDirectory {
	if client == nil {
		return nil
	}
	return &authorizedAppsDirectory{client: client}
}

// AccountAuthorizedApp reports whether the account granted the app the board
// scope. A missing grant is false, never an error.
func (d *authorizedAppsDirectory) AccountAuthorizedApp(ctx context.Context, accountID, appSlug, appID string) (bool, error) {
	response, err := d.client.QueryAuthorizedBoardApps(ctx, &gen.DyQueryAuthorizedBoardAppsRequest{
		AccountId: accountID,
		AppSlug:   appSlug,
		Take:      50,
	})
	if err != nil {
		return false, rpcErrorToService(err, "authorized app")
	}
	for _, app := range response.GetApps() {
		if app.GetAppId() == appID {
			return true, nil
		}
	}
	return false, nil
}

// UpdateBoardItemPayload is served by the profile service, not this one. The
// portal keeps the consent check and delegates the write.
func (d *authorizedAppsDirectory) UpdateBoardItemPayload(context.Context, BoardItemUpdate) (map[string]any, error) {
	return nil, fmt.Errorf("%w: board payload writes require the profile service", service.ErrDependency)
}

// profilesDirectory writes board payloads through DyProfileService.
type profilesDirectory struct {
	client gen.DyProfileServiceClient
}

// NewProfilesDirectory wraps Passport's DyProfileService for board writes.
func NewProfilesDirectory(client gen.DyProfileServiceClient) ProfileDirectory {
	if client == nil {
		return nil
	}
	return &profilesDirectory{client: client}
}

// AccountAuthorizedApp is not served by the profile service; consent lives in
// Stargate's authorized-app table.
func (d *profilesDirectory) AccountAuthorizedApp(context.Context, string, string, string) (bool, error) {
	return false, fmt.Errorf("%w: board consent requires the authorized-app service", service.ErrDependency)
}

// UpdateBoardItemPayload writes a validated payload onto an account board item.
func (d *profilesDirectory) UpdateBoardItemPayload(ctx context.Context, update BoardItemUpdate) (map[string]any, error) {
	payload, err := structpb.NewValue(update.Payload)
	if err != nil {
		return nil, fmt.Errorf("%w: encode board payload", service.ErrValidation)
	}
	request := &gen.DyUpdateBoardItemPayloadRequest{
		AccountId:          update.AccountID,
		CustomAppId:        update.AppID,
		CustomAppWidgetKey: update.WidgetKey,
		Payload:            payload.GetStructValue(),
	}
	if update.BoardItemID != "" {
		request.BoardItemId = update.BoardItemID
	}
	response, err := d.client.UpdateBoardItemPayload(ctx, request)
	if err != nil {
		return nil, boardWriteError(err)
	}
	return boardItemToMap(response), nil
}

// boardWriteError maps the profile service's board failures onto the service
// sentinels so the HTTP layer answers 400/404 rather than 500.
func boardWriteError(err error) error {
	switch status.Code(err) {
	case codes.NotFound:
		return fmt.Errorf("%w: board item", service.ErrNotFound)
	case codes.InvalidArgument, codes.FailedPrecondition:
		return fmt.Errorf("%w: %s", service.ErrValidation, status.Convert(err).Message())
	default:
		return rpcErrorToService(err, "board item")
	}
}

// boardItemToMap renders a board item for the API response.
func boardItemToMap(item *gen.DyAccountBoardItem) map[string]any {
	if item == nil {
		return map[string]any{}
	}
	result := map[string]any{
		"id":         item.GetId(),
		"order":      item.GetOrder(),
		"kind":       item.GetKind().String(),
		"is_enabled": item.GetIsEnabled(),
	}
	if key := item.GetWidgetKey(); key != "" {
		result["widget_key"] = key
	}
	if appID := item.GetCustomAppId(); appID != "" {
		result["custom_app_id"] = appID
	}
	if key := item.GetCustomAppWidgetKey(); key != "" {
		result["custom_app_widget_key"] = key
	}
	if payload := item.GetPayload(); payload != nil {
		result["payload"] = payload.AsMap()
	}
	return result
}

// ComposeProfileDirectory combines consent and write, each backed by the
// service that owns it. A nil side degrades the dependent feature.
func ComposeProfileDirectory(consent, writes ProfileDirectory) ProfileDirectory {
	return compositeProfiles{consent: consent, writes: writes}
}

type compositeProfiles struct {
	consent ProfileDirectory
	writes  ProfileDirectory
}

func (c compositeProfiles) AccountAuthorizedApp(ctx context.Context, accountID, appSlug, appID string) (bool, error) {
	if c.consent == nil {
		return false, fmt.Errorf("%w: authorized-app service is not configured", service.ErrDependency)
	}
	return c.consent.AccountAuthorizedApp(ctx, accountID, appSlug, appID)
}

func (c compositeProfiles) UpdateBoardItemPayload(ctx context.Context, update BoardItemUpdate) (map[string]any, error) {
	if c.writes == nil {
		return nil, fmt.Errorf("%w: profile service is not configured", service.ErrDependency)
	}
	return c.writes.UpdateBoardItemPayload(ctx, update)
}
