package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"src.solsynth.dev/sosys/go/pkg/handoff"

	"src.solsynth.dev/solsynth/distribution/internal/config"
	"src.solsynth.dev/solsynth/distribution/internal/service"
)

// The embedded file picker.
//
// The picker is served from another registrable domain — fs.solian.app against
// this console's dx.solsynth.dev — so when it is framed here the browser
// withholds its session cookie: a SameSite=Lax cookie is not sent in a
// cross-site iframe. The frame cannot complete a sign-in redirect either,
// because the identity host refuses to be framed.
//
// So the console asks the issuer for a single-use hand-off code bound to the
// *picker's* client and hands only that to the frame. The frame redeems it on
// its own origin for a bearer token it keeps in memory. The code is single-use
// and lives for handoffCodeTTL (60s by default), and the console never holds a
// token for the drive — only HttpOnly cookies for its own session, as before.

// pickerIssuerTimeout bounds the issue call. It sits in front of a modal the
// person is waiting on, so it may not hang on a slow issuer.
const pickerIssuerTimeout = 10 * time.Second

// pickerRoutes mounts the console's side of the picker hand-off.
type pickerRoutes struct {
	cfg *config.Config
	// issuer performs the HTTP call. Indirection so a test can answer without
	// a listener.
	issuer *http.Client
}

// registerPickerRoutes mounts the code route. It is mounted whenever the
// console is, but answers 503 until configured: a missing picker must read as
// "not offered here", not as a routing bug.
func registerPickerRoutes(engine *gin.Engine, cfg *config.Config) {
	if engine == nil {
		return
	}
	routes := &pickerRoutes{
		cfg:    cfg,
		issuer: &http.Client{Timeout: pickerIssuerTimeout},
	}
	engine.POST("/api/picker/code", routes.issueCode)
}

// enabled reports whether a cross-domain picker can be handed a code at all.
func (p *pickerRoutes) enabled() bool {
	return p != nil && p.cfg != nil && p.cfg.PickerEnabled() && p.cfg.HandoffEnabled()
}

// issueCode answers the console's own session holder with a code for the
// picker's client.
//
// The route is authenticated by the console's own cookie, which is a session
// token the issuer accepts as a bearer credential — the same token this server
// redeemed from the sign-in hand-off. The issuer hands off the *caller's*
// session, so the code is for whoever is signed in here, and the request body
// cannot name a different account.
func (p *pickerRoutes) issueCode(c *gin.Context) {
	if !p.enabled() {
		writeHandoffDisabled(c)
		return
	}
	token, ok := requestAuthToken(c)
	if !ok {
		writeError(c, service.ErrUnauthorized)
		return
	}

	frameURL := strings.TrimSpace(p.cfg.Picker.FrameURL)
	request := handoff.IssueWireRequest{
		ClientID:    strings.TrimSpace(p.cfg.Picker.ClientID),
		RedirectURI: frameURL,
	}
	// Narrowing to the frame's own host can only refuse a target, never grant
	// one: the client's registered hosts still apply underneath.
	if host := hostOf(frameURL); host != "" {
		request.AllowedHosts = []string{host}
	}

	issued, err := p.askIssuer(c.Request.Context(), token, request)
	if err != nil {
		// The issuer's own error is logged, never echoed: this route is
		// reachable by any signed-in account and the message would otherwise
		// leak whatever the issuer said about another client's configuration.
		c.Error(err) //nolint:errcheck // logging only
		writeHandoffDisabled(c)
		return
	}

	// The frame must be opened at exactly the URL the code was bound to, so
	// the server returns it rather than trusting the SPA's own build-time
	// value to agree. A mismatched frame would redeem against a target the
	// issuer never validated.
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"code":      issued.Code,
		"expiresIn": issued.ExpiresIn,
		"frameUrl":  frameURL,
	})
}

// askIssuer calls the issue endpoint with the console's token as the bearer
// credential. The SDK's TokenClient covers redeem and renew; issue is the one
// leg it does not carry, so this posts to the same wire shape.
func (p *pickerRoutes) askIssuer(
	ctx context.Context,
	token string,
	request handoff.IssueWireRequest,
) (*handoff.IssueWireResponse, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("marshal picker hand-off request: %w", err)
	}
	url := strings.TrimRight(strings.TrimSpace(p.cfg.Handoff.BaseURL), "/") + handoff.IssueEndpointPath
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build picker hand-off request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Authorization", "Bearer "+token)

	response, err := p.issuer.Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("ask the issuer for a picker code: %w", err)
	}
	defer response.Body.Close() //nolint:errcheck // reading the body releases the connection

	// A refused issue is reported as a status, not as the issuer's body: the
	// body may name another client's registration.
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read the issuer's answer: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the issuer refused a picker code with status %d", response.StatusCode)
	}

	var issued handoff.IssueWireResponse
	if err := json.Unmarshal(data, &issued); err != nil {
		return nil, fmt.Errorf("decode the issuer's answer: %w", err)
	}
	if strings.TrimSpace(issued.Code) == "" {
		return nil, fmt.Errorf("the issuer answered a picker code request with no code")
	}
	return &issued, nil
}

// hostOf is the URL's host, or "" when it has none. The frame URL has already
// been validated at config load, so a parse failure here means the value
// changed after validation, and an empty host — which drops the narrowing
// rather than failing the request — is the safe reading of that.
func hostOf(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return parsed.Host
}
