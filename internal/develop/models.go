// Package develop is the developer-portal domain: developers enrolled for a
// publisher, their projects, the custom apps and mini apps published from
// those projects, and the bots they operate.
//
// The package holds the schema and the reusable domain logic. Module packages
// under internal/modules/develop own route registration so a deployment can
// enable each surface independently.
package develop

import (
	"time"

	"gorm.io/gorm"

	"src.solsynth.dev/solsynth/distribution/internal/database"
)

// CustomAppStatus mirrors CustomAppStatus in the identity service.
type CustomAppStatus int

const (
	CustomAppDeveloping CustomAppStatus = iota
	CustomAppStaging
	CustomAppProduction
	CustomAppSuspended
)

// StatusName is the lowercase word the API returns for a status.
func (s CustomAppStatus) StatusName() string {
	switch s {
	case CustomAppStaging:
		return "staging"
	case CustomAppProduction:
		return "production"
	case CustomAppSuspended:
		return "suspended"
	default:
		return "developing"
	}
}

// ParseCustomAppStatus maps an API status word to its enum value.
func ParseCustomAppStatus(value string) (CustomAppStatus, bool) {
	switch value {
	case "developing":
		return CustomAppDeveloping, true
	case "staging":
		return CustomAppStaging, true
	case "production":
		return CustomAppProduction, true
	case "suspended":
		return CustomAppSuspended, true
	default:
		return CustomAppDeveloping, false
	}
}

// MiniAppStage mirrors MiniAppStage.
type MiniAppStage int

const (
	MiniAppDevelopment MiniAppStage = iota
	MiniAppStaging
	MiniAppProduction
)

// ParseMiniAppStage maps an API stage word to its enum value.
func ParseMiniAppStage(value string) (MiniAppStage, bool) {
	switch value {
	case "", "development":
		return MiniAppDevelopment, true
	case "staging":
		return MiniAppStaging, true
	case "production":
		return MiniAppProduction, true
	default:
		return MiniAppDevelopment, false
	}
}

// ProductRecurrence mirrors ProductRecurrence.
type ProductRecurrence int

const (
	RecurrenceNone ProductRecurrence = iota
	RecurrenceWeekly
	RecurrenceMonthly
	RecurrenceYearly
)

// Name returns the API word for a recurrence.
func (r ProductRecurrence) Name() string {
	switch r {
	case RecurrenceWeekly:
		return "weekly"
	case RecurrenceMonthly:
		return "monthly"
	case RecurrenceYearly:
		return "yearly"
	default:
		return ""
	}
}

// ParseRecurrence maps an API word to its enum value.
func ParseRecurrence(value string) ProductRecurrence {
	switch value {
	case "weekly":
		return RecurrenceWeekly
	case "monthly":
		return RecurrenceMonthly
	case "yearly":
		return RecurrenceYearly
	default:
		return RecurrenceNone
	}
}

// ProductStockMode mirrors ProductStockMode.
type ProductStockMode int

const (
	StockUnlimited ProductStockMode = iota
	StockDaily
	StockWeekly
	StockMonthly
	StockYearly
	StockManual
)

// Name returns the API word for a stock mode.
func (m ProductStockMode) Name() string {
	switch m {
	case StockDaily:
		return "daily"
	case StockWeekly:
		return "weekly"
	case StockMonthly:
		return "monthly"
	case StockYearly:
		return "yearly"
	case StockManual:
		return "manual"
	default:
		return "unlimited"
	}
}

// ParseStockMode maps an API word to its enum value.
func ParseStockMode(value string) ProductStockMode {
	switch value {
	case "daily":
		return StockDaily
	case "weekly":
		return StockWeekly
	case "monthly":
		return StockMonthly
	case "yearly":
		return StockYearly
	case "manual":
		return StockManual
	default:
		return StockUnlimited
	}
}

// Developer is one publisher enrolled in the developer program. It carries no
// timestamps, matching the identity service's SnDeveloper.
type Developer struct {
	ID          string `gorm:"primaryKey;size:36" json:"id"`
	PublisherID string `gorm:"size:36;index" json:"publisher_id"`

	Publisher *PublisherView `gorm:"-" json:"publisher,omitempty"`
}

