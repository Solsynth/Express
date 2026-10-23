// Package httpserver exposes the DistributionCenter HTTP surface.
package httpserver

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/gin-gonic/gin"

	"src.solsynth.dev/solsynth/distribution/internal/config"
	"src.solsynth.dev/solsynth/distribution/internal/webui"
)

// Server owns the HTTP router and readiness state.
type Server struct {
	Engine *gin.Engine
	ready  atomic.Bool
	// WebError reports why the SPA was not mounted (missing dist directory or
	// no index.html). The server stays API-only in that case.
	WebError error
}

// New builds the health and metadata endpoints used by local operators and
// Blade service discovery. Domain routes are registered under /api afterwards.
//
// When [web] distPath is set, the built console is mounted here: existing dist
// files are served before the route tree (a fingerprinted asset must not cost
// an auth round-trip) and the NoRoute handler returns index.html for
// client-side routes.
func New(cfg *config.Config) *Server {
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	engine.Use(gin.Logger(), gin.Recovery())

	server := &Server{Engine: engine}
	if cfg != nil && strings.TrimSpace(cfg.Web.DistPath) != "" {
		console, err := webui.New(cfg.Web.DistPath)
		if err != nil {
			server.WebError = err
		} else {
			engine.Use(console.ServeStatic)
			engine.NoRoute(console.Serve)
		}
	}
	engine.GET("/", func(c *gin.Context) {
		// The console owns the root path when it is mounted; the service
		// metadata stays reachable for probes and for API-only deployments.
		if server.WebError == nil && cfg != nil && strings.TrimSpace(cfg.Web.DistPath) != "" {
			if file, ok := consoleIndex(cfg.Web.DistPath); ok {
				c.File(file)
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{
			"service": cfg.ServiceName,
			"version": cfg.Version,
		})
	})
	engine.GET("/health", func(c *gin.Context) { c.Status(http.StatusOK) })
	engine.GET("/alive", func(c *gin.Context) { c.Status(http.StatusOK) })
	engine.GET("/ready", func(c *gin.Context) {
		if !server.ready.Load() {
			c.Status(http.StatusServiceUnavailable)
			return
		}
		c.Status(http.StatusOK)
	})
	return server
}

// consoleIndex resolves the mounted console's entry document.
func consoleIndex(distPath string) (string, bool) {
	index := filepath.Join(distPath, "index.html")
	if info, err := os.Stat(index); err != nil || info.IsDir() {
		return "", false
	}
	return index, true
}

// SetReady changes the readiness probe result.
func (s *Server) SetReady(ready bool) {
	s.ready.Store(ready)
}
