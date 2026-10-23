package developmodule

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"src.solsynth.dev/solsynth/distribution/internal/develop"
)

// The source tables and the query for each. Every uuid column is cast to text
// so it scans into the target's string ids, and every column is listed by name
// so a source schema change cannot silently shift a value into the wrong field.

func developersImporter() *importer {
	return &importer{
		moduleID: ModuleDevelopers,
		tables: []tableSpec{
			{
				name: "developers",
				query: func(cast string) string {
					return fmt.Sprintf(`SELECT id%s, publisher_id%s FROM developers`, cast, cast)
				},
				scan: func(ctx context.Context, rows *sql.Rows, state *importContext) (any, bool, error) {
					var id, publisherID string
					if err := rows.Scan(&id, &publisherID); err != nil {
						return nil, false, err
					}
					if _, ok := idOf(id); !ok {
						return nil, true, nil
					}
					state.plan("developers", id)
					return &develop.Developer{ID: id, PublisherID: strings.TrimSpace(publisherID)}, false, nil
				},
			},
			{
				name: "dev_projects",
				query: func(cast string) string {
					return fmt.Sprintf(`SELECT id%s, slug, name, description, developer_id%s,
				               created_at, updated_at, deleted_at
				        FROM dev_projects`, cast, cast)
				},
				scan: func(ctx context.Context, rows *sql.Rows, state *importContext) (any, bool, error) {
					var (
						id, slug, name, description, developerID string
						createdAt, updatedAt                     time.Time
						deleted                                  sql.NullTime
					)
					if err := rows.Scan(&id, &slug, &name, &description, &developerID, &createdAt, &updatedAt, &deleted); err != nil {
						return nil, false, err
					}
					if _, ok := idOf(id); !ok {
						return nil, true, nil
					}
					// A project whose developer is absent would dangle.
					if !state.exists(ctx, "developers", developerID) {
						return nil, true, nil
					}
					state.plan("dev_projects", id)
					return &develop.DevProject{
						ID:          id,
						Slug:        slug,
						Name:        name,
						Description: description,
						DeveloperID: strings.TrimSpace(developerID),
						CreatedAt:   createdAt,
						UpdatedAt:   updatedAt,
						DeletedAt:   deletedAt(deleted),
					}, false, nil
				},
			},
		},
	}
}

