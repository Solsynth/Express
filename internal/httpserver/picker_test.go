package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"src.solsynth.dev/solsynth/distribution/internal/config"
)

// The picker code route hands the SPA a single-use code bound to the picker's
// own client. What must never happen is the console receiving a token for the
// drive: the whole design keeps that inside the frame, where it can be held in
// memory rather than in a cookie the browser withholds from a cross-site
// iframe.
//
// The route is registered on the engine alone, with no database and no release
// service, so these tests need no fixtures beyond a config and a stub issuer.

type pickerIssuer struct {
	// code is what a successful issue returns.
	code string
	// status is the answer when the issuer refuses.
	status int
	// lastRequest and lastBody record the most recent issue call so a test can
	// assert on what the console actually sent.
	lastRequest *http.Request
	lastBody    string
}

func (i *pickerIssuer) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 0)
		if r.Body != nil {
			buf := make([]byte, 1<<16)
			n, _ := r.Body.Read(buf)
			body = buf[:n]
		}
		i.lastRequest = r
		i.lastBody = string(body)

		if i.status != 0 {
			w.WriteHeader(i.status)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(
			`{"code":"` + i.code + `","redirect_uri":"https://fs.solian.app/picker","expires_in":60}`,
		))
	}
}

// newPickerServer mounts the console with a picker configured against issuer.
func newPickerServer(t *testing.T, issuer *httptest.Server, configure func(*config.Config)) *Server {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg := config.Default()
	cfg.Handoff.BaseURL = issuer.URL
	cfg.Handoff.ClientID = "app.solian.express"
	if configure != nil {
		configure(cfg)
	}
	// New() mounts no routes; the publisher route set does, and that needs a
	// database this route does not. Mount the picker route on its own: it is
	// registered on the engine alone, with no release service behind it.
	server := New(cfg)
	registerPickerRoutes(server.Engine, cfg)
	return server
}

// signedInRequest carries the console's own session cookie.
func signedInRequest() *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/api/picker/code", nil)
	request.AddCookie(&http.Cookie{Name: "AuthToken", Value: "console-session-token"})
	return request
}

func decodeBody(t *testing.T, raw string) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	return body
}

// A deployment that never configured a picker must answer 503 rather than 404,
// and must never reach the issuer.
func TestPickerCodeRouteDisabledWithoutClient(t *testing.T) {
	issuer := &pickerIssuer{code: "should-not-be-issued"}
	upstream := httptest.NewServer(issuer.handler())
	defer upstream.Close()

	server := newPickerServer(t, upstream, func(cfg *config.Config) {
		cfg.Picker.ClientID = ""
		cfg.Picker.FrameURL = "https://fs.solian.app/picker"
	})

	response := httptest.NewRecorder()
	server.Engine.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/picker/code", nil))

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.Code)
	}
	if issuer.lastRequest != nil {
		t.Fatal("a disabled picker must not call the issuer")
	}
}

// A console with no session of its own must be refused before any issuer call.
func TestPickerCodeRouteRequiresSession(t *testing.T) {
	issuer := &pickerIssuer{code: "should-not-be-issued"}
	upstream := httptest.NewServer(issuer.handler())
	defer upstream.Close()

	server := newPickerServer(t, upstream, func(cfg *config.Config) {
		cfg.Picker.ClientID = "app.solian.snfs"
		cfg.Picker.FrameURL = "https://fs.solian.app/picker"
	})

	response := httptest.NewRecorder()
	server.Engine.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/picker/code", nil))

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
	if issuer.lastRequest != nil {
		t.Fatal("an unauthenticated caller must not reach the issuer")
	}
}

