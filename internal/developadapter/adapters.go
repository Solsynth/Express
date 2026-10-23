// Package developadapter wires the developer-portal domain to the fleet's gRPC
// services. Every adapter is optional: a deployment that leaves a target empty
// gets a clear dependency error from the feature that needs it instead of a
// silent no-op.
package developadapter

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/grpc"
	gen "src.solsynth.dev/sosys/go/proto"

	"src.solsynth.dev/solsynth/distribution/internal/authz"
	"src.solsynth.dev/solsynth/distribution/internal/config"
	"src.solsynth.dev/solsynth/distribution/internal/database"
	"src.solsynth.dev/solsynth/distribution/internal/develop"
	"src.solsynth.dev/solsynth/distribution/internal/integration"
)

// Connections are the dialed gRPC connections the adapters borrow. They are
// owned by the caller and closed with the process.
type Connections struct {
	Auth     *grpc.ClientConn
	Sphere   *grpc.ClientConn
	Files    *grpc.ClientConn
	Accounts *grpc.ClientConn
	Realms   *grpc.ClientConn
	Wallet   *grpc.ClientConn
	Actions  *grpc.ClientConn
	// Profiles serves board payload writes. It is optional: the consent check
	// lives in Stargate, so a deployment without Passport still lists board
	// widgets but cannot push payloads.
	Profiles *grpc.ClientConn
}

// Dial opens every configured developer-portal dependency. Auth and Sphere are
// required by the portal and dialed from the process's existing targets; the
// rest are optional.
func Dial(cfg *config.Config, authConn, sphereConn *grpc.ClientConn) (*Connections, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	connections := &Connections{Auth: authConn, Sphere: sphereConn}
	closeAll := func() {
		for _, conn := range []*grpc.ClientConn{connections.Files, connections.Accounts, connections.Realms, connections.Wallet, connections.Actions} {
			if conn != nil {
				_ = conn.Close()
			}
		}
	}
	dial := func(target config.ServiceTargetConfig) (*grpc.ClientConn, error) {
		if !target.Enabled() {
			return nil, nil
		}
		return integration.Dial(target.Target, target.UseTLS, target.TLSSkipVerify)
	}
	var err error
	if connections.Files, err = dial(cfg.Develop.Files); err != nil {
		closeAll()
		return nil, fmt.Errorf("dial files: %w", err)
	}
	if connections.Accounts, err = dial(cfg.Develop.Accounts); err != nil {
		closeAll()
		return nil, fmt.Errorf("dial accounts: %w", err)
	}
	if connections.Profiles, err = dial(cfg.Develop.Profiles); err != nil {
		closeAll()
		return nil, fmt.Errorf("dial profiles: %w", err)
	}
	if connections.Realms, err = dial(cfg.Develop.Realms); err != nil {
		closeAll()
		return nil, fmt.Errorf("dial realms: %w", err)
	}
	if connections.Wallet, err = dial(cfg.Develop.Wallet); err != nil {
		closeAll()
		return nil, fmt.Errorf("dial wallet: %w", err)
	}
	if connections.Actions, err = dial(cfg.Develop.ActionLog); err != nil {
		closeAll()
		return nil, fmt.Errorf("dial action log: %w", err)
	}
	return connections, nil
}

// Close closes every optional connection. Auth and Sphere belong to the caller.
func (c *Connections) Close() {
	if c == nil {
		return
	}
	for _, conn := range []*grpc.ClientConn{c.Files, c.Accounts, c.Realms, c.Wallet, c.Actions, c.Profiles} {
		if conn != nil {
			_ = conn.Close()
		}
	}
}

// Service composes the domain service from the configured dependencies.
func (c *Connections) Service(cfg *config.Config, db *database.DB) (*develop.Service, error) {
	if c == nil {
		return nil, fmt.Errorf("connections are required")
	}
	service := develop.New(db.DB, NewPublisherDirectory(c.Sphere))
	if c.Files != nil {
		service.Files = fileDirectory{client: gen.NewDyFileServiceClient(c.Files)}
	}
	if c.Accounts != nil {
		service.Accounts = accountDirectory{client: gen.NewDyAccountServiceClient(c.Accounts)}
	}
	if c.Realms != nil {
		service.Realms = realmDirectory{client: gen.NewDyRealmServiceClient(c.Realms)}
	}
	if c.Wallet != nil {
		service.Subscriptions = subscriptionCatalog{client: gen.NewDyPaymentServiceClient(c.Wallet)}
	}
	if c.Actions != nil {
		service.Actions = actionLogger{client: gen.NewDyActionLogServiceClient(c.Actions)}
	}
	if c.Auth != nil {
		service.Bots = botDirectory{client: gen.NewDyBotAccountReceiverServiceClient(c.Auth)}
		service.Permissions = authz.NewChecker(gen.NewDyPermissionServiceClient(c.Auth))
	}
	return service, nil
}