func (Developer) TableName() string { return "developers" }

// PublisherView is the publisher projection returned with developers, bots and
// mini apps. Publishers live in Sphere; the view is hydrated over gRPC.
type PublisherView struct {
	ID      string                       `json:"id"`
	Name    string                       `json:"name"`
	Nick    string                       `json:"nick"`
	Bio     string                       `json:"bio,omitempty"`
	Picture *database.CloudFileReference `json:"picture,omitempty"`
}

// DevProject groups the apps and bots published under one developer.
type DevProject struct {
	ID          string         `gorm:"primaryKey;size:36" json:"id"`
	Slug        string         `gorm:"size:1024" json:"slug"`
	Name        string         `gorm:"size:1024" json:"name"`
	Description string         `gorm:"size:4096" json:"description"`
	DeveloperID string         `gorm:"size:36;index" json:"developer_id"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`

	Developer *Developer `gorm:"foreignKey:DeveloperID" json:"developer,omitempty"`
}

func (DevProject) TableName() string { return "dev_projects" }

// CustomAppLinks is the set of public URLs a custom app declares.
type CustomAppLinks struct {
	HomePage       string `json:"home_page,omitempty"`
	PrivacyPolicy  string `json:"privacy_policy,omitempty"`
	TermsOfService string `json:"terms_of_service,omitempty"`
}

// CustomAppOauthConfig is the OIDC client registration carried by a custom app.
type CustomAppOauthConfig struct {
	ClientUri              string   `json:"client_uri,omitempty"`
	RedirectUris           []string `json:"redirect_uris"`
	PostLogoutRedirectUris []string `json:"post_logout_redirect_uris,omitempty"`
	AllowedScopes          []string `json:"allowed_scopes"`
	AllowedGrantTypes      []string `json:"allowed_grant_types"`
	RequirePkce            bool     `json:"require_pkce"`
	AllowOfflineAccess     bool     `json:"allow_offline_access"`
	IsPublicClient         bool     `json:"is_public_client"`
}

// HasScope reports whether the app declares a scope. Scope comparison is
// case-insensitive, matching the identity service.
func (c *CustomAppOauthConfig) HasScope(scope string) bool {
	if c == nil {
		return false
	}
	for _, candidate := range c.AllowedScopes {
		if equalFold(candidate, scope) {
			return true
		}
	}
	return false
}

