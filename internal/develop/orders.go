package develop

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
	gen "src.solsynth.dev/sosys/go/proto"

	"src.solsynth.dev/solsynth/distribution/internal/service"
)

// CustomOrderInput is an ad-hoc order a developer opens for an app, used when
// the purchase is not one of the catalogued app products.
type CustomOrderInput struct {
	Identifier      string         `json:"identifier"`
	Currency        string         `json:"currency"`
	Amount          float64        `json:"amount"`
	ExpirationHours int            `json:"expiration_hours"`
	Remarks         string         `json:"remarks"`
	Meta            map[string]any `json:"meta"`
}

// CreateCustomOrder opens an order for an app in the wallet. The order is
// attributed to the app's resource identifier so the wallet can settle it
// against the right developer.
func (s *Service) CreateCustomOrder(ctx context.Context, app *CustomApp, input CustomOrderInput) (*gen.DyOrder, error) {
	if s == nil || s.Subscriptions == nil {
		return nil, fmt.Errorf("%w: payment service unavailable", service.ErrDependency)
	}
	identifier := strings.TrimSpace(input.Identifier)
	if identifier == "" {
		return nil, fmt.Errorf("%w: identifier is required", service.ErrValidation)
	}
	currency := strings.TrimSpace(input.Currency)
	if currency == "" {
		return nil, fmt.Errorf("%w: currency is required", service.ErrValidation)
	}
	if input.Amount < 0.001 {
		return nil, fmt.Errorf("%w: amount must be at least 0.001", service.ErrValidation)
	}
	hours := input.ExpirationHours
	if hours == 0 {
		hours = 24
	}
	if hours < 1 || hours > 720 {
		return nil, fmt.Errorf("%w: expiration_hours must be between 1 and 720", service.ErrValidation)
	}

	request := &gen.DyCreateOrderRequest{
		Currency:          currency,
		Amount:            strconv.FormatFloat(input.Amount, 'f', -1, 64),
		Expiration:        durationpb.New(time.Duration(hours) * time.Hour),
		AppIdentifier:     new(app.ResourceIdentifier()),
		ProductIdentifier: new(identifier),
		Reuseable:         false,
	}
	if remarks := strings.TrimSpace(input.Remarks); remarks != "" {
		request.Remarks = new(remarks)
	}
	if len(input.Meta) > 0 {
		// The wallet stores meta as JSON bytes, matching InfraObjectCoder.
		encoded, err := json.Marshal(input.Meta)
		if err != nil {
			return nil, fmt.Errorf("%w: meta is not serializable", service.ErrValidation)
		}
		request.Meta = encoded
	}

	order, err := s.Subscriptions.CreateOrder(ctx, request)
	if err != nil {
		return nil, dependencyError(err)
	}
	return order, nil
}
