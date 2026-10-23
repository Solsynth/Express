package developmodule

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	gen "src.solsynth.dev/sosys/go/proto"

	"src.solsynth.dev/solsynth/distribution/internal/develop"
	"src.solsynth.dev/solsynth/distribution/internal/developapi"
	"src.solsynth.dev/solsynth/distribution/internal/service"
)

// registerAppRoutes mounts the custom app, secret and board surfaces.
//
// Private (developer console):
//
//	GET    /api/private/apps                          apps of a project
//	GET    /api/private/apps/:appId                   one app
//	POST   /api/private/apps                          create
//	PATCH  /api/private/apps/:appId                   update
//	DELETE /api/private/apps/:appId                   delete (sudo)
//	GET    /api/private/apps/:appId/secrets           list secrets
//	POST   /api/private/apps/:appId/secrets           create secret (sudo)
//	GET    /api/private/apps/:appId/secrets/:id       one secret
//	DELETE /api/private/apps/:appId/secrets/:id       delete secret (sudo)
//	POST   /api/private/apps/:appId/secrets/:id/rotate rotate secret (sudo)
//	POST   /api/private/apps/:appId/orders            ad-hoc wallet order
//	GET    /api/private/apps/:appId/board             list widgets
//	POST   /api/private/apps/:appId/board             create widget
//	PUT    /api/private/apps/:appId/board/:key        update widget
//	DELETE /api/private/apps/:appId/board/:key        delete widget
//
// Public:
//
//	GET    /api/custom-apps                           production app catalog
//	GET    /api/custom-apps/board                     board-capable apps
//	GET    /api/custom-apps/:slug                     one app
//	GET    /api/custom-apps/:slug/products            its products
//	GET    /api/custom-apps/:slug/products/:sku       one product
//
// App-secret authenticated (no account):
//
//	GET    /api/private/apps/:appId/board/widgets     the app's own widgets
//	POST   /api/private/apps/:appId/board/payload     push a board payload
func registerAppRoutes(api *gin.RouterGroup, deps Deps) {
	viewer := deps.Deps.Authenticate(developapi.Option{})
	editor := deps.Deps.Authenticate(developapi.Option{Permission: developapi.PermissionCustomAppsUpdate})
	create := deps.Deps.Authenticate(developapi.Option{Permission: developapi.PermissionCustomAppsCreate})
	remove := deps.Deps.Authenticate(developapi.Option{Permission: developapi.PermissionCustomAppsDelete, Sudo: true})
	secrets := deps.Deps.Authenticate(developapi.Option{Permission: developapi.PermissionCustomAppsSecretsManage, Sudo: true})

	apps := api.Group("/private/apps")
	apps.GET("", viewer, listApps(deps))
	apps.GET("/:appId", viewer, getApp(deps))
	apps.POST("", create, createApp(deps))
	apps.PATCH("/:appId", editor, updateApp(deps))
	apps.DELETE("/:appId", remove, deleteApp(deps))

	apps.GET("/:appId/secrets", editor, listSecrets(deps))
	apps.POST("/:appId/secrets", secrets, createSecret(deps))
	apps.GET("/:appId/secrets/:secretId", editor, getSecret(deps))
	apps.DELETE("/:appId/secrets/:secretId", secrets, deleteSecret(deps))
	apps.POST("/:appId/secrets/:secretId/rotate", secrets, rotateSecret(deps))

	// An ad-hoc order for a purchase that is not a catalogued product.
	apps.POST("/:appId/orders", editor, createCustomOrder(deps))

	apps.GET("/:appId/board", editor, listWidgets(deps))
	apps.POST("/:appId/board", editor, createWidget(deps))
	apps.PUT("/:appId/board/:key", editor, updateWidget(deps))
	apps.DELETE("/:appId/board/:key", editor, deleteWidget(deps))
	// App-secret authenticated: a running app reads its own widgets and pushes
	// payloads for a consenting account.
	apps.GET("/:appId/board/widgets", appSecret(deps), listOwnWidgets(deps))
	apps.POST("/:appId/board/payload", appSecret(deps), updateBoardPayload(deps))

	public := api.Group("/custom-apps")
	public.GET("", discoverApps(deps))
	public.GET("/board", discoverBoardApps(deps))
	public.GET("/:slug", getPublicApp(deps))
	public.GET("/:slug/products", listPublicProducts(deps))
	public.GET("/:slug/products/:sku", getPublicProduct(deps))
}

