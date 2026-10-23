// Package developmodule mounts the developer-portal HTTP surfaces. Each module
// owns one product area so a deployment can enable them independently.
package developmodule

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	gen "src.solsynth.dev/sosys/go/proto"

	"src.solsynth.dev/solsynth/distribution/internal/develop"
	"src.solsynth.dev/solsynth/distribution/internal/developapi"
	"src.solsynth.dev/solsynth/distribution/internal/eventbus"
	"src.solsynth.dev/solsynth/distribution/internal/service"
)

// Deps are the collaborators every portal route surface needs.
type Deps struct {
	Service *develop.Service
	Deps    developapi.Deps
	// Events publishes fleet events. Nil leaves the surfaces event-free.
	Events *eventbus.Bus
	// MaxPackageBytes caps one mini app archive. Zero uses the default.
	MaxPackageBytes int64
}

// accountID returns the authenticated account id or writes 401.
func accountID(c *gin.Context) (string, bool) {
	account, ok := developapi.Account(c)
	if !ok || strings.TrimSpace(account.ID) == "" {
		developapi.WriteError(c, service.ErrUnauthorized)
		return "", false
	}
	return account.ID, true
}

// queryInt reads a bounded integer query parameter.
func queryInt(c *gin.Context, key string, fallback, minimum, maximum int) (int, bool) {
	value := strings.TrimSpace(c.Query(key))
	if value == "" {
		return fallback, true
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		developapi.WriteError(c, validationError(key+" must be an integer"))
		return 0, false
	}
	if parsed < minimum {
		parsed = minimum
	}
	if maximum > 0 && parsed > maximum {
		parsed = maximum
	}
	return parsed, true
}

// bindJSON decodes a request body, reporting a validation error on failure.
func bindJSON(c *gin.Context, target any) bool {
	if err := c.ShouldBindJSON(target); err != nil {
		developapi.WriteError(c, errors.Join(service.ErrValidation, err))
		return false
	}
	return true
}

// validationError wraps a message as a validation failure.
func validationError(message string) error {
	return errors.Join(service.ErrValidation, errors.New(message))
}

// roleEditor and roleViewer name the membership roles route surfaces require.
const (
	roleViewer  = gen.DyPublisherMemberRole_DY_VIEWER
	roleEditor  = gen.DyPublisherMemberRole_DY_EDITOR
	roleManager = gen.DyPublisherMemberRole_DY_MANAGER
	roleOwner   = gen.DyPublisherMemberRole_DY_OWNER
)

// totalHeader reports a listing's total count the way the fleet's paginated
// endpoints do.
func totalHeader(c *gin.Context, total int) {
	c.Header("X-Total", strconv.Itoa(total))
}

// noContent answers a successful delete.
func noContent(c *gin.Context) { c.Status(http.StatusNoContent) }

// newEventID mints a fleet event identifier.
func newEventID() string { return uuid.NewString() }