func appsImporter() *importer {
	return &importer{
		moduleID: ModuleApps,
		tables: []tableSpec{
			{
				name: "custom_apps",
				query: func(cast string) string {
					return fmt.Sprintf(`SELECT id%s, slug, name, COALESCE(description, ''), status,
				               picture, background, verification, oauth_config, links,
				               payment_wallet_id%s, project_id%s,
				               created_at, updated_at, deleted_at
				        FROM custom_apps`, cast, cast, cast)
				},
				scan: func(ctx context.Context, rows *sql.Rows, state *importContext) (any, bool, error) {
					var (
						id, slug, name, description, projectID string
						status                                 int
						picture, background, verification      []byte
						oauthConfig, links                     []byte
						walletID                               sql.NullString
						createdAt, updatedAt                   time.Time
						deleted                                sql.NullTime
					)
					if err := rows.Scan(&id, &slug, &name, &description, &status,
						&picture, &background, &verification, &oauthConfig, &links,
						&walletID, &projectID, &createdAt, &updatedAt, &deleted); err != nil {
						return nil, false, err
					}
					if _, ok := idOf(id); !ok {
						return nil, true, nil
					}
					// An app whose project is absent would dangle.
					if !state.exists(ctx, "dev_projects", projectID) {
						return nil, true, nil
					}
					app := &develop.CustomApp{
						ID:          id,
						Slug:        slug,
						Name:        name,
						Description: description,
						Status:      develop.CustomAppStatus(status),
						ProjectID:   strings.TrimSpace(projectID),
						CreatedAt:   createdAt,
						UpdatedAt:   updatedAt,
						DeletedAt:   deletedAt(deleted),
					}
					var err error
					if app.Picture, err = cloudFile(picture); err != nil {
						return nil, false, err
					}
					if app.Background, err = cloudFile(background); err != nil {
						return nil, false, err
					}
					if err := decodeJSON(verification, &app.Verification); err != nil {
						return nil, false, err
					}
					if err := decodeJSON(oauthConfig, &app.OauthConfig); err != nil {
						return nil, false, err
					}
					if err := decodeJSON(links, &app.Links); err != nil {
						return nil, false, err
					}
					app.PaymentWalletID = nullString(walletID)
					state.plan("custom_apps", id)
					return app, false, nil
				},
			},
			{
				name: "custom_app_secrets",
				query: func(cast string) string {
					return fmt.Sprintf(`SELECT id%s, secret, COALESCE(description, ''), expired_at, is_oidc,
				               app_id%s, created_at, updated_at, deleted_at
				        FROM custom_app_secrets`, cast, cast)
				},
				scan: func(ctx context.Context, rows *sql.Rows, state *importContext) (any, bool, error) {
					var (
						id, secret, description, appID string
						expiredAt                      sql.NullTime
						isOidc                         bool
						createdAt, updatedAt           time.Time
						deleted                        sql.NullTime
					)
					if err := rows.Scan(&id, &secret, &description, &expiredAt, &isOidc,
						&appID, &createdAt, &updatedAt, &deleted); err != nil {
						return nil, false, err
					}
					if _, ok := idOf(id); !ok {
						return nil, true, nil
					}
					if !state.exists(ctx, "custom_apps", appID) {
						return nil, true, nil
					}
					return &develop.CustomAppSecret{
						ID:          id,
						Secret:      secret,
						Description: description,
						ExpiredAt:   nullTime(expiredAt),
						IsOidc:      isOidc,
						AppID:       strings.TrimSpace(appID),
						CreatedAt:   createdAt,
						UpdatedAt:   updatedAt,
						DeletedAt:   deletedAt(deleted),
					}, false, nil
				},
			},
			{
				name: "board_widgets",
				query: func(cast string) string {
					return fmt.Sprintf(`SELECT id%s, app_id%s, key, COALESCE(name, ''), COALESCE(description, ''),
				               is_enabled, renderer_type, payload_type,
				               field_types, required_fields, max_payload_bytes, allow_multiple,
				               created_at, updated_at, deleted_at
				        FROM board_widgets`, cast, cast)
				},
				scan: func(ctx context.Context, rows *sql.Rows, state *importContext) (any, bool, error) {
					var (
						id, appID, key, name, description string
						isEnabled                         bool
						rendererType, payloadType         string
						fieldTypes, requiredFields        []byte
						maxPayloadBytes                   sql.NullInt64
						allowMultiple                     bool
						createdAt, updatedAt              time.Time
						deleted                           sql.NullTime
					)
					if err := rows.Scan(&id, &appID, &key, &name, &description,
						&isEnabled, &rendererType, &payloadType,
						&fieldTypes, &requiredFields, &maxPayloadBytes, &allowMultiple,
						&createdAt, &updatedAt, &deleted); err != nil {
						return nil, false, err
					}
					if _, ok := idOf(id); !ok {
						return nil, true, nil
					}
					if !state.exists(ctx, "custom_apps", appID) {
						return nil, true, nil
					}
					widget := &develop.BoardWidget{
						ID:            id,
						AppID:         strings.TrimSpace(appID),
						Key:           key,
						Name:          name,
						Description:   description,
						IsEnabled:     isEnabled,
						RendererType:  rendererType,
						PayloadType:   payloadType,
						AllowMultiple: allowMultiple,
						CreatedAt:     createdAt,
						UpdatedAt:     updatedAt,
						DeletedAt:     deletedAt(deleted),
					}
					if maxPayloadBytes.Valid {
						value := int(maxPayloadBytes.Int64)
						widget.MaxPayloadBytes = &value
					}
					if err := decodeJSON(fieldTypes, &widget.FieldTypes); err != nil {
						return nil, false, err
					}
					if err := decodeJSON(requiredFields, &widget.RequiredFields); err != nil {
						return nil, false, err
					}
					return widget, false, nil
				},
			},
		},
	}
}

