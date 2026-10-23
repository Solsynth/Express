// Package syncctl mounts the release-sync HTTP surface. The sync feature
// module owns registration so a deployment can disable the surface entirely.
package syncctl

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"src.solsynth.dev/solsynth/distribution/internal/httpserver"
	"src.solsynth.dev/solsynth/distribution/internal/service"
	syncengine "src.solsynth.dev/solsynth/distribution/internal/sync"
)

// Deps are the collaborators the sync routes need.
type Deps struct {
	Service    *syncengine.Service
	Releases   *service.ReleaseService
	Publishers service.PublisherDirectory
}

type createConnectionRequest struct {
	Provider string            `json:"provider"`
	Name     string            `json:"name"`
	BaseURL  string            `json:"base_url"`
	Secret   map[string]string `json:"secret"`
}

type linkRequest struct {
	ConnectionID string                   `json:"connection_id"`
	Repository   string                   `json:"repository"`
	Channels     syncengine.ChannelFilter `json:"channels"`
	Draft        bool                     `json:"draft"`
	Prerelease   bool                     `json:"prerelease"`
	Enabled      *bool                    `json:"enabled"`
}

// Register mounts the sync routes on the /api router group.
func Register(api *gin.RouterGroup, deps Deps) {
	if api == nil || deps.Service == nil {
		return
	}
	manage := httpserver.PublisherBearer(deps.Releases, deps.Publishers, true, service.PermissionSyncManage)
	productManage := httpserver.PublisherBearer(deps.Releases, deps.Publishers, false, service.PermissionSyncManage)

	connections := api.Group("/publishers/:publisherName/sync/connections")
	connections.GET("", manage, listConnections(deps))
	connections.POST("", manage, createConnection(deps))
	connections.PUT("/:connectionID", manage, updateConnection(deps))
	connections.DELETE("/:connectionID", manage, deleteConnection(deps))

	links := api.Group("/products/:productID/sync/links")
	links.GET("", listLinks(deps))
	links.POST("", productManage, createLink(deps))
	links.PUT("/:linkID", productManage, updateLink(deps))
	links.DELETE("/:linkID", productManage, deleteLink(deps))

	jobs := api.Group("/products/:productID/sync/jobs")
	jobs.GET("", productManage, listJobs(deps))
	jobs.POST("/:jobID/retry", productManage, retryJob(deps))
}

func listConnections(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		publisherID, ok := publisherID(c, deps)
		if !ok {
			return
		}
		connections, err := deps.Service.ListConnections(c.Request.Context(), publisherID)
		if err != nil {
			httpserver.WriteError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": connections, "providers": deps.Service.Providers()})
	}
}

func createConnection(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		publisherID, ok := publisherID(c, deps)
		if !ok {
			return
		}
		var input createConnectionRequest
		if err := c.ShouldBindJSON(&input); err != nil {
			httpserver.WriteError(c, errors.Join(service.ErrValidation, err))
			return
		}
		connection, err := deps.Service.CreateConnection(c.Request.Context(), publisherID, syncengine.ConnectionInput{
			Provider: input.Provider, Name: input.Name, BaseURL: input.BaseURL, Secret: input.Secret,
		})
		if err != nil {
			httpserver.WriteError(c, err)
			return
		}
		c.JSON(http.StatusCreated, connection)
	}
}

func updateConnection(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		publisherID, ok := publisherID(c, deps)
		if !ok {
			return
		}
		var input createConnectionRequest
		if err := c.ShouldBindJSON(&input); err != nil {
			httpserver.WriteError(c, errors.Join(service.ErrValidation, err))
			return
		}
		connection, err := deps.Service.UpdateConnection(c.Request.Context(), publisherID, c.Param("connectionID"), syncengine.ConnectionInput{
			Provider: input.Provider, Name: input.Name, BaseURL: input.BaseURL, Secret: input.Secret,
		})
		if err != nil {
			httpserver.WriteError(c, err)
			return
		}
		c.JSON(http.StatusOK, connection)
	}
}

func deleteConnection(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		publisherID, ok := publisherID(c, deps)
		if !ok {
			return
		}
		if err := deps.Service.DeleteConnection(c.Request.Context(), publisherID, c.Param("connectionID")); err != nil {
			httpserver.WriteError(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	}
}

func listLinks(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		links, err := deps.Service.ListLinks(c.Request.Context(), c.Param("productID"))
		if err != nil {
			httpserver.WriteError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": links})
	}
}

func createLink(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input linkRequest
		if err := c.ShouldBindJSON(&input); err != nil {
			httpserver.WriteError(c, errors.Join(service.ErrValidation, err))
			return
		}
		link, err := deps.Service.CreateLink(c.Request.Context(), c.Param("productID"), linkInput(input))
		if err != nil {
			httpserver.WriteError(c, err)
			return
		}
		c.JSON(http.StatusCreated, link)
	}
}

func updateLink(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input linkRequest
		if err := c.ShouldBindJSON(&input); err != nil {
			httpserver.WriteError(c, errors.Join(service.ErrValidation, err))
			return
		}
		link, err := deps.Service.UpdateLink(c.Request.Context(), c.Param("productID"), c.Param("linkID"), linkInput(input))
		if err != nil {
			httpserver.WriteError(c, err)
			return
		}
		c.JSON(http.StatusOK, link)
	}
}

func deleteLink(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := deps.Service.DeleteLink(c.Request.Context(), c.Param("productID"), c.Param("linkID")); err != nil {
			httpserver.WriteError(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	}
}

func listJobs(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		query := syncengine.JobQuery{State: strings.TrimSpace(c.Query("state"))}
		var err error
		if query.Limit, err = queryInt(c, "limit", 50); err != nil {
			httpserver.WriteError(c, err)
			return
		}
		if query.Offset, err = queryInt(c, "offset", 0); err != nil {
			httpserver.WriteError(c, err)
			return
		}
		page, err := deps.Service.ListJobs(c.Request.Context(), c.Param("productID"), query)
		if err != nil {
			httpserver.WriteError(c, err)
			return
		}
		c.JSON(http.StatusOK, page)
	}
}

func retryJob(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		job, err := deps.Service.RetryJob(c.Request.Context(), c.Param("productID"), c.Param("jobID"))
		if err != nil {
			httpserver.WriteError(c, err)
			return
		}
		c.JSON(http.StatusOK, job)
	}
}

func linkInput(input linkRequest) syncengine.LinkInput {
	return syncengine.LinkInput{
		ConnectionID: input.ConnectionID,
		Repository:   input.Repository,
		Channels:     input.Channels,
		Draft:        input.Draft,
		Prerelease:   input.Prerelease,
		Enabled:      input.Enabled,
	}
}

func publisherID(c *gin.Context, deps Deps) (string, bool) {
	publisherID, err := httpserver.ResolvePublisherID(c, deps.Publishers, c.Param("publisherName"))
	if err != nil {
		httpserver.WriteError(c, err)
		return "", false
	}
	return publisherID, true
}

func queryInt(c *gin.Context, key string, fallback int) (int, error) {
	value := c.Query(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, errors.Join(service.ErrValidation, errors.New(key+" must be an integer"))
	}
	return parsed, nil
}