// VerificationMark mirrors the identity service's verification mark.
type VerificationMark struct {
	Type        string    `json:"type"`
	Title       string    `json:"title,omitempty"`
	Description string    `json:"description,omitempty"`
	VerifiedBy  string    `json:"verified_by,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// CustomApp is an application published by a developer project.
type CustomApp struct {
	ID              string                       `gorm:"primaryKey;size:36" json:"id"`
	Slug            string                       `gorm:"size:1024;index" json:"slug"`
	Name            string                       `gorm:"size:1024" json:"name"`
	Description     string                       `gorm:"size:4096" json:"description,omitempty"`
	Status          CustomAppStatus              `gorm:"not null;default:0" json:"status"`
	Picture         *database.CloudFileReference `gorm:"type:json;serializer:json" json:"picture,omitempty"`
	Background      *database.CloudFileReference `gorm:"type:json;serializer:json" json:"background,omitempty"`
	Verification    *VerificationMark            `gorm:"type:json;serializer:json" json:"verification,omitempty"`
	OauthConfig     *CustomAppOauthConfig        `gorm:"type:json;serializer:json" json:"oauth_config,omitempty"`
	Links           *CustomAppLinks              `gorm:"type:json;serializer:json" json:"links,omitempty"`
	PaymentWalletID *string                      `gorm:"size:36" json:"payment_wallet_id,omitempty"`
	ProjectID       string                       `gorm:"size:36;index" json:"project_id"`
	CreatedAt       time.Time                    `json:"created_at"`
	UpdatedAt       time.Time                    `json:"updated_at"`
	DeletedAt       gorm.DeletedAt               `gorm:"index" json:"-"`

	Project *DevProject `gorm:"foreignKey:ProjectID" json:"project,omitempty"`
}

func (CustomApp) TableName() string { return "custom_apps" }

// ResourceIdentifier is the product/payment namespace of this app. It matches
// the identity service's SnCustomApp.ResourceIdentifier.
func (a *CustomApp) ResourceIdentifier() string {
	if a == nil {
		return ""
	}
	return "developer.app:" + a.ID
}

// StatusName is the lowercase word the API returns for the app status.
func (a *CustomApp) StatusName() string {
	if a == nil {
		return ""
	}
	return a.Status.StatusName()
}

// CustomAppSecret is one API key or OIDC client secret of a custom app.
type CustomAppSecret struct {
	ID          string         `gorm:"primaryKey;size:36" json:"id"`
	Secret      string         `gorm:"size:1024" json:"-"`
	Description string         `gorm:"size:4096" json:"description,omitempty"`
	ExpiredAt   *time.Time     `json:"expired_at,omitempty"`
	IsOidc      bool           `json:"is_oidc"`
	AppID       string         `gorm:"size:36;index" json:"app_id"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

func (CustomAppSecret) TableName() string { return "custom_app_secrets" }

// SecretTypeName is the API word for the secret kind.
func (s *CustomAppSecret) SecretTypeName() string {
	if s != nil && s.IsOidc {
		return "oidc"
	}
	return "api_key"
}

// BoardFieldType describes one field of a board widget payload.
type BoardFieldType struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Label    string `json:"label,omitempty"`
	Format   string `json:"format,omitempty"`
	Required bool   `json:"required"`
}

// BoardWidget is one widget a custom app contributes to the account board.
type BoardWidget struct {
	ID              string           `gorm:"primaryKey;size:36" json:"id"`
	AppID           string           `gorm:"size:36;uniqueIndex:idx_board_widgets_app_key,priority:1" json:"app_id"`
	Key             string           `gorm:"size:128;uniqueIndex:idx_board_widgets_app_key,priority:2" json:"key"`
	Name            string           `gorm:"size:256" json:"name"`
	Description     string           `gorm:"size:1024" json:"description"`
	IsEnabled       bool             `gorm:"not null;default:true" json:"is_enabled"`
	RendererType    string           `gorm:"size:128;default:data" json:"renderer_type"`
	PayloadType     string           `gorm:"size:128;default:object" json:"payload_type"`
	FieldTypes      []BoardFieldType `gorm:"type:json;serializer:json" json:"field_types"`
	RequiredFields  []string         `gorm:"type:json;serializer:json" json:"required_fields"`
	MaxPayloadBytes *int             `json:"max_payload_bytes,omitempty"`
	AllowMultiple   bool             `gorm:"not null;default:true" json:"allow_multiple"`
	CreatedAt       time.Time        `json:"created_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
	DeletedAt       gorm.DeletedAt   `gorm:"index" json:"-"`
}

func (BoardWidget) TableName() string { return "board_widgets" }

// BoardWidgetManifest is the create/update payload for a board widget. The
// widget slug is derived, never stored.
type BoardWidgetManifest struct {
	Key             string           `json:"key"`
	Name            string           `json:"name"`
	Description     string           `json:"description"`
	IsEnabled       bool             `json:"is_enabled"`
	RendererType    string           `json:"renderer_type"`
	PayloadType     string           `json:"payload_type"`
	FieldTypes      []BoardFieldType `json:"field_types"`
	RequiredFields  []string         `json:"required_fields"`
	MaxPayloadBytes *int             `json:"max_payload_bytes"`
	AllowMultiple   bool             `json:"allow_multiple"`
}

// Manifest projects the stored widget onto the wire shape.
func (w *BoardWidget) Manifest() BoardWidgetManifest {
	if w == nil {
		return BoardWidgetManifest{}
	}
	payloadType := w.PayloadType
	if payloadType == "" {
		payloadType = "object"
	}
	return BoardWidgetManifest{
		Key:             w.Key,
		Name:            w.Name,
		Description:     w.Description,
		IsEnabled:       w.IsEnabled,
		RendererType:    w.RendererType,
		PayloadType:     payloadType,
		FieldTypes:      w.FieldTypes,
		RequiredFields:  w.RequiredFields,
		MaxPayloadBytes: w.MaxPayloadBytes,
		AllowMultiple:   w.AllowMultiple,
	}
}

// BotAccount is the developer-portal record of a bot account. The account
// itself lives in the identity service; this row is the local registry.
type BotAccount struct {
	ID        string         `gorm:"primaryKey;size:36" json:"id"`
	Slug      string         `gorm:"size:1024" json:"slug"`
	IsActive  bool           `gorm:"not null;default:true" json:"is_active"`
	ProjectID string         `gorm:"size:36;index" json:"project_id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	Project *DevProject `gorm:"foreignKey:ProjectID" json:"project,omitempty"`
}