func botsImporter() *importer {
	return &importer{
		moduleID: ModuleBots,
		tables: []tableSpec{
			{
				name: "bot_accounts",
				query: func(cast string) string {
					return fmt.Sprintf(`SELECT id%s, slug, is_active, project_id%s,
				               created_at, updated_at, deleted_at
				        FROM bot_accounts`, cast, cast)
				},
				scan: func(ctx context.Context, rows *sql.Rows, state *importContext) (any, bool, error) {
					var (
						id, slug, projectID  string
						isActive             bool
						createdAt, updatedAt time.Time
						deleted              sql.NullTime
					)
					if err := rows.Scan(&id, &slug, &isActive, &projectID, &createdAt, &updatedAt, &deleted); err != nil {
						return nil, false, err
					}
					if _, ok := idOf(id); !ok {
						return nil, true, nil
					}
					if !state.exists(ctx, "dev_projects", projectID) {
						return nil, true, nil
					}
					state.plan("bot_accounts", id)
					return &develop.BotAccount{
						ID:        id,
						Slug:      slug,
						IsActive:  isActive,
						ProjectID: strings.TrimSpace(projectID),
						CreatedAt: createdAt,
						UpdatedAt: updatedAt,
						DeletedAt: deletedAt(deleted),
					}, false, nil
				},
			},
			{
				name: "bot_chat_configs",
				query: func(cast string) string {
					return fmt.Sprintf(`SELECT id%s, commands, auto_approve_dm, auto_approve_group_chat,
				               support_chat, subscribed_events, created_at, updated_at, deleted_at
				        FROM bot_chat_configs`, cast)
				},
				scan: func(ctx context.Context, rows *sql.Rows, state *importContext) (any, bool, error) {
					var (
						id                              string
						commands, subscribedEvents      []byte
						autoApproveDm, autoApproveGroup bool
						supportChat                     bool
						createdAt, updatedAt            time.Time
						deleted                         sql.NullTime
					)
					if err := rows.Scan(&id, &commands, &autoApproveDm, &autoApproveGroup,
						&supportChat, &subscribedEvents, &createdAt, &updatedAt, &deleted); err != nil {
						return nil, false, err
					}
					if _, ok := idOf(id); !ok {
						return nil, true, nil
					}
					// A chat config is keyed by its bot: importing one for a
					// bot that did not land would leave an orphan.
					if !state.exists(ctx, "bot_accounts", id) {
						return nil, true, nil
					}
					config := &develop.BotChatConfig{
						ID:                   id,
						AutoApproveDm:        autoApproveDm,
						AutoApproveGroupChat: autoApproveGroup,
						SupportChat:          supportChat,
						CreatedAt:            createdAt,
						UpdatedAt:            updatedAt,
						DeletedAt:            deletedAt(deleted),
					}
					if err := decodeJSON(commands, &config.Commands); err != nil {
						return nil, false, err
					}
					if err := decodeJSON(subscribedEvents, &config.SubscribedEvents); err != nil {
						return nil, false, err
					}
					return config, false, nil
				},
			},
		},
	}
}

func miniAppsImporter() *importer {
	return &importer{
		moduleID: ModuleMiniApps,
		tables: []tableSpec{
			{
				name: "mini_apps",
				query: func(cast string) string {
					return fmt.Sprintf(`SELECT id%s, slug, plugin_id, name, COALESCE(version, '1.0.0'),
				               author, description, entry_url, icon, background, homepage,
				               package_url, package_storage_key, package_sha256, package_size,
				               COALESCE(download_count, 0), stage, manifest, project_id%s,
				               created_at, updated_at, deleted_at
				        FROM mini_apps`, cast, cast)
				},
				scan: func(ctx context.Context, rows *sql.Rows, state *importContext) (any, bool, error) {
					var (
						id, slug, version, projectID  string
						pluginID, name, author        sql.NullString
						description, entryURL         sql.NullString
						icon, background              []byte
						homepage, packageURL          sql.NullString
						packageStorageKey, packageSha sql.NullString
						packageSize                   sql.NullInt64
						downloadCount                 int64
						stage                         int
						manifest                      []byte
						createdAt, updatedAt          time.Time
						deleted                       sql.NullTime
					)
					if err := rows.Scan(&id, &slug, &pluginID, &name, &version,
						&author, &description, &entryURL, &icon, &background, &homepage,
						&packageURL, &packageStorageKey, &packageSha, &packageSize,
						&downloadCount, &stage, &manifest, &projectID,
						&createdAt, &updatedAt, &deleted); err != nil {
						return nil, false, err
					}
					if _, ok := idOf(id); !ok {
						return nil, true, nil
					}
					if !state.exists(ctx, "dev_projects", projectID) {
						return nil, true, nil
					}
					app := &develop.MiniApp{
						ID:                id,
						Slug:              slug,
						PluginID:          nullString(pluginID),
						Name:              nullString(name),
						Version:           version,
						Author:            nullString(author),
						Description:       nullString(description),
						EntryUrl:          nullString(entryURL),
						Homepage:          nullString(homepage),
						PackageUrl:        nullString(packageURL),
						PackageStorageKey: nullString(packageStorageKey),
						PackageSha256:     nullString(packageSha),
						DownloadCount:     downloadCount,
						Stage:             develop.MiniAppStage(stage),
						ProjectID:         strings.TrimSpace(projectID),
						CreatedAt:         createdAt,
						UpdatedAt:         updatedAt,
						DeletedAt:         deletedAt(deleted),
					}
					if packageSize.Valid {
						size := packageSize.Int64
						app.PackageSize = &size
					}
					var err error
					if app.Icon, err = cloudFile(icon); err != nil {
						return nil, false, err
					}
					if app.Background, err = cloudFile(background); err != nil {
						return nil, false, err
					}
					if err := decodeJSON(manifest, &app.Manifest); err != nil {
						return nil, false, err
					}
					return app, false, nil
				},
			},
		},
	}
}

