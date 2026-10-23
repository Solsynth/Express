package develop

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"gorm.io/gorm"
	gen "src.solsynth.dev/sosys/go/proto"

	"src.solsynth.dev/solsynth/distribution/internal/service"
)

// BotDirectory is the identity-service bot contract: bot accounts and their
// API keys live there, the portal keeps only the local registry.
type BotDirectory interface {
	CreateBotAccount(ctx context.Context, req *gen.DyCreateBotAccountRequest) (*gen.DyBotAccount, error)
	UpdateBotAccount(ctx context.Context, req *gen.DyUpdateBotAccountRequest) (*gen.DyBotAccount, error)
	DeleteBotAccount(ctx context.Context, req *gen.DyDeleteBotAccountRequest) error

	ListAPIKeys(ctx context.Context, automatedID string) ([]*gen.DyApiKey, error)
	GetAPIKey(ctx context.Context, keyID string) (*gen.DyApiKey, error)
	CreateAPIKey(ctx context.Context, key *gen.DyApiKey) (*gen.DyApiKey, error)
	RotateAPIKey(ctx context.Context, keyID string) (*gen.DyApiKey, error)
	DeleteAPIKey(ctx context.Context, keyID string) error
}

// BotInput is the create payload for a bot account. Account profile fields are
// forwarded to the identity service verbatim.
type BotInput struct {
	Name         string `json:"name"`
	Nick         string `json:"nick"`
	Slug         string `json:"slug"`
	Language     string `json:"language"`
	FirstName    string `json:"first_name"`
	MiddleName   string `json:"middle_name"`
	LastName     string `json:"last_name"`
	Gender       string `json:"gender"`
	Pronouns     string `json:"pronouns"`
	TimeZone     string `json:"time_zone"`
	Location     string `json:"location"`
	Bio          string `json:"bio"`
	Birthday     *int64 `json:"birthday"`
	PictureID    string `json:"picture_id"`
	BackgroundID string `json:"background_id"`
}

// BotUpdateInput is the patch payload for a bot account.
type BotUpdateInput struct {
	BotInput
	IsActive *bool `json:"is_active"`
}

// ListBots lists a project's bots and hydrates their identity accounts.
func (s *Service) ListBots(ctx context.Context, projectID string) ([]BotAccount, map[string]*gen.DyAccount, error) {
	var bots []BotAccount
	if err := s.DB.Where("project_id = ?", projectID).Order("created_at ASC").Find(&bots).Error; err != nil {
		return nil, nil, fmt.Errorf("list bots: %w", err)
	}
	if bots == nil {
		bots = []BotAccount{}
	}
	accounts, err := s.LoadBotsAccounts(ctx, bots)
	if err != nil {
		return nil, nil, err
	}
	return bots, accounts, nil
}

// LoadBot resolves one bot of a project.
func (s *Service) LoadBot(projectID, botID string) (*BotAccount, error) {
	var bot BotAccount
	if err := s.DB.Where("id = ? AND project_id = ?", botID, projectID).First(&bot).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: bot", service.ErrNotFound)
		}
		return nil, fmt.Errorf("load bot: %w", err)
	}
	return &bot, nil
}

// CreateBot registers a bot account locally and in the identity service.
func (s *Service) CreateBot(ctx context.Context, project *DevProject, input BotInput) (*BotAccount, error) {
	if s == nil || s.Bots == nil {
		return nil, fmt.Errorf("%w: bot directory unavailable", service.ErrDependency)
	}
	slug := strings.TrimSpace(input.Slug)
	if slug == "" {
		return nil, fmt.Errorf("%w: slug is required", service.ErrValidation)
	}
	var existing int64
	if err := s.DB.Model(&BotAccount{}).Where("project_id = ? AND LOWER(slug) = ?", project.ID, strings.ToLower(slug)).Count(&existing).Error; err != nil {
		return nil, fmt.Errorf("check bot slug: %w", err)
	}
	if existing > 0 {
		return nil, fmt.Errorf("%w: a bot with this slug already exists in this project", service.ErrConflict)
	}

	botID := newUUID()
	account := botAccountProto(input)
	account.Id = botID
	createRequest := &gen.DyCreateBotAccountRequest{AutomatedId: botID, Account: account}
	if input.PictureID != "" {
		createRequest.PictureId = wrapperspb.String(input.PictureID)
	}
	if input.BackgroundID != "" {
		createRequest.BackgroundId = wrapperspb.String(input.BackgroundID)
	}
	created, err := s.Bots.CreateBotAccount(ctx, createRequest)
	if err != nil {
		return nil, botError(err)
	}

	bot := &BotAccount{ID: botID, Slug: slug, ProjectID: project.ID, Project: project, IsActive: created.GetIsActive()}
	if err := s.DB.Create(bot).Error; err != nil {
		// The remote account exists but the local row does not; roll it back so
		// the caller can retry without an orphan.
		_ = s.Bots.DeleteBotAccount(ctx, &gen.DyDeleteBotAccountRequest{AutomatedId: botID, Force: true})
		return nil, fmt.Errorf("create bot: %w", err)
	}
	return bot, nil
}