func (BotAccount) TableName() string { return "bot_accounts" }

// BotCommandParameter is one parameter of a bot slash command.
type BotCommandParameter struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required"`
	Type        string `json:"type,omitempty"`
}

// BotCommand is one slash command a bot understands.
type BotCommand struct {
	Name        string                `json:"name"`
	Description string                `json:"description"`
	Usage       string                `json:"usage,omitempty"`
	Parameters  []BotCommandParameter `json:"parameters,omitempty"`
}

// BotChatConfig is the chat behaviour of a bot, keyed by the bot account ID.
type BotChatConfig struct {
	ID                   string         `gorm:"primaryKey;size:36" json:"id"`
	Commands             []BotCommand   `gorm:"type:json;serializer:json" json:"commands"`
	AutoApproveDm        bool           `gorm:"not null;default:true" json:"auto_approve_dm"`
	AutoApproveGroupChat bool           `gorm:"not null;default:false" json:"auto_approve_group_chat"`
	SupportChat          bool           `gorm:"not null;default:true" json:"support_chat"`
	SubscribedEvents     []string       `gorm:"type:json;serializer:json" json:"subscribed_events"`
	CreatedAt            time.Time      `json:"created_at"`
	UpdatedAt            time.Time      `json:"updated_at"`
	DeletedAt            gorm.DeletedAt `gorm:"index" json:"-"`
}

func (BotChatConfig) TableName() string { return "bot_chat_configs" }

// MiniAppManifest is the manifest.json inside an uploaded mini app package.
type MiniAppManifest struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Author      string   `json:"author,omitempty"`
	Description string   `json:"description,omitempty"`
	Entry       string   `json:"entry"`
	Permissions []string `json:"permissions,omitempty"`
	Background  bool     `json:"background"`
	Icon        string   `json:"icon,omitempty"`
	Homepage    string   `json:"homepage,omitempty"`
	EntryUrl    string   `json:"entry_url,omitempty"`
}

// MiniApp is a packaged client extension published by a developer project.
type MiniApp struct {
	ID                string                       `gorm:"primaryKey;size:36" json:"id"`
	Slug              string                       `gorm:"size:1024" json:"slug"`
	PluginID          *string                      `gorm:"size:256;uniqueIndex" json:"plugin_id,omitempty"`
	Name              *string                      `gorm:"size:256" json:"name,omitempty"`
	Version           string                       `gorm:"size:32;default:1.0.0" json:"version"`
	Author            *string                      `gorm:"size:256" json:"author,omitempty"`
	Description       *string                      `gorm:"size:4096" json:"description,omitempty"`
	EntryUrl          *string                      `gorm:"size:2048" json:"entry_url,omitempty"`
	Icon              *database.CloudFileReference `gorm:"type:json;serializer:json" json:"icon,omitempty"`
	Background        *database.CloudFileReference `gorm:"type:json;serializer:json" json:"background,omitempty"`
	Homepage          *string                      `gorm:"size:2048" json:"homepage,omitempty"`
	PackageUrl        *string                      `gorm:"size:2048" json:"package_url,omitempty"`
	PackageStorageKey *string                      `gorm:"size:512" json:"-"`
	PackageSha256     *string                      `gorm:"size:128" json:"package_sha256,omitempty"`
	PackageSize       *int64                       `json:"package_size,omitempty"`
	DownloadCount     int64                        `gorm:"not null;default:0" json:"download_count"`
	Stage             MiniAppStage                 `gorm:"not null;default:0" json:"stage"`
	Manifest          *MiniAppManifest             `gorm:"type:json;serializer:json" json:"manifest,omitempty"`
	ProjectID         string                       `gorm:"size:36;index" json:"project_id"`
	CreatedAt         time.Time                    `json:"created_at"`
	UpdatedAt         time.Time                    `json:"updated_at"`
	DeletedAt         gorm.DeletedAt               `gorm:"index" json:"-"`

	Project *DevProject `gorm:"foreignKey:ProjectID" json:"project,omitempty"`
}

