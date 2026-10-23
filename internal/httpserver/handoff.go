package httpserver

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"src.solsynth.dev/sosys/go/pkg/handoff"

	"src.solsynth.dev/solsynth/distribution/internal/config"
	"src.solsynth.dev/solsynth/distribution/internal/service"
)

// refreshTokenCookieName holds the pair's refresh token. It lives beside
// AuthToken on this console's own domain and is only ever read by the refresh
// route; no other route accepts it as a credential.
const refreshTokenCookieName = "RefreshToken"

// handoffRoutes is the relying-app side of the identity hand-off: the console
// signs in on the issuer, comes back with a one-time code, and this server
// trades that code for a token pair it stores in its own cookies.
type handoffRoutes struct {
	client *handoff.TokenClient
	cfg    *config.Config
}

// registerHandoffRoutes mounts the console's session endpoints. They stay
// mounted when the hand-off is unconfigured so the console gets an explicit
// 503 instead of a 404 that reads as a routing bug.
func registerHandoffRoutes(engine *gin.Engine, releases *service.ReleaseService, publishers service.PublisherDirectory, cfg *config.Config) {
	if engine == nil {
		return
	}
	routes := &handoffRoutes{cfg: cfg}
	if cfg != nil {
		routes.client = &handoff.TokenClient{
			BaseURL:      strings.TrimSpace(cfg.Handoff.BaseURL),
			ClientID:     strings.TrimSpace(cfg.Handoff.ClientID),
			ClientSecret: cfg.Handoff.ClientSecret,
		}
	}

	engine.POST("/api/auth/session", routes.redeem(releases, publishers))
	engine.POST("/api/auth/refresh", routes.refresh)
	engine.POST("/api/auth/logout", routes.logout)
}

// enabled reports whether the issuer hand-off is configured. A disabled
// deployment answers 503 rather than pretending the caller is signed out.
func (h *handoffRoutes) enabled() bool {
	return h.cfg != nil && h.cfg.HandoffEnabled()
}

// redeem exchanges the console's one-time code for a token pair, plants this
// console's own cookies, and answers with the same session body
// /api/console/session serves so the console parses one shape either way.
func (h *handoffRoutes) redeem(releases *service.ReleaseService, publishers service.PublisherDirectory) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !h.enabled() {
			writeHandoffDisabled(c)
			return
		}
		var input struct {
			Code string `json:"code"`
		}
		if err := c.ShouldBindJSON(&input); err != nil || strings.TrimSpace(input.Code) == "" {
			writeHandoffFailure(c, handoff.NewError(handoff.CodeInvalidRequest, "The request carried no hand-off code."))
			return
		}

		tokens, err := h.client.Redeem(c.Request.Context(), input.Code)
		if err != nil {
			// A refused code leaves the browser exactly as it was: nothing is
			// planted, so a stale session is not silently replaced by one.
			writeHandoffFailure(c, err)
			return
		}

		// Resolving the account before the cookies are set keeps a token the
		// fleet does not accept from becoming a session that 401s everywhere.
		view, err := buildConsoleSession(c, releases, publishers, tokens.Token)
		if err != nil {
			writeError(c, err)
			return
		}

		h.setSessionCookies(c, tokens)
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, view)
	}
}

// refresh trades the refresh cookie for a fresh pair, so an access token that
// expired mid-session does not end the session. Anything the issuer refuses
// clears both cookies and reads as signed out.
func (h *handoffRoutes) refresh(c *gin.Context) {
	if !h.enabled() {
		writeHandoffDisabled(c)
		return
	}
	refreshToken, err := c.Cookie(refreshTokenCookieName)
	if err != nil || strings.TrimSpace(refreshToken) == "" {
		h.clearSessionCookies(c)
		writeInvalidGrant(c)
		return
	}
	tokens, err := h.client.Renew(c.Request.Context(), refreshToken)
	if err != nil {
		h.clearSessionCookies(c)
		writeInvalidGrant(c)
		return
	}
	h.setSessionCookies(c, tokens)
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"expires_in":         tokens.ExpiresIn,
		"refresh_expires_in": tokens.RefreshExpiresIn,
	})
}

// logout clears this console's own cookies. The issuer session is a separate
// cookie on the issuer domain and is not this deployment's to end.
func (h *handoffRoutes) logout(c *gin.Context) {
	if !h.enabled() {
		writeHandoffDisabled(c)
		return
	}
	h.clearSessionCookies(c)
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusOK)
}

// setSessionCookies plants both cookies on the configured domain. An empty
// cookieDomain leaves them host-only, which is what local development wants.
func (h *handoffRoutes) setSessionCookies(c *gin.Context, tokens *handoff.RedeemWireResponse) {
	c.SetSameSite(http.SameSiteLaxMode)
	h.setCookie(c, authTokenCookieName, tokens.Token, cookieMaxAge(tokens.ExpiresIn))
	// An issuer that issued no refresh token must not leave the previous one
	// behind: it would be spent by a refresh the new session never earned.
	if refresh := strings.TrimSpace(tokens.RefreshToken); refresh != "" {
		h.setCookie(c, refreshTokenCookieName, refresh, cookieMaxAge(tokens.RefreshExpiresIn))
	} else {
		h.setCookie(c, refreshTokenCookieName, "", -1)
	}
}

// clearSessionCookies expires both cookies on the same domain and path they
// were set with; a cookie cleared on another domain stays behind.
func (h *handoffRoutes) clearSessionCookies(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	h.setCookie(c, authTokenCookieName, "", -1)
	h.setCookie(c, refreshTokenCookieName, "", -1)
}

func (h *handoffRoutes) setCookie(c *gin.Context, name, value string, maxAge int) {
	domain := ""
	secure := true
	if h.cfg != nil {
		domain = strings.TrimSpace(h.cfg.Handoff.CookieDomain)
		secure = h.cfg.Handoff.CookieSecure
	}
	c.SetCookie(name, value, maxAge, "/", domain, secure, true)
}

// cookieMaxAge clamps an issuer lifetime to the cookie attribute's range. Zero
// keeps the cookie for the browser session, which is the sane reading of an
// issuer that reported no lifetime.
func cookieMaxAge(seconds int64) int {
	if seconds <= 0 {
		return 0
	}
	if seconds > math.MaxInt32 {
		return math.MaxInt32
	}
	return int(seconds)
}

// writeInvalidGrant is the refresh route's one failure body: the console treats
// it as "sign in again", so the code is fixed rather than derived.
func writeInvalidGrant(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusUnauthorized, gin.H{"error": handoff.CodeInvalidGrant})
}

// writeHandoffFailure renders a protocol failure in the issuer's wire shape:
// the code is what the caller branches on, the description is for the log.
func writeHandoffFailure(c *gin.Context, err error) {
	body := gin.H{"error": handoff.ErrorCodeFor(err)}
	var protocol *handoff.Error
	if errors.As(err, &protocol) && strings.TrimSpace(protocol.Message) != "" {
		body["error_description"] = protocol.Message
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(handoff.HTTPStatusForError(err), body)
}

// writeHandoffDisabled answers a deployment with no issuer configured. It uses
// the package's 503 shape so the console can tell "not offered here" from
// "wrong code" and from a routing 404.
func writeHandoffDisabled(c *gin.Context) {
	writeError(c, fmt.Errorf("%w: session hand-off is not configured", service.ErrDependency))
}