// The answer is a code and the frame it was bound to. It must carry no token:
// a token in this body would put the drive's session in the page's JavaScript,
// which is the exposure the hand-off exists to avoid.
func TestPickerCodeRouteReturnsCodeNotToken(t *testing.T) {
	issuer := &pickerIssuer{code: "single-use-" + uuid.NewString()}
	upstream := httptest.NewServer(issuer.handler())
	defer upstream.Close()

	server := newPickerServer(t, upstream, func(cfg *config.Config) {
		cfg.Picker.ClientID = "app.solian.snfs"
		cfg.Picker.FrameURL = "https://fs.solian.app/picker"
	})

	response := httptest.NewRecorder()
	server.Engine.ServeHTTP(response, signedInRequest())

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", response.Code, response.Body.String())
	}

	raw := response.Body.String()
	body := decodeBody(t, raw)
	if body["code"] != issuer.code {
		t.Fatalf("code = %v, want %s", body["code"], issuer.code)
	}
	if body["frameUrl"] != "https://fs.solian.app/picker" {
		t.Fatalf("frameUrl = %v", body["frameUrl"])
	}
	if _, ok := body["expiresIn"]; !ok {
		t.Fatalf("expiresIn is missing from %s", raw)
	}

	// No key may carry a token, however it is named.
	for _, key := range []string{"token", "access_token", "accessToken", "refresh_token", "refreshToken"} {
		if _, ok := body[key]; ok {
			t.Fatalf("the picker code route must not hand out a token: %s is present in %s", key, raw)
		}
	}
	if strings.Contains(raw, "console-session-token") {
		t.Fatalf("the console's own session token leaked into %s", raw)
	}
	if cached := response.Header().Get("Cache-Control"); cached != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store: a single-use code must not be cached", cached)
	}
}

// The code is issued for the *picker's* client, not this console's, and the
// console's own session token is what authenticates the issue call.
func TestPickerCodeRouteIssuesForPickerClient(t *testing.T) {
	issuer := &pickerIssuer{code: "abc"}
	upstream := httptest.NewServer(issuer.handler())
	defer upstream.Close()

	server := newPickerServer(t, upstream, func(cfg *config.Config) {
		cfg.Picker.ClientID = "app.solian.snfs"
		cfg.Picker.FrameURL = "https://fs.solian.app/picker"
	})

	server.Engine.ServeHTTP(httptest.NewRecorder(), signedInRequest())

	if issuer.lastRequest == nil {
		t.Fatal("the issuer must be called")
	}
	if got := issuer.lastRequest.Header.Get("Authorization"); got != "Bearer console-session-token" {
		t.Fatalf("Authorization = %q, want the console's own session token", got)
	}
	if !strings.Contains(issuer.lastBody, `"client_id":"app.solian.snfs"`) {
		t.Fatalf("the code must be issued for the picker's client, body = %s", issuer.lastBody)
	}
	if strings.Contains(issuer.lastBody, "app.solian.express") {
		t.Fatalf("the console's own client id must not appear in the issue request: %s", issuer.lastBody)
	}
	if !strings.Contains(issuer.lastBody, `"redirect_uri":"https://fs.solian.app/picker"`) {
		t.Fatalf("the redirect target must be the frame URL, body = %s", issuer.lastBody)
	}
}

// A refused issue reports the failure without echoing what the issuer said.
// The issuer's message can name another client's registration, and this route
// is reachable by any signed-in account.
func TestPickerCodeRouteHidesIssuerError(t *testing.T) {
	issuer := &pickerIssuer{status: http.StatusBadRequest}
	upstream := httptest.NewServer(issuer.handler())
	defer upstream.Close()

	server := newPickerServer(t, upstream, func(cfg *config.Config) {
		cfg.Picker.ClientID = "app.solian.snfs"
		cfg.Picker.FrameURL = "https://fs.solian.app/picker"
	})

	response := httptest.NewRecorder()
	server.Engine.ServeHTTP(response, signedInRequest())

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.Code)
	}
	if strings.Contains(response.Body.String(), "invalid_grant") {
		t.Fatalf("the issuer's own error text must not reach the console: %s", response.Body.String())
	}
}
