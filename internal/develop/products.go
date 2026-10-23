package develop

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	gen "src.solsynth.dev/sosys/go/proto"

	"src.solsynth.dev/solsynth/distribution/internal/service"
)

// ProductInput is the create/update payload for an app product. Pointer fields
// distinguish "leave unchanged" from "clear".
type ProductInput struct {
	Identifier      *string             `json:"identifier"`
	DisplayName     *string             `json:"display_name"`
	Description     *string             `json:"description"`
	Currency        *string             `json:"currency"`
	Price           *float64            `json:"price"`
	PictureID       *string             `json:"picture_id"`
	BackgroundID    *string             `json:"background_id"`
	Recurrence      *string             `json:"recurrence"`
	GroupIdentifier *string             `json:"group_identifier"`
	Fulfillment     *ProductFulfillment `json:"fulfillment"`
	State           *ProductStateInput  `json:"state"`
}

// ProductStateInput is the sale-state patch of a product.
type ProductStateInput struct {
	IsEnabled     *bool  `json:"is_enabled"`
	StockMode     string `json:"stock_mode"`
	StockQuantity *int   `json:"stock_quantity"`
}

// ListProducts lists an app's products.
func (s *Service) ListProducts(appID string) ([]AppProduct, error) {
	var products []AppProduct
	if err := s.DB.Preload("State").Where("app_id = ?", appID).Order("identifier ASC").Find(&products).Error; err != nil {
		return nil, fmt.Errorf("list app products: %w", err)
	}
	if products == nil {
		products = []AppProduct{}
	}
	return products, nil
}

// LoadProduct resolves one product of an app.
func (s *Service) LoadProduct(appID, productID string) (*AppProduct, error) {
	var product AppProduct
	if err := s.DB.Preload("State").Where("id = ? AND app_id = ?", productID, appID).First(&product).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: product", service.ErrNotFound)
		}
		return nil, fmt.Errorf("load app product: %w", err)
	}
	return &product, nil
}

// CreateProduct adds a purchasable product to an app.
func (s *Service) CreateProduct(ctx context.Context, app *CustomApp, input ProductInput) (*AppProduct, error) {
	identifier := strings.TrimSpace(deref(input.Identifier))
	displayName := strings.TrimSpace(deref(input.DisplayName))
	currency := strings.TrimSpace(deref(input.Currency))
	if identifier == "" || displayName == "" || currency == "" {
		return nil, fmt.Errorf("%w: identifier, display_name and currency are required", service.ErrValidation)
	}
	var existing int64
	if err := s.DB.Model(&AppProduct{}).Where("app_id = ? AND identifier = ?", app.ID, identifier).Count(&existing).Error; err != nil {
		return nil, fmt.Errorf("check product identifier: %w", err)
	}
	if existing > 0 {
		return nil, fmt.Errorf("%w: a product with this identifier already exists", service.ErrConflict)
	}

	product := &AppProduct{
		ID:              newUUID(),
		Identifier:      identifier,
		DisplayName:     displayName,
		Description:     deref(input.Description),
		Currency:        currency,
		Price:           derefFloat(input.Price),
		AppID:           app.ID,
		Recurrence:      ParseRecurrence(deref(input.Recurrence)),
		GroupIdentifier: optionalString(input.GroupIdentifier),
		Fulfillment:     normalizeFulfillment(input.Fulfillment),
	}
	var err error
	if product.Picture, err = s.ResolveFile(ctx, deref(input.PictureID), "picture"); err != nil {
		return nil, err
	}
	if product.Background, err = s.ResolveFile(ctx, deref(input.BackgroundID), "background"); err != nil {
		return nil, err
	}
	product.State = buildProductState(input.State)

	if err := s.DB.Transaction(func(tx *gorm.DB) error {
		// The state row is written explicitly: creating it through the
		// association would insert it twice.
		state := product.State
		product.State = nil
		if err := tx.Create(product).Error; err != nil {
			return fmt.Errorf("create app product: %w", err)
		}
		state.ProductID = product.ID
		if err := tx.Create(state).Error; err != nil {
			return fmt.Errorf("create app product state: %w", err)
		}
		product.State = state
		return nil
	}); err != nil {
		return nil, err
	}
	s.syncSubscription(ctx, app, product)
	return product, nil
}

