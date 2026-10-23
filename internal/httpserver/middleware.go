package httpserver

import (
	"github.com/gin-gonic/gin"

	"src.solsynth.dev/solsynth/distribution/internal/service"
)

// PublisherBearer is the exported publisher authorization middleware. Module
// route surfaces (internal/httpserver/syncctl) reuse it instead of
// re-implementing account, publisher, and permission checks.
func PublisherBearer(releases *service.ReleaseService, publishers service.PublisherDirectory, publisherPath bool, permission string) gin.HandlerFunc {
	return publisherBearer(releases, publishers, publisherPath, permission)
}

// ResolvePublisherID maps a publisher name to its publisher ID.
func ResolvePublisherID(c *gin.Context, publishers service.PublisherDirectory, publisherName string) (string, error) {
	return resolvePublisherID(c, publishers, publisherName)
}

// WriteError renders a service error with the standard status mapping.
func WriteError(c *gin.Context, err error) {
	writeError(c, err)
}
