package developapi

import (
	"fmt"
	"strings"
)

// presetScopes never gate a permission node: they are identity presets rather
// than capability grants. Mirrors PermissionScopeGate in the shared library.
var presetScopes = map[string]bool{
	"openid":  true,
	"profile": true,
	"email":   true,
	"*":       true,
}

// isOAuthSession reports whether a session's permissions are narrowed by its
// scopes. Only OAuth sessions carry scope gating.
func isOAuthSession(sessionType string) bool {
	return strings.EqualFold(strings.TrimSpace(sessionType), "oauth")
}

// hasFullScope reports whether an OAuth session carries the wildcard scope.
func hasFullScope(scopes []string) bool {
	for _, scope := range scopes {
		if strings.TrimSpace(scope) == "*" {
			return true
		}
	}
	return false
}

// scopeAllows reports whether an OAuth session's scopes cover a permission
// node. A scope grants itself, its "prefix.*" subtree, or everything.
func scopeAllows(scopes []string, permissionKey string) bool {
	permissionKey = strings.TrimSpace(permissionKey)
	if permissionKey == "" {
		return false
	}
	for _, raw := range scopes {
		scope := strings.TrimSpace(raw)
		if scope == "" || presetScopes[scope] {
			continue
		}
		if strings.EqualFold(scope, permissionKey) {
			return true
		}
		if strings.HasSuffix(scope, ".*") && strings.HasPrefix(strings.ToLower(permissionKey), strings.ToLower(scope[:len(scope)-1])) {
			return true
		}
	}
	return false
}

// errorf wraps a service sentinel with a formatted message, keeping
// errors.Is working for the HTTP status mapping.
func errorf(sentinel error, format string, args ...any) error {
	return fmt.Errorf("%w: %s", sentinel, fmt.Sprintf(format, args...))
}