// scopeApp resolves the developer, project and app for a console request.
func scopeApp(c *gin.Context, deps Deps, role gen.DyPublisherMemberRole) (*develop.DeveloperScope, *develop.CustomApp, bool) {
	scope, ok := deps.Deps.Scope(c, role)
	if !ok {
		return nil, nil, false
	}
	app, err := deps.Service.LoadApp(scope.Project.ID, c.Param("appId"))
	if err != nil {
		developapi.WriteError(c, err)
		return nil, nil, false
	}
	return scope, app, true
}

func listApps(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		scope, ok := deps.Deps.Scope(c, roleViewer)
		if !ok {
			return
		}
		apps, err := deps.Service.ListApps(scope.Project.ID)
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		c.JSON(http.StatusOK, apps)
	}
}

func getApp(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		_, app, ok := scopeApp(c, deps, roleViewer)
		if !ok {
			return
		}
		c.JSON(http.StatusOK, app)
	}
}

func createApp(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input develop.CustomAppInput
		if !bindJSON(c, &input) {
			return
		}
		scope, ok := deps.Deps.Scope(c, roleEditor)
		if !ok {
			return
		}
		app, err := deps.Service.CreateApp(c.Request.Context(), scope.Project.ID, input)
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		c.JSON(http.StatusCreated, app)
	}
}

func updateApp(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input develop.CustomAppInput
		if !bindJSON(c, &input) {
			return
		}
		_, app, ok := scopeApp(c, deps, roleEditor)
		if !ok {
			return
		}
		updated, err := deps.Service.UpdateApp(c.Request.Context(), app, input)
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		c.JSON(http.StatusOK, updated)
	}
}

func deleteApp(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		_, app, ok := scopeApp(c, deps, roleEditor)
		if !ok {
			return
		}
		if err := deps.Service.DeleteApp(app); err != nil {
			developapi.WriteError(c, err)
			return
		}
		noContent(c)
	}
}

// createCustomOrder opens an ad-hoc order in the wallet.
func createCustomOrder(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input develop.CustomOrderInput
		if !bindJSON(c, &input) {
			return
		}
		_, app, ok := scopeApp(c, deps, roleEditor)
		if !ok {
			return
		}
		order, err := deps.Service.CreateCustomOrder(c.Request.Context(), app, input)
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		c.JSON(http.StatusOK, order)
	}
}

// --- Secrets ---

// secretView hides the plaintext except on create and rotate, where it is the
// only time the caller can read it.
type secretView struct {
	ID          string `json:"id"`
	Secret      string `json:"secret,omitempty"`
	Description string `json:"description,omitempty"`
	ExpiresAt   any    `json:"expired_at,omitempty"`
	Type        string `json:"type"`
	CreatedAt   any    `json:"created_at"`
	UpdatedAt   any    `json:"updated_at"`
}

func secretToView(secret *develop.CustomAppSecret, reveal bool) secretView {
	view := secretView{
		ID:          secret.ID,
		Description: secret.Description,
		ExpiresAt:   secret.ExpiredAt,
		Type:        secret.SecretTypeName(),
		CreatedAt:   secret.CreatedAt,
		UpdatedAt:   secret.UpdatedAt,
	}
	if reveal {
		view.Secret = secret.Secret
	}
	return view
}

func listSecrets(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		_, app, ok := scopeApp(c, deps, roleEditor)
		if !ok {
			return
		}
		secrets, err := deps.Service.ListSecrets(app.ID)
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		views := make([]secretView, 0, len(secrets))
		for index := range secrets {
			views = append(views, secretToView(&secrets[index], false))
		}
		c.JSON(http.StatusOK, views)
	}
}

func getSecret(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		_, app, ok := scopeApp(c, deps, roleEditor)
		if !ok {
			return
		}
		secret, err := deps.Service.LoadSecret(app.ID, c.Param("secretId"))
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		c.JSON(http.StatusOK, secretToView(secret, false))
	}
}