// UpdateProduct patches a product and re-syncs its subscription definition.
func (s *Service) UpdateProduct(ctx context.Context, app *CustomApp, product *AppProduct, input ProductInput) (*AppProduct, error) {
	if input.Identifier != nil {
		identifier := strings.TrimSpace(*input.Identifier)
		if identifier == "" {
			return nil, fmt.Errorf("%w: identifier must not be empty", service.ErrValidation)
		}
		if identifier != product.Identifier {
			var existing int64
			if err := s.DB.Model(&AppProduct{}).
				Where("app_id = ? AND identifier = ? AND id <> ?", product.AppID, identifier, product.ID).
				Count(&existing).Error; err != nil {
				return nil, fmt.Errorf("check product identifier: %w", err)
			}
			if existing > 0 {
				return nil, fmt.Errorf("%w: a product with this identifier already exists", service.ErrConflict)
			}
			product.Identifier = identifier
		}
	}
	if input.DisplayName != nil {
		if strings.TrimSpace(*input.DisplayName) == "" {
			return nil, fmt.Errorf("%w: display_name must not be empty", service.ErrValidation)
		}
		product.DisplayName = strings.TrimSpace(*input.DisplayName)
	}
	if input.Description != nil {
		product.Description = *input.Description
	}
	if input.Currency != nil {
		if strings.TrimSpace(*input.Currency) == "" {
			return nil, fmt.Errorf("%w: currency must not be empty", service.ErrValidation)
		}
		product.Currency = strings.TrimSpace(*input.Currency)
	}
	if input.Price != nil {
		product.Price = *input.Price
	}
	if input.Recurrence != nil {
		product.Recurrence = ParseRecurrence(*input.Recurrence)
	}
	if input.GroupIdentifier != nil {
		product.GroupIdentifier = optionalString(input.GroupIdentifier)
	}
	if input.Fulfillment != nil {
		product.Fulfillment = normalizeFulfillment(input.Fulfillment)
	}
	var err error
	if product.Picture, err = s.ResolveFile(ctx, deref(input.PictureID), "picture"); err != nil {
		return nil, err
	}
	if product.Background, err = s.ResolveFile(ctx, deref(input.BackgroundID), "background"); err != nil {
		return nil, err
	}
	applyProductState(product, input.State)

	if err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(product).Error; err != nil {
			return fmt.Errorf("update app product: %w", err)
		}
		if product.State == nil {
			return nil
		}
		product.State.ProductID = product.ID
		if err := tx.Save(product.State).Error; err != nil {
			return fmt.Errorf("update app product state: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	s.syncSubscription(ctx, app, product)
	return product, nil
}

// DeleteProduct removes a product and unregisters its subscription definition.
func (s *Service) DeleteProduct(ctx context.Context, app *CustomApp, product *AppProduct) error {
	if err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("product_id = ?", product.ID).Delete(&AppProductState{}).Error; err != nil {
			return fmt.Errorf("delete app product state: %w", err)
		}
		if err := tx.Delete(product).Error; err != nil {
			return fmt.Errorf("delete app product: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}
	s.removeSubscription(ctx, app, product)
	return nil
}

// syncSubscription mirrors a recurring product into the Wallet catalog. A
// transport failure leaves the definition stale; the next update re-syncs it.
func (s *Service) syncSubscription(ctx context.Context, app *CustomApp, product *AppProduct) {
	if s == nil || s.Subscriptions == nil || product.Recurrence == RecurrenceNone {
		return
	}
	request := &gen.DyRegisterAppSubscriptionDefinitionRequest{
		Identifier:        product.Identifier,
		AppIdentifier:     app.ResourceIdentifier(),
		DisplayName:       product.DisplayName,
		Currency:          product.Currency,
		BasePrice:         strconv.FormatFloat(product.Price, 'f', -1, 64),
		CycleDurationDays: int32(recurrenceDays(product.Recurrence)),
	}
	if product.GroupIdentifier != nil {
		request.GroupIdentifier = *product.GroupIdentifier
	}
	_ = s.Subscriptions.RegisterAppSubscriptionDefinition(ctx, request)
}

// removeSubscription withdraws a product from the Wallet catalog.
func (s *Service) removeSubscription(ctx context.Context, app *CustomApp, product *AppProduct) {
	if s == nil || s.Subscriptions == nil || product.Recurrence == RecurrenceNone {
		return
	}
	_ = s.Subscriptions.RegisterAppSubscriptionDefinition(ctx, &gen.DyRegisterAppSubscriptionDefinitionRequest{
		Identifier:    product.Identifier,
		AppIdentifier: app.ResourceIdentifier(),
		Remove:        true,
	})
}

func recurrenceDays(recurrence ProductRecurrence) int {
	switch recurrence {
	case RecurrenceWeekly:
		return 7
	case RecurrenceYearly:
		return 365
	default:
		return 30
	}
}

// buildProductState validates a new product's sale state.
func buildProductState(input *ProductStateInput) *AppProductState {
	state := &AppProductState{ID: newUUID(), IsEnabled: true, StockMode: StockUnlimited}
	if input != nil {
		if input.IsEnabled != nil {
			state.IsEnabled = *input.IsEnabled
		}
		state.StockMode = ParseStockMode(input.StockMode)
		state.StockQuantity = input.StockQuantity
	}
	stampRestock(state)
	return state
}

// applyProductState patches an existing product's sale state.
func applyProductState(product *AppProduct, input *ProductStateInput) {
	if input == nil {
		return
	}
	if product.State == nil {
		product.State = &AppProductState{ID: newUUID(), ProductID: product.ID, IsEnabled: true, StockMode: StockUnlimited}
	}
	if input.IsEnabled != nil {
		product.State.IsEnabled = *input.IsEnabled
	}
	if input.StockMode != "" {
		product.State.StockMode = ParseStockMode(input.StockMode)
	}
	if input.StockQuantity != nil {
		product.State.StockQuantity = input.StockQuantity
	}
	stampRestock(product.State)
}

// stampRestock records the restock time when manual stock is set.
func stampRestock(state *AppProductState) {
	if state == nil || state.StockMode != StockManual || state.StockQuantity == nil {
		return
	}
	now := time.Now().UTC()
	state.LastRestockedAt = &now
	state.LastRestockedQuantity = state.StockQuantity
}

// normalizeFulfillment dedupes the required scopes and drops an empty block.
func normalizeFulfillment(input *ProductFulfillment) *ProductFulfillment {
	if input == nil {
		return nil
	}
	scopes := make([]string, 0, len(input.RequiredScopes))
	seen := make(map[string]bool, len(input.RequiredScopes))
	for _, scope := range input.RequiredScopes {
		scope = strings.TrimSpace(scope)
		if scope == "" || seen[scope] {
			continue
		}
		seen[scope] = true
		scopes = append(scopes, scope)
	}
	return &ProductFulfillment{IsAddressRequired: input.IsAddressRequired, RequiredScopes: scopes}
}

func optionalString(value *string) *string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	return &trimmed
}

func derefFloat(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}