// publisherDirectory is the Sphere adapter for developer-portal publisher reads.
type publisherDirectory struct {
	client gen.DyPublisherServiceClient
}

// NewPublisherDirectory wraps the Sphere publisher client.
func NewPublisherDirectory(conn *grpc.ClientConn) develop.PublisherDirectory {
	if conn == nil {
		return nil
	}
	return publisherDirectory{client: gen.NewDyPublisherServiceClient(conn)}
}

func (d publisherDirectory) GetPublisher(ctx context.Context, ref string) (*gen.DyPublisher, error) {
	request := &gen.DyGetPublisherRequest{}
	if isUUID(ref) {
		request.Query = &gen.DyGetPublisherRequest_Id{Id: ref}
	} else {
		request.Query = &gen.DyGetPublisherRequest_Name{Name: ref}
	}
	response, err := d.client.GetPublisher(ctx, request)
	if err != nil {
		return nil, err
	}
	return response.GetPublisher(), nil
}

func (d publisherDirectory) GetPublisherBatch(ctx context.Context, ids []string) ([]*gen.DyPublisher, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	response, err := d.client.GetPublisherBatch(ctx, &gen.DyGetPublisherBatchRequest{Ids: ids})
	if err != nil {
		return nil, err
	}
	return response.GetPublishers(), nil
}

func (d publisherDirectory) IsPublisherMember(ctx context.Context, publisherID, accountID string, role gen.DyPublisherMemberRole) (bool, error) {
	response, err := d.client.IsPublisherMember(ctx, &gen.DyIsPublisherMemberRequest{
		PublisherId: publisherID,
		AccountId:   accountID,
		Role:        role,
	})
	if err != nil {
		return false, err
	}
	return response.GetValid(), nil
}

func (d publisherDirectory) ListPublishers(ctx context.Context, accountID string) ([]*gen.DyPublisher, error) {
	response, err := d.client.ListPublishers(ctx, &gen.DyListPublishersRequest{AccountId: accountID})
	if err != nil {
		return nil, err
	}
	return response.GetPublishers(), nil
}

// fileDirectory reads cloud-file metadata.
type fileDirectory struct {
	client gen.DyFileServiceClient
}

func (d fileDirectory) GetFile(ctx context.Context, id string) (*gen.DyCloudFile, error) {
	return d.client.GetFile(ctx, &gen.DyGetFileRequest{Id: id})
}

// UnsetFilePublic revokes public read access on a file.
func (d fileDirectory) UnsetFilePublic(ctx context.Context, fileID string) error {
	_, err := d.client.UnsetFilePublic(ctx, &gen.DyUnsetFilePublicRequest{FileId: fileID})
	return err
}

// accountDirectory reads accounts and bot accounts.
type accountDirectory struct {
	client gen.DyAccountServiceClient
}

func (d accountDirectory) GetAccount(ctx context.Context, id string) (*gen.DyAccount, error) {
	return d.client.GetAccount(ctx, &gen.DyGetAccountRequest{Id: id})
}

func (d accountDirectory) GetBotAccountBatch(ctx context.Context, automatedIDs []string) ([]*gen.DyAccount, error) {
	if len(automatedIDs) == 0 {
		return nil, nil
	}
	response, err := d.client.GetBotAccountBatch(ctx, &gen.DyGetBotAccountBatchRequest{AutomatedId: automatedIDs})
	if err != nil {
		return nil, err
	}
	return response.GetAccounts(), nil
}

// realmDirectory resolves realm ownership.
type realmDirectory struct {
	client gen.DyRealmServiceClient
}

func (d realmDirectory) GetRealmBatch(ctx context.Context, ids []string) ([]*gen.DyRealm, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	response, err := d.client.GetRealmBatch(ctx, &gen.DyGetRealmBatchRequest{Ids: ids})
	if err != nil {
		return nil, err
	}
	return response.GetRealms(), nil
}