func createSecret(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input develop.SecretInput
		if !bindJSON(c, &input) {
			return
		}
		_, app, ok := scopeApp(c, deps, roleEditor)
		if !ok {
			return
		}
		secret, err := deps.Service.CreateSecret(app.ID, input)
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		c.JSON(http.StatusCreated, secretToView(secret, true))
	}
}

func rotateSecret(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input develop.SecretInput
		if !bindJSON(c, &input) {
			return
		}
		_, app, ok := scopeApp(c, deps, roleEditor)
		if !ok {
			return
		}
		secret, err := deps.Service.RotateSecret(app.ID, c.Param("secretId"), input)
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		c.JSON(http.StatusOK, secretToView(secret, true))
	}
}

func deleteSecret(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		_, app, ok := scopeApp(c, deps, roleEditor)
		if !ok {
			return
		}
		if err := deps.Service.DeleteSecret(app.ID, c.Param("secretId")); err != nil {
			developapi.WriteError(c, err)
			return
		}
		noContent(c)
	}
}

// --- Board widgets ---

func listWidgets(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		_, app, ok := scopeApp(c, deps, roleEditor)
		if !ok {
			return
		}
		widgets, err := deps.Service.ListWidgets(app.ID)
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		manifests := make([]develop.BoardWidgetManifest, 0, len(widgets))
		for index := range widgets {
			manifests = append(manifests, widgets[index].Manifest())
		}
		c.JSON(http.StatusOK, manifests)
	}
}

func createWidget(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input develop.BoardWidgetManifest
		if !bindJSON(c, &input) {
			return
		}
		_, app, ok := scopeApp(c, deps, roleEditor)
		if !ok {
			return
		}
		widget, err := deps.Service.CreateWidget(app.ID, input)
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		c.JSON(http.StatusOK, widget.Manifest())
	}
}

func updateWidget(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input develop.BoardWidgetManifest
		if !bindJSON(c, &input) {
			return
		}
		_, app, ok := scopeApp(c, deps, roleEditor)
		if !ok {
			return
		}
		widget, err := deps.Service.UpdateWidget(app.ID, c.Param("key"), input)
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		c.JSON(http.StatusOK, widget.Manifest())
	}
}

func deleteWidget(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		_, app, ok := scopeApp(c, deps, roleEditor)
		if !ok {
			return
		}
		if err := deps.Service.DeleteWidget(app.ID, c.Param("key")); err != nil {
			developapi.WriteError(c, err)
			return
		}
		noContent(c)
	}
}

// --- App-secret authenticated board surface ---

// appSecret authenticates a running custom app by its API key. It accepts the
// bearer token or the X-App-Secret header, matching the identity service.
func appSecret(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		appID := strings.TrimSpace(c.Param("appId"))
		secret := requestAppSecret(c)
		if secret == "" {
			developapi.WriteError(c, service.ErrUnauthorized)
			c.Abort()
			return
		}
		valid, err := deps.Service.ValidateSecret(c.Request.Context(), appID, secret)
		if err != nil {
			developapi.WriteError(c, err)
			c.Abort()
			return
		}
		if !valid {
			developapi.WriteError(c, service.ErrUnauthorized)
			c.Abort()
			return
		}
		app, err := deps.Service.LoadApp("", appID)
		if err != nil {
			developapi.WriteError(c, err)
			c.Abort()
			return
		}
		c.Set(appSecretContextKey, app)
		c.Next()
	}
}

const appSecretContextKey = "develop_app_secret_app"

// requestAppSecret reads the app's API key from the request.
func requestAppSecret(c *gin.Context) string {
	fields := strings.Fields(c.GetHeader("Authorization"))
	if len(fields) == 2 && strings.EqualFold(fields[0], "Bearer") {
		return strings.TrimSpace(fields[1])
	}
	return strings.TrimSpace(c.GetHeader("X-App-Secret"))
}

func secretApp(c *gin.Context) (*develop.CustomApp, bool) {
	value, ok := c.Get(appSecretContextKey)
	if !ok {
		return nil, false
	}
	app, ok := value.(*develop.CustomApp)
	return app, ok
}

func listOwnWidgets(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		app, ok := secretApp(c)
		if !ok {
			developapi.WriteError(c, service.ErrUnauthorized)
			return
		}
		if !requireBoardScope(c, app) {
			return
		}
		widgets, err := deps.Service.ListWidgets(app.ID)
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		manifests := make([]develop.BoardWidgetManifest, 0, len(widgets))
		for index := range widgets {
			manifests = append(manifests, widgets[index].Manifest())
		}
		c.JSON(http.StatusOK, manifests)
	}
}

