package developmodule

import (
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"src.solsynth.dev/solsynth/distribution/internal/develop"
	"src.solsynth.dev/solsynth/distribution/internal/developapi"
	"src.solsynth.dev/solsynth/distribution/internal/service"
)

// registerMiniAppRoutes mounts the mini app surfaces.
//
//	GET    /api/private/miniapps                    mini apps of a project
//	GET    /api/private/miniapps/:id                one mini app
//	POST   /api/private/miniapps                    create
//	PATCH  /api/private/miniapps/:id                update
//	DELETE /api/private/miniapps/:id                delete
//	POST   /api/private/miniapps/:id/package        upload a package
//	GET    /api/miniapps                            published catalog
//	GET    /api/miniapps/:slug                      one published mini app
//	GET    /api/miniapps/:slug/package              download its package
func registerMiniAppRoutes(api *gin.RouterGroup, deps Deps) {
	viewer := deps.Deps.Authenticate(developapi.Option{Permission: developapi.PermissionMiniAppsView})
	create := deps.Deps.Authenticate(developapi.Option{Permission: developapi.PermissionMiniAppsCreate})
	update := deps.Deps.Authenticate(developapi.Option{Permission: developapi.PermissionMiniAppsUpdate})
	remove := deps.Deps.Authenticate(developapi.Option{Permission: developapi.PermissionMiniAppsDelete})
	upload := deps.Deps.Authenticate(developapi.Option{Permission: developapi.PermissionMiniAppsPackageUpload})

	private := api.Group("/private/miniapps")
	private.GET("", viewer, listMiniApps(deps))
	private.GET("/:id", viewer, getMiniApp(deps))
	private.POST("", create, createMiniApp(deps))
	private.PATCH("/:id", update, updateMiniApp(deps))
	private.DELETE("/:id", remove, deleteMiniApp(deps))
	private.POST("/:id/package", upload, uploadMiniAppPackage(deps))

	public := api.Group("/miniapps")
	public.GET("", discoverMiniApps(deps))
	public.GET("/:slug", getPublicMiniApp(deps))
	public.GET("/:slug/package", downloadMiniAppPackage(deps))
}

func listMiniApps(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		scope, ok := deps.Deps.Scope(c, roleViewer)
		if !ok {
			return
		}
		apps, err := deps.Service.ListMiniApps(c.Request.Context(), scope.Project.ID)
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		c.JSON(http.StatusOK, apps)
	}
}

func getMiniApp(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		scope, ok := deps.Deps.Scope(c, roleViewer)
		if !ok {
			return
		}
		app, err := deps.Service.LoadMiniApp(scope.Project.ID, c.Param("id"))
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		c.JSON(http.StatusOK, app)
	}
}

func createMiniApp(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input develop.MiniAppInput
		if !bindJSON(c, &input) {
			return
		}
		scope, ok := deps.Deps.Scope(c, roleEditor)
		if !ok {
			return
		}
		app, err := deps.Service.CreateMiniApp(c.Request.Context(), scope.Project.ID, input)
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		c.JSON(http.StatusCreated, app)
	}
}

func updateMiniApp(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input develop.MiniAppInput
		if !bindJSON(c, &input) {
			return
		}
		scope, ok := deps.Deps.Scope(c, roleEditor)
		if !ok {
			return
		}
		app, err := deps.Service.LoadMiniApp(scope.Project.ID, c.Param("id"))
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		updated, err := deps.Service.UpdateMiniApp(c.Request.Context(), app, input)
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		c.JSON(http.StatusOK, updated)
	}
}

func deleteMiniApp(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		scope, ok := deps.Deps.Scope(c, roleEditor)
		if !ok {
			return
		}
		app, err := deps.Service.LoadMiniApp(scope.Project.ID, c.Param("id"))
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		if err := deps.Service.DeleteMiniApp(c.Request.Context(), app); err != nil {
			developapi.WriteError(c, err)
			return
		}
		noContent(c)
	}
}

// uploadMiniAppPackage stores a ZIP archive and applies its manifest. The body
// is capped before it is buffered, so an oversized upload never lands in RAM.
func uploadMiniAppPackage(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		scope, ok := deps.Deps.Scope(c, roleEditor)
		if !ok {
			return
		}
		app, err := deps.Service.LoadMiniApp(scope.Project.ID, c.Param("id"))
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		maxBytes := deps.MaxPackageBytes
		if maxBytes <= 0 {
			maxBytes = 5 * 1024 * 1024
		}
		file, header, err := c.Request.FormFile("file")
		if err != nil {
			developapi.WriteError(c, validationError("a package file is required"))
			return
		}
		defer file.Close()
		if header.Size > maxBytes {
			developapi.WriteError(c, validationError("plugin package is too large"))
			return
		}
		if !strings.EqualFold(filepath.Ext(header.Filename), ".zip") {
			developapi.WriteError(c, validationError("only ZIP plugin packages are supported"))
			return
		}
		content, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
		if err != nil {
			developapi.WriteError(c, validationError("read the uploaded package"))
			return
		}
		if int64(len(content)) > maxBytes {
			developapi.WriteError(c, validationError("plugin package is too large"))
			return
		}
		result, err := deps.Service.UploadPackage(c.Request.Context(), app, header.Filename, content, maxBytes)
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"key":          result.Key,
			"url":          result.URL,
			"file_name":    result.FileName,
			"content_type": "application/zip",
			"size":         result.Size,
			"sha256":       result.Sha256,
		})
	}
}

// --- Public catalog ---

func discoverMiniApps(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit, ok := queryInt(c, "take", 20, 1, 100)
		if !ok {
			return
		}
		offset, ok := queryInt(c, "offset", 0, 0, 0)
		if !ok {
			return
		}
		apps, total, err := deps.Service.DiscoverMiniApps(c.Request.Context(), limit, offset, c.Query("search"))
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		views := make([]develop.MiniAppView, 0, len(apps))
		for index := range apps {
			views = append(views, deps.Service.PublicMiniApp(c.Request.Context(), &apps[index]))
		}
		totalHeader(c, int(total))
		c.JSON(http.StatusOK, views)
	}
}

func getPublicMiniApp(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		app, err := deps.Service.FindPublishedMiniApp(c.Param("slug"))
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		c.JSON(http.StatusOK, deps.Service.PublicMiniApp(c.Request.Context(), app))
	}
}

func downloadMiniAppPackage(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		file, redirectURL, err := deps.Service.DownloadPackage(c.Request.Context(), c.Param("slug"))
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		if redirectURL != "" {
			c.Redirect(http.StatusFound, redirectURL)
			return
		}
		if file == nil {
			developapi.WriteError(c, service.ErrNotFound)
			return
		}
		c.Header("Content-Disposition", "attachment; filename=\""+file.Name+"\"")
		c.Data(http.StatusOK, file.ContentType, file.Content)
	}
}
