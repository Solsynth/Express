package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"src.solsynth.dev/sosys/go/pkg/handoff"

	"src.solsynth.dev/solsynth/distribution/internal/config"
	"src.solsynth.dev/solsynth/distribution/internal/database"
	"src.solsynth.dev/solsynth/distribution/internal/service"
	gen "src.solsynth.dev/sosys/go/proto"
)

// issuerStub is the hand-off issuer's HTTP side: it answers redeem and renew
// with the wire shapes TokenClient expects, so the routes are tested against
// the real client rather than a hand-written stand-in.
type issuerStub struct {
	server   *httptest.Server
	redeems  int
	renews   int
	lastCode string
	lastBody map[string]any
}

func newIssuerStub(t *testing.T) *issuerStub {
	t.Helper()
	stub := &issuerStub{}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		stub.lastBody = body
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/auth/handoff/redeem":
			stub.redeems++
			code, _ := body["code"].(string)
			stub.lastCode = code
			if code != "good-code" {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(handoff.ErrorWireResponse{
					Error:            handoff.CodeInvalidGrant,
					ErrorDescription: "The code expired.",
				})
				return
			}
			_ = json.NewEncoder(w).Encode(handoff.RedeemWireResponse{
				Token:            "sphere-token",
				RefreshToken:     "rt-one",
				ExpiresIn:        900,
				RefreshExpiresIn: 86400,
			})
		case "/api/auth/handoff/renew":
			stub.renews++
			if body["refresh_token"] != "rt-one" {
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(handoff.ErrorWireResponse{Error: handoff.CodeInvalidGrant})
				return
			}
			_ = json.NewEncoder(w).Encode(handoff.RedeemWireResponse{
				Token:            "at-two",
				RefreshToken:     "rt-two",
				ExpiresIn:        900,
				RefreshExpiresIn: 86400,
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

// handoffTestDirectory is a Sphere stand-in that accepts exactly one token, so
// a test can tell a redeemed token from a refused one.
type handoffTestDirectory struct {
	accountID string
	accepted  map[string]bool
}

func (d *handoffTestDirectory) Authenticate(ctx context.Context, token string) (string, error) {
	account, err := d.AuthenticateAccount(ctx, token)
	return account.ID, err
}

func (d *handoffTestDirectory) AuthenticateAccount(_ context.Context, token string) (service.AuthenticatedAccount, error) {
	if !d.accepted[token] {
		return service.AuthenticatedAccount{}, service.ErrUnauthorized
	}
	return service.AuthenticatedAccount{ID: d.accountID, Name: "alice", Nick: "Alice"}, nil
}

func (d *handoffTestDirectory) GetPublisher(context.Context, string) (*gen.DyPublisher, error) {
	return &gen.DyPublisher{Id: uuid.NewString(), Name: "acme"}, nil
}

func (d *handoffTestDirectory) IsPublisherMember(context.Context, string, string, gen.DyPublisherMemberRole) (bool, error) {
	return true, nil
}

func newHandoffServer(t *testing.T, issuerURL string) (*Server, *handoffTestDirectory) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:http-handoff-test-"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := db.AutoMigrate(&database.Product{}, &database.Channel{}, &database.Release{}, &database.ReleaseArtifact{}, &database.ClientCheck{}, &database.Localization{}); err != nil {
		t.Fatal(err)
	}
	directory := &handoffTestDirectory{
		accountID: uuid.NewString(),
		accepted:  map[string]bool{"sphere-token": true, "at-two": true},
	}
	releases := service.NewPublisherReleaseService(db, directory, nil, nil)
	releases.SetPermissionChecker(grantedPermissions{})

	cfg := config.Default()
	cfg.Handoff.BaseURL = issuerURL
	cfg.Handoff.ClientID = "app.solian.express"
	cfg.Handoff.ClientSecret = "shh"
	cfg.Handoff.CookieDomain = "dx.solsynth.dev"
	cfg.Handoff.CookieSecure = true
	server := New(cfg)
	RegisterPublisherRoutes(server.Engine, releases, directory, cfg)
	return server, directory
}

// sessionCookies indexes the response's Set-Cookie headers by name so a test
// can assert on the domain, path, and flags rather than on header text.
func sessionCookies(t *testing.T, response *httptest.ResponseRecorder) map[string]*http.Cookie {
	t.Helper()
	cookies := map[string]*http.Cookie{}
	for _, cookie := range response.Result().Cookies() {
		cookies[cookie.Name] = cookie
	}
	return cookies
}

func TestHandoffRedeemSetsBothCookiesOnConfiguredDomain(t *testing.T) {
	issuer := newIssuerStub(t)
	server, directory := newHandoffServer(t, issuer.server.URL)

	request := httptest.NewRequest(http.MethodPost, "/api/auth/session", strings.NewReader(`{"code":"good-code"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.Engine.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if issuer.redeems != 1 {
		t.Fatalf("redeem calls = %d, want 1", issuer.redeems)
	}
	if issuer.lastBody["client_id"] != "app.solian.express" || issuer.lastBody["client_secret"] != "shh" {
		t.Fatalf("redeem body = %v, want the configured client", issuer.lastBody)
	}

	cookies := sessionCookies(t, response)
	access := cookies[authTokenCookieName]
	if access == nil {
		t.Fatalf("no %s cookie in %v", authTokenCookieName, response.Result().Header.Values("Set-Cookie"))
	}
	if access.Value != "sphere-token" || access.MaxAge != 900 {
		t.Fatalf("access cookie = %q maxAge %d", access.Value, access.MaxAge)
	}
	refresh := cookies[refreshTokenCookieName]
	if refresh == nil {
		t.Fatal("no RefreshToken cookie")
	}
	if refresh.Value != "rt-one" || refresh.MaxAge != 86400 {
		t.Fatalf("refresh cookie = %q maxAge %d", refresh.Value, refresh.MaxAge)
	}
	for name, cookie := range cookies {
		if cookie.Domain != "dx.solsynth.dev" {
			t.Fatalf("%s domain = %q, want dx.solsynth.dev", name, cookie.Domain)
		}
		if cookie.Path != "/" || !cookie.HttpOnly || !cookie.Secure {
			t.Fatalf("%s = %+v, want path /, HttpOnly, Secure", name, cookie)
		}
		if cookie.SameSite != http.SameSiteLaxMode {
			t.Fatalf("%s SameSite = %v, want Lax", name, cookie.SameSite)
		}
	}

	var view consoleSessionView
	if err := json.Unmarshal(response.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	if view.Account.ID != directory.accountID || view.Account.Name != "alice" {
		t.Fatalf("session = %+v, want the redeemed account", view)
	}
}

func TestHandoffRedeemSurfacesIssuerRefusalWithoutCookies(t *testing.T) {
	issuer := newIssuerStub(t)
	server, _ := newHandoffServer(t, issuer.server.URL)

	request := httptest.NewRequest(http.MethodPost, "/api/auth/session", strings.NewReader(`{"code":"stale-code"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.Engine.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if body["error"] != handoff.CodeInvalidGrant {
		t.Fatalf("error = %q, want %q", body["error"], handoff.CodeInvalidGrant)
	}
	if cookies := response.Result().Cookies(); len(cookies) != 0 {
		t.Fatalf("refused redeem set cookies: %v", cookies)
	}
}

func TestHandoffRefreshWithoutCookieIsUnauthorized(t *testing.T) {
	issuer := newIssuerStub(t)
	server, _ := newHandoffServer(t, issuer.server.URL)

	request := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
	response := httptest.NewRecorder()
	server.Engine.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if body["error"] != handoff.CodeInvalidGrant {
		t.Fatalf("error = %q, want %q", body["error"], handoff.CodeInvalidGrant)
	}
	if issuer.renews != 0 {
		t.Fatalf("renew calls = %d, want 0", issuer.renews)
	}
	cookies := sessionCookies(t, response)
	for _, name := range []string{authTokenCookieName, refreshTokenCookieName} {
		cookie := cookies[name]
		if cookie == nil {
			t.Fatalf("no %s cookie in the clearing response", name)
		}
		if cookie.MaxAge >= 0 {
			t.Fatalf("%s MaxAge = %d, want an expiring cookie", name, cookie.MaxAge)
		}
		if cookie.Domain != "dx.solsynth.dev" {
			t.Fatalf("%s domain = %q, want dx.solsynth.dev", name, cookie.Domain)
		}
	}
}

func TestHandoffRefreshRenewsBothCookies(t *testing.T) {
	issuer := newIssuerStub(t)
	server, _ := newHandoffServer(t, issuer.server.URL)

	request := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
	request.AddCookie(&http.Cookie{Name: refreshTokenCookieName, Value: "rt-one"})
	response := httptest.NewRecorder()
	server.Engine.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	cookies := sessionCookies(t, response)
	if cookies[authTokenCookieName].Value != "at-two" {
		t.Fatalf("access cookie = %q", cookies[authTokenCookieName].Value)
	}
	if cookies[refreshTokenCookieName].Value != "rt-two" {
		t.Fatalf("refresh cookie = %q", cookies[refreshTokenCookieName].Value)
	}
}

func TestHandoffLogoutClearsBothCookies(t *testing.T) {
	issuer := newIssuerStub(t)
	server, _ := newHandoffServer(t, issuer.server.URL)

	response := httptest.NewRecorder()
	server.Engine.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	cookies := sessionCookies(t, response)
	if len(cookies) != 2 {
		t.Fatalf("cookies = %v, want both cleared", cookies)
	}
	for _, name := range []string{authTokenCookieName, refreshTokenCookieName} {
		if cookies[name].MaxAge >= 0 {
			t.Fatalf("%s MaxAge = %d, want an expiring cookie", name, cookies[name].MaxAge)
		}
	}
}

func TestHandoffRoutesAnswerServiceUnavailableWhenUnconfigured(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:http-handoff-disabled-"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := db.AutoMigrate(&database.Product{}, &database.Channel{}, &database.Release{}, &database.ReleaseArtifact{}, &database.ClientCheck{}, &database.Localization{}); err != nil {
		t.Fatal(err)
	}
	directory := &handoffTestDirectory{accountID: uuid.NewString(), accepted: map[string]bool{}}
	releases := service.NewPublisherReleaseService(db, directory, nil, nil)
	cfg := config.Default()
	server := New(cfg)
	RegisterPublisherRoutes(server.Engine, releases, directory, cfg)

	for _, path := range []string{"/api/auth/session", "/api/auth/refresh", "/api/auth/logout"} {
		response := httptest.NewRecorder()
		server.Engine.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"code":"x"}`)))
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s status = %d, want 503", path, response.Code)
		}
		var body map[string]string
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s decode: %v", path, err)
		}
		if body["error"] == "" {
			t.Fatalf("%s body = %s, want an error message", path, response.Body.String())
		}
	}
}