// UpdateBot patches a bot locally and in the identity service.
func (s *Service) UpdateBot(ctx context.Context, bot *BotAccount, input BotUpdateInput) (*BotAccount, error) {
	if s == nil || s.Bots == nil {
		return nil, fmt.Errorf("%w: bot directory unavailable", service.ErrDependency)
	}
	slug := strings.TrimSpace(input.Slug)
	if slug != "" && slug != bot.Slug {
		var existing int64
		if err := s.DB.Model(&BotAccount{}).
			Where("project_id = ? AND LOWER(slug) = ? AND id <> ?", bot.ProjectID, strings.ToLower(slug), bot.ID).
			Count(&existing).Error; err != nil {
			return nil, fmt.Errorf("check bot slug: %w", err)
		}
		if existing > 0 {
			return nil, fmt.Errorf("%w: a bot with this slug already exists in this project", service.ErrConflict)
		}
		bot.Slug = slug
	}

	account := botAccountProto(input.BotInput)
	updateRequest := &gen.DyUpdateBotAccountRequest{AutomatedId: bot.ID, Account: account}
	if input.PictureID != "" {
		updateRequest.PictureId = &input.PictureID
	}
	if input.BackgroundID != "" {
		updateRequest.BackgroundId = &input.BackgroundID
	}
	updated, err := s.Bots.UpdateBotAccount(ctx, updateRequest)
	if err != nil {
		return nil, botError(err)
	}
	bot.IsActive = updated.GetIsActive()
	if input.IsActive != nil {
		bot.IsActive = *input.IsActive
	}
	if err := s.DB.Save(bot).Error; err != nil {
		return nil, fmt.Errorf("update bot: %w", err)
	}
	return bot, nil
}

// DeleteBot removes a bot locally and in the identity service.
func (s *Service) DeleteBot(ctx context.Context, bot *BotAccount) error {
	if s == nil || s.Bots == nil {
		return fmt.Errorf("%w: bot directory unavailable", service.ErrDependency)
	}
	err := s.Bots.DeleteBotAccount(ctx, &gen.DyDeleteBotAccountRequest{AutomatedId: bot.ID})
	if err != nil && status.Code(err) != codes.NotFound {
		return botError(err)
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ?", bot.ID).Delete(&BotChatConfig{}).Error; err != nil {
			return fmt.Errorf("delete bot chat config: %w", err)
		}
		if err := tx.Delete(bot).Error; err != nil {
			return fmt.Errorf("delete bot: %w", err)
		}
		return nil
	})
}

// ListBotKeys lists a bot's API keys from the identity service.
func (s *Service) ListBotKeys(ctx context.Context, botID string) ([]*gen.DyApiKey, error) {
	if s == nil || s.Bots == nil {
		return nil, fmt.Errorf("%w: bot directory unavailable", service.ErrDependency)
	}
	keys, err := s.Bots.ListAPIKeys(ctx, botID)
	if err != nil {
		return nil, botError(err)
	}
	if keys == nil {
		keys = []*gen.DyApiKey{}
	}
	return keys, nil
}

// CreateBotKey mints an API key for a bot.
func (s *Service) CreateBotKey(ctx context.Context, botID, label string) (*gen.DyApiKey, error) {
	if strings.TrimSpace(label) == "" {
		return nil, fmt.Errorf("%w: label is required", service.ErrValidation)
	}
	key, err := s.Bots.CreateAPIKey(ctx, &gen.DyApiKey{AccountId: botID, Label: label})
	if err != nil {
		return nil, botError(err)
	}
	return key, nil
}

// RotateBotKey rotates an existing bot API key.
func (s *Service) RotateBotKey(ctx context.Context, keyID string) (*gen.DyApiKey, error) {
	key, err := s.Bots.RotateAPIKey(ctx, keyID)
	if err != nil {
		return nil, botError(err)
	}
	return key, nil
}

// DeleteBotKey revokes a bot API key.
func (s *Service) DeleteBotKey(ctx context.Context, keyID string) error {
	if err := s.Bots.DeleteAPIKey(ctx, keyID); err != nil {
		return botError(err)
	}
	return nil
}

// GetBotKey resolves one bot API key.
func (s *Service) GetBotKey(ctx context.Context, keyID string) (*gen.DyApiKey, error) {
	key, err := s.Bots.GetAPIKey(ctx, keyID)
	if err != nil {
		return nil, botError(err)
	}
	return key, nil
}

// --- Chat config ---

// DefaultChatConfig is the config returned before a bot has one stored.
func DefaultChatConfig(botID string) BotChatConfig {
	return BotChatConfig{
		ID:                   botID,
		Commands:             []BotCommand{},
		AutoApproveDm:        true,
		AutoApproveGroupChat: false,
		SupportChat:          true,
		SubscribedEvents:     []string{"messages.new"},
	}
}