func (MiniApp) TableName() string { return "mini_apps" }

// ProductFulfillment describes what a purchase of an app product delivers.
type ProductFulfillment struct {
	IsAddressRequired bool     `json:"is_address_required"`
	RequiredScopes    []string `json:"required_scopes"`
}

// AppProductState is the sale state of an app product.
type AppProductState struct {
	ID                    string           `gorm:"primaryKey;size:36" json:"id"`
	IsEnabled             bool             `gorm:"not null;default:true" json:"is_enabled"`
	StockMode             ProductStockMode `gorm:"not null;default:0" json:"stock_mode"`
	StockQuantity         *int             `json:"stock_quantity,omitempty"`
	LastRestockedAt       *time.Time       `json:"last_restocked_at,omitempty"`
	LastRestockedQuantity *int             `json:"last_restocked_quantity,omitempty"`
	ProductID             string           `gorm:"size:36;uniqueIndex" json:"product_id"`
	CreatedAt             time.Time        `json:"created_at"`
	UpdatedAt             time.Time        `json:"updated_at"`
	DeletedAt             gorm.DeletedAt   `gorm:"index" json:"-"`
}

func (AppProductState) TableName() string { return "app_product_states" }

// AppProduct is one purchasable item of a custom app.
type AppProduct struct {
	ID              string                       `gorm:"primaryKey;size:36" json:"id"`
	Identifier      string                       `gorm:"size:1024" json:"identifier"`
	DisplayName     string                       `gorm:"size:1024" json:"display_name"`
	Description     string                       `gorm:"size:4096" json:"description,omitempty"`
	Currency        string                       `gorm:"size:128" json:"currency"`
	Price           float64                      `gorm:"type:numeric" json:"price"`
	Picture         *database.CloudFileReference `gorm:"type:json;serializer:json" json:"picture,omitempty"`
	Background      *database.CloudFileReference `gorm:"type:json;serializer:json" json:"background,omitempty"`
	Fulfillment     *ProductFulfillment          `gorm:"type:json;serializer:json" json:"fulfillment,omitempty"`
	AppID           string                       `gorm:"size:36;index" json:"app_id"`
	Recurrence      ProductRecurrence            `gorm:"not null;default:0" json:"recurrence"`
	GroupIdentifier *string                      `gorm:"size:4096" json:"group_identifier,omitempty"`
	CreatedAt       time.Time                    `json:"created_at"`
	UpdatedAt       time.Time                    `json:"updated_at"`
	DeletedAt       gorm.DeletedAt               `gorm:"index" json:"-"`

	State *AppProductState `gorm:"foreignKey:ProductID" json:"state,omitempty"`
}

func (AppProduct) TableName() string { return "app_products" }

// Models is the schema owned by the developer portal. Modules pass the subset
// they own to module.Factory.Models.
func Models() []any {
	return []any{
		&Developer{},
		&DevProject{},
		&CustomApp{},
		&CustomAppSecret{},
		&BoardWidget{},
		&BotAccount{},
		&BotChatConfig{},
		&MiniApp{},
		&AppProduct{},
		&AppProductState{},
	}
}