// subscriptionCatalog registers recurring app products with Wallet.
type subscriptionCatalog struct {
	client gen.DyPaymentServiceClient
}

func (d subscriptionCatalog) RegisterAppSubscriptionDefinition(ctx context.Context, req *gen.DyRegisterAppSubscriptionDefinitionRequest) error {
	_, err := d.client.RegisterAppSubscriptionDefinition(ctx, req)
	return err
}

// CreateOrder opens an order in the wallet on the caller's behalf.
func (d subscriptionCatalog) CreateOrder(ctx context.Context, req *gen.DyCreateOrderRequest) (*gen.DyOrder, error) {
	return d.client.CreateOrder(ctx, req)
}

// actionLogger writes developer-program audit entries.
type actionLogger struct {
	client gen.DyActionLogServiceClient
}

func (d actionLogger) CreateActionLog(ctx context.Context, req *gen.DyCreateActionLogRequest) error {
	_, err := d.client.CreateActionLog(ctx, req)
	return err
}

// botDirectory is the identity-service bot contract.
type botDirectory struct {
	client gen.DyBotAccountReceiverServiceClient
}

func (d botDirectory) CreateBotAccount(ctx context.Context, req *gen.DyCreateBotAccountRequest) (*gen.DyBotAccount, error) {
	response, err := d.client.CreateBotAccount(ctx, req)
	if err != nil {
		return nil, err
	}
	return response.GetBot(), nil
}

func (d botDirectory) UpdateBotAccount(ctx context.Context, req *gen.DyUpdateBotAccountRequest) (*gen.DyBotAccount, error) {
	response, err := d.client.UpdateBotAccount(ctx, req)
	if err != nil {
		return nil, err
	}
	return response.GetBot(), nil
}

func (d botDirectory) DeleteBotAccount(ctx context.Context, req *gen.DyDeleteBotAccountRequest) error {
	_, err := d.client.DeleteBotAccount(ctx, req)
	return err
}

func (d botDirectory) ListAPIKeys(ctx context.Context, automatedID string) ([]*gen.DyApiKey, error) {
	response, err := d.client.ListApiKey(ctx, &gen.DyListApiKeyRequest{AutomatedId: automatedID})
	if err != nil {
		return nil, err
	}
	return response.GetData(), nil
}

func (d botDirectory) GetAPIKey(ctx context.Context, keyID string) (*gen.DyApiKey, error) {
	return d.client.GetApiKey(ctx, &gen.DyGetApiKeyRequest{Id: keyID})
}

func (d botDirectory) CreateAPIKey(ctx context.Context, key *gen.DyApiKey) (*gen.DyApiKey, error) {
	return d.client.CreateApiKey(ctx, key)
}

func (d botDirectory) RotateAPIKey(ctx context.Context, keyID string) (*gen.DyApiKey, error) {
	return d.client.RotateApiKey(ctx, &gen.DyGetApiKeyRequest{Id: keyID})
}

func (d botDirectory) DeleteAPIKey(ctx context.Context, keyID string) error {
	_, err := d.client.DeleteApiKey(ctx, &gen.DyGetApiKeyRequest{Id: keyID})
	return err
}

// isUUID reports whether a publisher reference is an id rather than a name.
func isUUID(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 36 {
		return false
	}
	for index, char := range value {
		switch index {
		case 8, 13, 18, 23:
			if char != '-' {
				return false
			}
		default:
			if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f' || char >= 'A' && char <= 'F') {
				return false
			}
		}
	}
	return true
}

// ProfileDirectory composes the board consent and board write directories from
// the configured connections. A missing target leaves that half nil, and the
// dependent route reports a dependency error.
func (c *Connections) ProfileDirectory() develop.ProfileDirectory {
	if c == nil {
		return nil
	}
	var consent, writes develop.ProfileDirectory
	if c.Auth != nil {
		consent = develop.NewAuthorizedAppsDirectory(gen.NewDyAuthorizedAppServiceClient(c.Auth))
	}
	if c.Profiles != nil {
		writes = develop.NewProfilesDirectory(gen.NewDyProfileServiceClient(c.Profiles))
	}
	if consent == nil && writes == nil {
		return nil
	}
	return develop.ComposeProfileDirectory(consent, writes)
}