// GetChatConfig returns a bot's chat config, creating the default row on first
// read so a subsequent update has something to patch.
func (s *Service) GetChatConfig(botID string) (*BotChatConfig, error) {
	var config BotChatConfig
	err := s.DB.Where("id = ?", botID).First(&config).Error
	if err == nil {
		return &config, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("load bot chat config: %w", err)
	}
	config = DefaultChatConfig(botID)
	if err := s.DB.Create(&config).Error; err != nil {
		return nil, fmt.Errorf("create bot chat config: %w", err)
	}
	return &config, nil
}

// PeekChatConfig returns a bot's chat config without creating one.
func (s *Service) PeekChatConfig(botID string) (*BotChatConfig, error) {
	var config BotChatConfig
	err := s.DB.Where("id = ?", botID).First(&config).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load bot chat config: %w", err)
	}
	return &config, nil
}

// SaveChatConfig replaces a bot's chat config.
func (s *Service) SaveChatConfig(botID string, input BotChatConfig) (*BotChatConfig, error) {
	config, err := s.GetChatConfig(botID)
	if err != nil {
		return nil, err
	}
	config.Commands = input.Commands
	config.AutoApproveDm = input.AutoApproveDm
	config.AutoApproveGroupChat = input.AutoApproveGroupChat
	config.SupportChat = input.SupportChat
	config.SubscribedEvents = input.SubscribedEvents
	if err := s.DB.Save(config).Error; err != nil {
		return nil, fmt.Errorf("save bot chat config: %w", err)
	}
	return config, nil
}

// PatchChatConfig applies a partial manifest onto a bot's chat config.
func (s *Service) PatchChatConfig(botID string, input BotManifestInput) (*BotChatConfig, error) {
	config, err := s.GetChatConfig(botID)
	if err != nil {
		return nil, err
	}
	if input.Commands != nil {
		config.Commands = *input.Commands
	}
	if input.AutoApproveDm != nil {
		config.AutoApproveDm = *input.AutoApproveDm
	}
	if input.AutoApproveGroupChat != nil {
		config.AutoApproveGroupChat = *input.AutoApproveGroupChat
	}
	if input.SupportChat != nil {
		config.SupportChat = *input.SupportChat
	}
	if input.SubscribedEvents != nil {
		config.SubscribedEvents = *input.SubscribedEvents
	}
	if err := s.DB.Save(config).Error; err != nil {
		return nil, fmt.Errorf("save bot chat config: %w", err)
	}
	return config, nil
}

// BotManifestInput is the partial chat-config payload a bot may push.
type BotManifestInput struct {
	Commands             *[]BotCommand `json:"commands"`
	AutoApproveDm        *bool         `json:"auto_approve_dm"`
	AutoApproveGroupChat *bool         `json:"auto_approve_group_chat"`
	SupportChat          *bool         `json:"support_chat"`
	SubscribedEvents     *[]string     `json:"subscribed_events"`
}

// botAccountProto builds the identity-service account for a bot.
func botAccountProto(input BotInput) *gen.DyAccount {
	account := &gen.DyAccount{
		Name:     input.Name,
		Nick:     input.Nick,
		Language: input.Language,
		Profile: &gen.DyAccountProfile{
			Bio:        wrapperspb.String(input.Bio),
			Gender:     wrapperspb.String(input.Gender),
			FirstName:  wrapperspb.String(input.FirstName),
			MiddleName: wrapperspb.String(input.MiddleName),
			LastName:   wrapperspb.String(input.LastName),
			TimeZone:   wrapperspb.String(input.TimeZone),
			Pronouns:   wrapperspb.String(input.Pronouns),
			Location:   wrapperspb.String(input.Location),
		},
	}
	if input.Birthday != nil {
		account.Profile.Birthday = timestamppb.New(time.Unix(*input.Birthday, 0).UTC())
	}
	return account
}

// botError maps a bot-directory failure onto a service error.
func botError(err error) error {
	switch status.Code(err) {
	case codes.AlreadyExists:
		return fmt.Errorf("%w: a bot account with this id already exists in the identity service", service.ErrConflict)
	case codes.NotFound:
		return fmt.Errorf("%w: bot account", service.ErrNotFound)
	case codes.InvalidArgument:
		return fmt.Errorf("%w: %s", service.ErrValidation, status.Convert(err).Message())
	default:
		return dependencyError(err)
	}
}

// LoadBotByID resolves a bot by its record id, hydrating its project.
func (s *Service) LoadBotByID(botID string) (*BotAccount, error) {
	var bot BotAccount
	if err := s.DB.Preload("Project").Where("id = ?", botID).First(&bot).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: bot", service.ErrNotFound)
		}
		return nil, fmt.Errorf("load bot: %w", err)
	}
	return &bot, nil
}