// boardPayloadRequest is the payload a custom app pushes for an account.
type boardPayloadRequest struct {
	AccountID   string         `json:"account_id"`
	BoardItemID string         `json:"board_item_id"`
	WidgetKey   string         `json:"widget_key"`
	Payload     map[string]any `json:"payload"`
}

func updateBoardPayload(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input boardPayloadRequest
		if !bindJSON(c, &input) {
			return
		}
		app, ok := secretApp(c)
		if !ok {
			developapi.WriteError(c, service.ErrUnauthorized)
			return
		}
		if !requireBoardScope(c, app) {
			return
		}
		if strings.TrimSpace(input.AccountID) == "" {
			developapi.WriteError(c, validationError("account_id is required"))
			return
		}
		if strings.TrimSpace(input.WidgetKey) == "" {
			developapi.WriteError(c, validationError("widget_key is required"))
			return
		}
		if deps.Service.Profiles == nil {
			developapi.WriteError(c, service.ErrDependency)
			return
		}
		authorized, err := deps.Service.Profiles.AccountAuthorizedApp(c.Request.Context(), input.AccountID, app.Slug, app.ID)
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		if !authorized {
			developapi.WriteError(c, service.ErrForbidden)
			return
		}
		result := deps.Service.ValidateBoardPayload(app, input.WidgetKey, input.Payload)
		if !result.Valid {
			developapi.WriteError(c, develop.BoardPayloadError(result.Message))
			return
		}
		item, err := deps.Service.Profiles.UpdateBoardItemPayload(c.Request.Context(), develop.BoardItemUpdate{
			AccountID:   input.AccountID,
			BoardItemID: input.BoardItemID,
			AppID:       app.ID,
			WidgetKey:   result.Widget.Key,
			Payload:     result.NormalizedPayload,
		})
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		c.JSON(http.StatusOK, item)
	}
}

// requireBoardScope asserts the app declared the board OAuth scope.
func requireBoardScope(c *gin.Context, app *develop.CustomApp) bool {
	if app.OauthConfig.HasScope(developapi.PermissionBoardScope) {
		return true
	}
	developapi.WriteError(c, service.ErrForbidden)
	return false
}

// --- Public catalog ---

func discoverApps(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit, ok := queryInt(c, "take", 20, 1, 100)
		if !ok {
			return
		}
		offset, ok := queryInt(c, "offset", 0, 0, 0)
		if !ok {
			return
		}
		apps, total, err := deps.Service.DiscoverApps(limit, offset, c.Query("search"))
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		totalHeader(c, int(total))
		c.JSON(http.StatusOK, apps)
	}
}

func discoverBoardApps(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit, ok := queryInt(c, "take", 20, 1, 100)
		if !ok {
			return
		}
		offset, ok := queryInt(c, "offset", 0, 0, 0)
		if !ok {
			return
		}
		apps, err := deps.Service.DiscoverBoardApps(c.Request.Context(), limit, offset, c.Query("slug"))
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		c.JSON(http.StatusOK, apps)
	}
}

func getPublicApp(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		app, err := deps.Service.FindAppBySlug(c.Param("slug"))
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		view := gin.H{"app": app}
		if app.Project != nil {
			if developer, err := deps.Service.LoadDeveloperByID(c.Request.Context(), app.Project.DeveloperID); err == nil {
				view["developer"] = developer
			}
		}
		c.JSON(http.StatusOK, view)
	}
}

func listPublicProducts(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		app, err := deps.Service.FindAppBySlug(c.Param("slug"))
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		products, err := deps.Service.ListProducts(app.ID)
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		c.JSON(http.StatusOK, products)
	}
}

func getPublicProduct(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		app, err := deps.Service.FindAppBySlug(c.Param("slug"))
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		products, err := deps.Service.ListProducts(app.ID)
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		for index := range products {
			if products[index].Identifier == c.Param("sku") {
				c.JSON(http.StatusOK, products[index])
				return
			}
		}
		developapi.WriteError(c, service.ErrNotFound)
	}
}
