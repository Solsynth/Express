// Package authz holds the narrow authorization contracts route surfaces use:
// global permission nodes and step-up ("sudo") elevation. Both are owned by
// Stargate and validated live, because a cached elevation or permission flag
// goes stale.
package authz

import (
	"context"
	"strings"

	gen "src.solsynth.dev/sosys/go/proto"
)

// Checker reports whether an account holds a global permission node.
type Checker interface {
	HasPermission(context.Context, string, string) (bool, error)
}

// SudoValidator reports whether an auth session is currently elevated, and
// which factor types the client may use to elevate.
type SudoValidator interface {
	ValidateSudo(ctx context.Context, sessionID string) (bool, []string, error)
}

type grpcChecker struct {
	client gen.DyPermissionServiceClient
}

// NewChecker wraps Stargate's DyPermissionService.
func NewChecker(client gen.DyPermissionServiceClient) Checker {
	if client == nil {
		return nil
	}
	return grpcChecker{client: client}
}

func (c grpcChecker) HasPermission(ctx context.Context, accountID, key string) (bool, error) {
	response, err := c.client.HasPermission(ctx, &gen.DyHasPermissionRequest{Actor: accountID, Key: key})
	if err != nil {
		return false, err
	}
	return response != nil && response.GetHasPermission(), nil
}

type grpcSudoValidator struct {
	client gen.DyAuthServiceClient
}

// NewSudoValidator wraps Stargate's DyAuthService.ValidateSudo.
func NewSudoValidator(client gen.DyAuthServiceClient) SudoValidator {
	if client == nil {
		return nil
	}
	return grpcSudoValidator{client: client}
}

func (v grpcSudoValidator) ValidateSudo(ctx context.Context, sessionID string) (bool, []string, error) {
	if strings.TrimSpace(sessionID) == "" {
		return false, nil, nil
	}
	response, err := v.client.ValidateSudo(ctx, &gen.DyValidateSudoRequest{SessionId: sessionID})
	if err != nil {
		return false, nil, err
	}
	return response.GetValid(), response.GetFactorTypes(), nil
}
