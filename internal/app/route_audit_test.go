package app

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
)

// TestEveryPortedEndpointIsMounted walks the full Develop surface. A route
// that is missing answers 404; a mounted one answers anything else (401 for
// authenticated surfaces, 200 for public catalogs).
func TestEveryPortedEndpointIsMounted(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	app, err := New(gatingConfig(t), log)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = app.Stop(context.Background()) })

	id := "11111111-1111-1111-1111-111111111111"
	type route struct{ method, path string }
	routes := []route{
		// DeveloperController
		{"GET", "/api/developers"},
		{"GET", "/api/developers/solsynth"},
		{"GET", "/api/developers/solsynth/stats"},
		{"POST", "/api/developers/solsynth/enroll"},
		// DevelopQuotaController
		{"GET", "/api/develop/quota"},
		// DevProjectController
		{"GET", "/api/private/projects"},
		{"GET", "/api/private/projects/" + id},
		{"POST", "/api/private/projects"},
		{"PUT", "/api/private/projects/" + id},
		{"DELETE", "/api/private/projects/" + id},
		// CustomAppController
		{"GET", "/api/private/apps"},
		{"GET", "/api/private/apps/" + id},
		{"POST", "/api/private/apps"},
		{"PATCH", "/api/private/apps/" + id},
		{"DELETE", "/api/private/apps/" + id},
		{"POST", "/api/private/apps/" + id + "/orders"},
		{"GET", "/api/private/apps/" + id + "/board"},
		{"POST", "/api/private/apps/" + id + "/board"},
		{"PUT", "/api/private/apps/" + id + "/board/steps"},
		{"DELETE", "/api/private/apps/" + id + "/board/steps"},
		{"GET", "/api/private/apps/" + id + "/board/widgets"},
		{"POST", "/api/private/apps/" + id + "/board/payload"},
		{"GET", "/api/private/apps/" + id + "/secrets"},
		{"POST", "/api/private/apps/" + id + "/secrets"},
		{"GET", "/api/private/apps/" + id + "/secrets/" + id},
		{"DELETE", "/api/private/apps/" + id + "/secrets/" + id},
		{"POST", "/api/private/apps/" + id + "/secrets/" + id + "/rotate"},
		// AppProductController
		{"GET", "/api/private/apps/" + id + "/products"},
		{"GET", "/api/private/apps/" + id + "/products/" + id},
		{"POST", "/api/private/apps/" + id + "/products"},
		{"PATCH", "/api/private/apps/" + id + "/products/" + id},
		{"DELETE", "/api/private/apps/" + id + "/products/" + id},
		// BotAccountController
		{"GET", "/api/private/bots"},
		{"GET", "/api/private/bots/" + id},
		{"POST", "/api/private/bots"},
		{"PATCH", "/api/private/bots/" + id},
		{"DELETE", "/api/private/bots/" + id},
		{"GET", "/api/private/bots/" + id + "/keys"},
		{"GET", "/api/private/bots/" + id + "/keys/" + id},
		{"POST", "/api/private/bots/" + id + "/keys"},
		{"POST", "/api/private/bots/" + id + "/keys/" + id + "/rotate"},
		{"DELETE", "/api/private/bots/" + id + "/keys/" + id},
		{"GET", "/api/private/bots/" + id + "/chat"},
		{"PUT", "/api/private/bots/" + id + "/chat"},
		{"POST", "/api/private/bots/" + id + "/chat/manifest"},
		// BotAccountPublicController
		{"GET", "/api/bots/" + id},
		{"GET", "/api/bots/" + id + "/developer"},
		{"GET", "/api/bots/" + id + "/chat"},
		{"GET", "/api/bots/" + id + "/commands"},
		// MiniAppController
		{"GET", "/api/private/miniapps"},
		{"GET", "/api/private/miniapps/" + id},
		{"POST", "/api/private/miniapps"},
		{"PATCH", "/api/private/miniapps/" + id},
		{"DELETE", "/api/private/miniapps/" + id},
		{"POST", "/api/private/miniapps/" + id + "/package"},
		// MiniAppPublicController
		{"GET", "/api/miniapps"},
		{"GET", "/api/miniapps/counter"},
		{"GET", "/api/miniapps/counter/package"},
		// CustomAppPublicController (renamed from /api/apps)
		{"GET", "/api/custom-apps"},
		{"GET", "/api/custom-apps/island"},
		{"GET", "/api/custom-apps/island/products"},
		{"GET", "/api/custom-apps/island/products/pro"},
		// BoardPublicController
		{"GET", "/api/custom-apps/board"},
	}

	// Check the router table rather than response codes: a handler that
	// correctly answers 404 for a missing record is still mounted.
	registered := make(map[string]bool, len(app.HTTPServer.Engine.Routes()))
	for _, entry := range app.HTTPServer.Engine.Routes() {
		registered[entry.Method+" "+entry.Path] = true
	}
	normalize := func(path string) string {
		// Collapse the concrete id/slug in the probe path back to the route
		// parameter gin registered.
		return path
	}
	missing := []string{}
	for _, r := range routes {
		if registered[r.method+" "+normalize(r.path)] {
			continue
		}
		// Fall back to matching a parameterized route.
		if matchParamRoute(registered, r.method, r.path) {
			continue
		}
		missing = append(missing, r.method+" "+r.path)
	}
	if len(missing) > 0 {
		t.Fatalf("%d ported endpoints are not mounted:\n%s", len(missing), strings.Join(missing, "\n"))
	}
	t.Logf("all %d ported HTTP endpoints are mounted", len(routes))
}

// matchParamRoute reports whether a concrete path matches a registered
// parameterized route.
func matchParamRoute(registered map[string]bool, method, path string) bool {
	for key := range registered {
		parts := strings.SplitN(key, " ", 2)
		if len(parts) != 2 || parts[0] != method {
			continue
		}
		if routeMatches(parts[1], path) {
			return true
		}
	}
	return false
}

// routeMatches compares a gin route pattern against a concrete path.
func routeMatches(pattern, path string) bool {
	patternParts := strings.Split(strings.Trim(pattern, "/"), "/")
	pathParts := strings.Split(strings.Trim(path, "/"), "/")
	if len(patternParts) != len(pathParts) {
		return false
	}
	for index, part := range patternParts {
		if strings.HasPrefix(part, ":") || strings.HasPrefix(part, "*") {
			continue
		}
		if part != pathParts[index] {
			return false
		}
	}
	return true
}
