package developmodule

import (
	"net/http"

	"github.com/gin-gonic/gin"
	gen "src.solsynth.dev/sosys/go/proto"

	"src.solsynth.dev/solsynth/distribution/internal/develop"
	"src.solsynth.dev/solsynth/distribution/internal/developapi"
)

// registerProductRoutes mounts the app product surface.
//
//	GET    /api/private/apps/:appId/products                     products of an app
//	GET    /api/private/apps/:appId/products/:productId          one product
//	POST   /api/private/apps/:appId/products                     create
//	PATCH  /api/private/apps/:appId/products/:productId          update
//	DELETE /api/private/apps/:appId/products/:productId          delete
func registerProductRoutes(api *gin.RouterGroup, deps Deps) {
	viewer := deps.Deps.Authenticate(developapi.Option{Permission: developapi.PermissionAppProductsCreate})
	create := deps.Deps.Authenticate(developapi.Option{Permission: developapi.PermissionAppProductsCreate})
	update := deps.Deps.Authenticate(developapi.Option{Permission: developapi.PermissionAppProductsUpdate})
	remove := deps.Deps.Authenticate(developapi.Option{Permission: developapi.PermissionAppProductsDelete})

	products := api.Group("/private/apps/:appId/products")
	products.GET("", viewer, listProducts(deps))
	products.GET("/:productId", viewer, getProduct(deps))
	products.POST("", create, createProduct(deps))
	products.PATCH("/:productId", update, updateProduct(deps))
	products.DELETE("/:productId", remove, deleteProduct(deps))
}

// scopeProduct resolves the developer, project, app and product of a request.
func scopeProduct(c *gin.Context, deps Deps, role gen.DyPublisherMemberRole) (*develop.DeveloperScope, *develop.CustomApp, *develop.AppProduct, bool) {
	scope, ok := deps.Deps.Scope(c, role)
	if !ok {
		return nil, nil, nil, false
	}
	app, err := deps.Service.LoadApp(scope.Project.ID, c.Param("appId"))
	if err != nil {
		developapi.WriteError(c, err)
		return nil, nil, nil, false
	}
	product, err := deps.Service.LoadProduct(app.ID, c.Param("productId"))
	if err != nil {
		developapi.WriteError(c, err)
		return nil, nil, nil, false
	}
	return scope, app, product, true
}

// scopeAppOnly resolves the developer, project and app of a request.
func scopeAppOnly(c *gin.Context, deps Deps, role gen.DyPublisherMemberRole) (*develop.DeveloperScope, *develop.CustomApp, bool) {
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

func listProducts(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		_, app, ok := scopeAppOnly(c, deps, roleViewer)
		if !ok {
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

func getProduct(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		_, _, product, ok := scopeProduct(c, deps, roleViewer)
		if !ok {
			return
		}
		c.JSON(http.StatusOK, product)
	}
}

func createProduct(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input develop.ProductInput
		if !bindJSON(c, &input) {
			return
		}
		_, app, ok := scopeAppOnly(c, deps, roleEditor)
		if !ok {
			return
		}
		product, err := deps.Service.CreateProduct(c.Request.Context(), app, input)
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		c.JSON(http.StatusCreated, product)
	}
}

func updateProduct(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input develop.ProductInput
		if !bindJSON(c, &input) {
			return
		}
		_, app, product, ok := scopeProduct(c, deps, roleEditor)
		if !ok {
			return
		}
		updated, err := deps.Service.UpdateProduct(c.Request.Context(), app, product, input)
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		c.JSON(http.StatusOK, updated)
	}
}

func deleteProduct(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		_, app, product, ok := scopeProduct(c, deps, roleEditor)
		if !ok {
			return
		}
		if err := deps.Service.DeleteProduct(c.Request.Context(), app, product); err != nil {
			developapi.WriteError(c, err)
			return
		}
		noContent(c)
	}
}