func productsImporter() *importer {
	return &importer{
		moduleID: ModuleProducts,
		tables: []tableSpec{
			{
				name: "app_products",
				query: func(cast string) string {
					return fmt.Sprintf(`SELECT id%s, identifier, display_name, COALESCE(description, ''),
				               currency, price%s, picture, background, fulfillment,
				               app_id%s, COALESCE(recurrence, 0), group_identifier,
				               created_at, updated_at, deleted_at
				        FROM app_products`, cast, cast, cast)
				},
				scan: func(ctx context.Context, rows *sql.Rows, state *importContext) (any, bool, error) {
					var (
						id, identifier, displayName, description string
						currency, priceText                      string
						picture, background, fulfillment         []byte
						appID                                    string
						recurrence                               int
						groupIdentifier                          sql.NullString
						createdAt, updatedAt                     time.Time
						deleted                                  sql.NullTime
					)
					if err := rows.Scan(&id, &identifier, &displayName, &description,
						&currency, &priceText, &picture, &background, &fulfillment,
						&appID, &recurrence, &groupIdentifier,
						&createdAt, &updatedAt, &deleted); err != nil {
						return nil, false, err
					}
					if _, ok := idOf(id); !ok {
						return nil, true, nil
					}
					if !state.exists(ctx, "custom_apps", appID) {
						return nil, true, nil
					}
					price, err := parsePrice(priceText)
					if err != nil {
						return nil, false, fmt.Errorf("product %s: %w", id, err)
					}
					product := &develop.AppProduct{
						ID:              id,
						Identifier:      identifier,
						DisplayName:     displayName,
						Description:     description,
						Currency:        currency,
						Price:           price,
						AppID:           strings.TrimSpace(appID),
						Recurrence:      develop.ProductRecurrence(recurrence),
						GroupIdentifier: nullString(groupIdentifier),
						CreatedAt:       createdAt,
						UpdatedAt:       updatedAt,
						DeletedAt:       deletedAt(deleted),
					}
					if product.Picture, err = cloudFile(picture); err != nil {
						return nil, false, err
					}
					if product.Background, err = cloudFile(background); err != nil {
						return nil, false, err
					}
					if err := decodeJSON(fulfillment, &product.Fulfillment); err != nil {
						return nil, false, err
					}
					state.plan("app_products", id)
					return product, false, nil
				},
			},
			{
				name: "app_product_states",
				query: func(cast string) string {
					return fmt.Sprintf(`SELECT id%s, is_enabled, stock_mode, stock_quantity,
				               last_restocked_at, last_restocked_quantity, product_id%s,
				               created_at, updated_at, deleted_at
				        FROM app_product_states`, cast, cast)
				},
				scan: func(ctx context.Context, rows *sql.Rows, state *importContext) (any, bool, error) {
					var (
						id                    string
						isEnabled             bool
						stockMode             int
						stockQuantity         sql.NullInt64
						lastRestockedAt       sql.NullTime
						lastRestockedQuantity sql.NullInt64
						productID             string
						createdAt, updatedAt  time.Time
						deleted               sql.NullTime
					)
					if err := rows.Scan(&id, &isEnabled, &stockMode, &stockQuantity,
						&lastRestockedAt, &lastRestockedQuantity, &productID,
						&createdAt, &updatedAt, &deleted); err != nil {
						return nil, false, err
					}
					if _, ok := idOf(id); !ok {
						return nil, true, nil
					}
					if !state.exists(ctx, "app_products", productID) {
						return nil, true, nil
					}
					productState := &develop.AppProductState{
						ID:        id,
						IsEnabled: isEnabled,
						StockMode: develop.ProductStockMode(stockMode),
						ProductID: strings.TrimSpace(productID),
						CreatedAt: createdAt,
						UpdatedAt: updatedAt,
						DeletedAt: deletedAt(deleted),
					}
					if stockQuantity.Valid {
						value := int(stockQuantity.Int64)
						productState.StockQuantity = &value
					}
					if lastRestockedQuantity.Valid {
						value := int(lastRestockedQuantity.Int64)
						productState.LastRestockedQuantity = &value
					}
					productState.LastRestockedAt = nullTime(lastRestockedAt)
					return productState, false, nil
				},
			},
		},
	}
}

// parsePrice reads the source's numeric price column. Postgres renders it as
// text, so a decimal parse is the safe conversion.
func parsePrice(raw string) (float64, error) {
	var price float64
	if _, err := fmt.Sscanf(strings.TrimSpace(raw), "%g", &price); err != nil {
		return 0, fmt.Errorf("parse price %q: %w", raw, err)
	}
	return price, nil
}

// marshalJSON is used by the importer tests to build source jsonb values.
func marshalJSON(value any) []byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return encoded
}
