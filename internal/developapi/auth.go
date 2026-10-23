// Package developapi holds the HTTP surface shared by the developer-portal
// modules: request authentication, publisher/developer authorization, global
// permission nodes, and step-up elevation.
//
// It mirrors the identity service's RemotePermissionMiddleware: a route names
// the permission node and, optionally, requires an elevated session; the
// middleware answers 401/403 before the handler runs.
package developapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	gen "src.solsynth.dev/sosys/go/proto"

	"src.solsynth.dev/solsynth/distribution/internal/authz"
	"src.solsynth.dev/solsynth/distribution/internal/develop"
	"src.solsynth.dev/solsynth/distribution/internal/service"
)

// Permission keys the developer portal gates on. They are the same nodes the
// identity service used, so existing grants keep working after the move.
const (
	PermissionDevelopersCreate = "developers.create"

	PermissionDevProjectsCreate = "dev.projects.create"
	PermissionDevProjectsUpdate = "dev.projects.update"
	PermissionDevProjectsDelete = "dev.projects.delete"

	PermissionCustomAppsCreate        = "custom.apps.create"
	PermissionCustomAppsUpdate        = "custom.apps.update"
	PermissionCustomAppsDelete        = "custom.apps.delete"
	PermissionCustomAppsSecretsManage = "custom.apps.secrets.manage"

	PermissionBotAccountsCreate     = "bot.accounts.create"
	PermissionBotAccountsUpdate     = "bot.accounts.update"
	PermissionBotAccountsDelete     = "bot.accounts.delete"
	PermissionBotAccountsKeysManage = "bot.accounts.keys.manage"
	PermissionBotAccountsChatManage = "bot.accounts.chat.manage"

	PermissionAppProductsCreate = "app.products.create"
	PermissionAppProductsUpdate = "app.products.update"
	PermissionAppProductsDelete = "app.products.delete"

	PermissionMiniAppsView          = "mini.apps.view"
	PermissionMiniAppsCreate        = "mini.apps.create"
	PermissionMiniAppsUpdate        = "mini.apps.update"
	PermissionMiniAppsDelete        = "mini.apps.delete"
	PermissionMiniAppsPackageUpload = "mini.apps.package.upload"

	// PermissionBoardScope is the OAuth scope an app must declare before it may
	// contribute board widgets.
	PermissionBoardScope = "accounts.profile.board"
)

// PortalPermissions lists every node the developer console asks about in one
// session lookup, so the SPA can render its navigation without probing routes.
var PortalPermissions = []string{
	PermissionDevelopersCreate,
	PermissionDevProjectsCreate,
	PermissionDevProjectsUpdate,
	PermissionDevProjectsDelete,
	PermissionCustomAppsCreate,
	PermissionCustomAppsUpdate,
	PermissionCustomAppsDelete,
	PermissionCustomAppsSecretsManage,
	PermissionBotAccountsCreate,
	PermissionBotAccountsUpdate,
	PermissionBotAccountsDelete,
	PermissionBotAccountsKeysManage,
	PermissionBotAccountsChatManage,
	PermissionAppProductsCreate,
	PermissionAppProductsUpdate,
	PermissionAppProductsDelete,
	PermissionMiniAppsView,
	PermissionMiniAppsCreate,
	PermissionMiniAppsUpdate,
	PermissionMiniAppsDelete,
	PermissionMiniAppsPackageUpload,
}

// Deps are the collaborators every portal route needs.
type Deps struct {
	Service    *develop.Service
	Publishers service.PublisherDirectory
	// Permissions answers global permission-node checks. Nil allows every check.
	Permissions authz.Checker
	// Sudo validates step-up elevation. Nil allows every elevated route.
	Sudo authz.SudoValidator
}

// Option gates one route: an account, an optional publisher role, an optional
// permission node, and optional step-up.
type Option struct {
	// Permission is the global permission node required for the account.
	Permission string
	// Sudo requires an elevated session.
	Sudo bool
}

// contextAccountKey is the gin key holding the authenticated account.
const contextAccountKey = "develop_account"

// Authenticate resolves the request's bearer token (or AuthToken cookie) into
// an account, enforcing the route's permission node and elevation.
func (d Deps) Authenticate(option Option) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, ok := requestToken(c)
		if !ok {
			WriteError(c, service.ErrUnauthorized)
			c.Abort()
			return
		}
		account, err := authenticate(c.Request.Context(), d.Publishers, token)
		if err != nil || strings.TrimSpace(account.ID) == "" {
			WriteError(c, errOr(err, service.ErrUnauthorized))
			c.Abort()
			return
		}
		c.Set(contextAccountKey, account)

		if option.Permission != "" {
			if err := d.requirePermission(c.Request.Context(), account, option.Permission); err != nil {
				WriteError(c, err)
				c.Abort()
				return
			}
		}
		if option.Sudo {
			if err := d.requireSudo(c.Request.Context(), account); err != nil {
				WriteError(c, err)
				c.Abort()
				return
			}
		}
		c.Next()
	}
}

