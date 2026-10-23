package developmodule

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"src.solsynth.dev/solsynth/distribution/internal/develop"
	"src.solsynth.dev/solsynth/distribution/internal/developapi"
)

// registerDeveloperRoutes mounts the developer program and project surfaces.
//
//	GET    /api/developers                       developers the account belongs to
//	GET    /api/developers/:name                 one developer
//	GET    /api/developers/:name/stats           public counters
//	POST   /api/developers/:name/enroll          join the program (owner only)
//	GET    /api/develop/quota                    bot quota for the account
//	GET    /api/private/projects                 projects of a developer
//	GET    /api/private/projects/:id             one project
//	POST   /api/private/projects                 create
//	PUT    /api/private/projects/:id             update
//	DELETE /api/private/projects/:id             delete
func registerDeveloperRoutes(api *gin.RouterGroup, deps Deps) {
	authenticated := deps.Deps.Authenticate(developapi.Option{})
	createProject := deps.Deps.Authenticate(developapi.Option{Permission: developapi.PermissionDevProjectsCreate})
	updateProject := deps.Deps.Authenticate(developapi.Option{Permission: developapi.PermissionDevProjectsUpdate})
	deleteProject := deps.Deps.Authenticate(developapi.Option{Permission: developapi.PermissionDevProjectsDelete})
	enroll := deps.Deps.Authenticate(developapi.Option{Permission: developapi.PermissionDevelopersCreate})

	developers := api.Group("/developers")
	developers.GET("", authenticated, listJoinedDevelopers(deps))
	developers.GET("/:name", getDeveloper(deps))
	developers.GET("/:name/stats", developerStats(deps))
	developers.POST("/:name/enroll", enroll, enrollDeveloper(deps))

	api.GET("/develop/quota", authenticated, developerQuota(deps))

	projects := api.Group("/private/projects")
	projects.GET("", authenticated, listProjects(deps))
	projects.GET("/:id", authenticated, getProject(deps))
	projects.POST("", createProject, createProjectHandler(deps))
	projects.PUT("/:id", updateProject, updateProjectHandler(deps))
	projects.DELETE("/:id", deleteProject, deleteProjectHandler(deps))
}

func listJoinedDevelopers(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		account, ok := accountID(c)
		if !ok {
			return
		}
		developers, total, err := deps.Service.ListJoinedDevelopers(c.Request.Context(), account)
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		totalHeader(c, total)
		c.JSON(http.StatusOK, developers)
	}
}

func getDeveloper(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		developer, err := deps.Service.LoadDeveloper(c.Request.Context(), c.Param("name"))
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		c.JSON(http.StatusOK, developer)
	}
}

func developerStats(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		developer, err := deps.Service.LoadDeveloper(c.Request.Context(), c.Param("name"))
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		stats, err := deps.Service.DeveloperStatsFor(developer.ID)
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		c.JSON(http.StatusOK, stats)
	}
}

// enrollDeveloper joins the developer program. Only the publisher owner may
// enroll it, which is stricter than the editor role the other routes accept.
func enrollDeveloper(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		account, ok := accountID(c)
		if !ok {
			return
		}
		name := c.Param("name")
		publisher, err := deps.Service.GetPublisher(c.Request.Context(), name)
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		if err := deps.Service.RequireMember(c.Request.Context(), publisher.GetId(), account, roleOwner); err != nil {
			developapi.WriteError(c, err)
			return
		}
		developer, err := deps.Service.EnrollDeveloper(c.Request.Context(), name)
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		deps.Service.Audit(c.Request.Context(), account, "developers.enroll",
			map[string]any{"publisher_id": publisher.GetId(), "publisher_name": publisher.GetName()},
			c.GetHeader("User-Agent"), c.ClientIP())
		c.JSON(http.StatusOK, developer)
	}
}

func developerQuota(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		account, ok := accountID(c)
		if !ok {
			return
		}
		quota, err := deps.Service.DeveloperQuota(c.Request.Context(), account)
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		c.JSON(http.StatusOK, quota)
	}
}

func listProjects(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		scope, ok := deps.Deps.Scope(c, roleViewer)
		if !ok {
			return
		}
		projects, err := deps.Service.ListProjects(scope.Developer.ID)
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		c.JSON(http.StatusOK, projects)
	}
}

func getProject(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		scope, ok := deps.Deps.Scope(c, roleViewer)
		if !ok {
			return
		}
		project, err := deps.Service.LoadProject(scope.Developer.ID, c.Param("id"))
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		c.JSON(http.StatusOK, project)
	}
}

func createProjectHandler(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input develop.ProjectInput
		if !bindJSON(c, &input) {
			return
		}
		scope, ok := deps.Deps.Scope(c, roleEditor)
		if !ok {
			return
		}
		project, err := deps.Service.CreateProject(scope.Developer.ID, input)
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		c.JSON(http.StatusCreated, project)
	}
}

func updateProjectHandler(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input develop.ProjectInput
		if !bindJSON(c, &input) {
			return
		}
		scope, ok := deps.Deps.Scope(c, roleManager)
		if !ok {
			return
		}
		project, err := deps.Service.UpdateProject(scope.Developer.ID, c.Param("id"), input)
		if err != nil {
			developapi.WriteError(c, err)
			return
		}
		c.JSON(http.StatusOK, project)
	}
}

func deleteProjectHandler(deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		scope, ok := deps.Deps.Scope(c, roleManager)
		if !ok {
			return
		}
		if err := deps.Service.DeleteProject(c.Request.Context(), scope.Developer.ID, c.Param("id")); err != nil {
			developapi.WriteError(c, err)
			return
		}
		noContent(c)
	}
}