// requirePermission enforces one global node, honouring superusers and the
// OAuth session's permission scopes exactly as the identity service did.
func (d Deps) requirePermission(ctx context.Context, account service.AuthenticatedAccount, key string) error {
	if account.IsSuperuser {
		return nil
	}
	if isOAuthSession(account.SessionType) {
		if hasFullScope(account.Scopes) {
			return nil
		}
		if !scopeAllows(account.Scopes, key) {
			return forbidden("%s was omitted by token scope", key)
		}
	}
	if d.Permissions == nil {
		return nil
	}
	allowed, err := d.Permissions.HasPermission(ctx, account.ID, key)
	if err != nil {
		return dependency(err)
	}
	if !allowed {
		return forbidden("%s was required", key)
	}
	return nil
}

// requireSudo enforces step-up elevation. It fails closed: an unverifiable
// elevation is never allowed through.
func (d Deps) requireSudo(ctx context.Context, account service.AuthenticatedAccount) error {
	if account.IsSuperuser {
		return nil
	}
	if strings.TrimSpace(account.SessionID) == "" {
		return unauthorized("An interactive session is required for this action.")
	}
	if d.Sudo == nil {
		return nil
	}
	elevated, factors, err := d.Sudo.ValidateSudo(ctx, account.SessionID)
	if err != nil {
		return dependency(err)
	}
	if !elevated {
		return &sudoRequiredError{factors: factors}
	}
	return nil
}

// sudoRequiredError carries the factor types a client may use to elevate.
type sudoRequiredError struct {
	factors []string
}

func (e *sudoRequiredError) Error() string {
	return "AUTH_SUDO_REQUIRED: step-up authentication is required"
}

// Account returns the authenticated account of the current request.
func Account(c *gin.Context) (service.AuthenticatedAccount, bool) {
	value, ok := c.Get(contextAccountKey)
	if !ok {
		return service.AuthenticatedAccount{}, false
	}
	account, ok := value.(service.AuthenticatedAccount)
	return account, ok
}

// Scope resolves the developer named by the dev query parameter, the caller's
// publisher role, and the project named by the proj query parameter.
func (d Deps) Scope(c *gin.Context, role gen.DyPublisherMemberRole) (*develop.DeveloperScope, bool) {
	account, ok := Account(c)
	if !ok {
		WriteError(c, service.ErrUnauthorized)
		return nil, false
	}
	scope, err := d.Service.ResolveScope(c.Request.Context(), account.ID, c.Query("dev"), role, c.Query("proj"))
	if err != nil {
		WriteError(c, err)
		return nil, false
	}
	return scope, true
}

// requestToken reads the bearer token, then the AuthToken cookie.
func requestToken(c *gin.Context) (string, bool) {
	fields := strings.Fields(c.GetHeader("Authorization"))
	if len(fields) == 2 && strings.EqualFold(fields[0], "Bearer") && strings.TrimSpace(fields[1]) != "" {
		return strings.TrimSpace(fields[1]), true
	}
	if token, err := c.Cookie("AuthToken"); err == nil && strings.TrimSpace(token) != "" {
		return strings.TrimSpace(token), true
	}
	return "", false
}

func authenticate(ctx context.Context, publishers service.PublisherDirectory, token string) (service.AuthenticatedAccount, error) {
	if authenticator, ok := publishers.(service.AccountAuthenticator); ok {
		return authenticator.AuthenticateAccount(ctx, token)
	}
	if publishers == nil {
		return service.AuthenticatedAccount{}, service.ErrDependency
	}
	accountID, err := publishers.Authenticate(ctx, token)
	return service.AuthenticatedAccount{ID: accountID}, err
}

// WriteError renders a service error with the standard status mapping. Portal
// errors use the fleet's ApiError body shape.
func WriteError(c *gin.Context, err error) {
	status, code, message := classify(err)
	body := gin.H{"code": code, "message": message, "status": status}
	var sudo *sudoRequiredError
	if errors.As(err, &sudo) {
		body["factor_types"] = sudo.factors
	}
	c.JSON(status, body)
}

func classify(err error) (int, string, string) {
	message := err.Error()
	if index := strings.Index(message, ": "); index >= 0 {
		message = message[index+2:]
	}
	switch {
	case errors.Is(err, service.ErrValidation):
		return http.StatusBadRequest, "VALIDATION_ERROR", message
	case errors.Is(err, service.ErrUnauthorized):
		// A bare sentinel would leak the internal word; the fleet's clients
		// expect the canonical sentence.
		if message == service.ErrUnauthorized.Error() {
			message = "Authentication is required."
		}
		return http.StatusUnauthorized, "UNAUTHORIZED", message
	case errors.Is(err, service.ErrNotFound):
		return http.StatusNotFound, "NOT_FOUND", message
	case errors.Is(err, service.ErrForbidden):
		return http.StatusForbidden, "FORBIDDEN", message
	case errors.Is(err, service.ErrConflict):
		return http.StatusConflict, "CONFLICT", message
	case errors.Is(err, service.ErrDependency):
		return http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", message
	}
	var sudo *sudoRequiredError
	if errors.As(err, &sudo) {
		return http.StatusForbidden, "AUTH_SUDO_REQUIRED", message
	}
	return http.StatusInternalServerError, "INTERNAL_ERROR", message
}

func forbidden(format string, args ...any) error {
	return errorf(service.ErrForbidden, format, args...)
}

func unauthorized(format string, args ...any) error {
	return errorf(service.ErrUnauthorized, format, args...)
}

func dependency(err error) error {
	return errorf(service.ErrDependency, "%v", err)
}

func errOr(err, fallback error) error {
	if err != nil {
		return err
	}
	return fallback
}
