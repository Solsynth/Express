package integrations

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"src.solsynth.dev/solsynth/distribution/internal/service"
	syncengine "src.solsynth.dev/solsynth/distribution/internal/sync"
)

const githubAPI = "https://api.github.com"

type githubTarget struct {
	client *http.Client
	now    func() time.Time

	mu     sync.Mutex
	tokens map[string]githubInstallToken
}

type githubInstallToken struct {
	token   string
	expires time.Time
}

// NewGitHub returns the GitHub release adapter. It performs no I/O.
func NewGitHub(client *http.Client) syncengine.Target {
	return &githubTarget{client: httpClient(client), now: time.Now, tokens: map[string]githubInstallToken{}}
}

func (t *githubTarget) Provider() string { return syncengine.GitHubProviderID }

func (t *githubTarget) Validate(conn syncengine.Connection) error {
	if baseURL := strings.TrimSpace(conn.BaseURL); baseURL != "" {
		parsed, err := url.Parse(baseURL)
		if err != nil || parsed.Host == "" || parsed.Scheme != "https" {
			return fmt.Errorf("%w: base_url must be an absolute HTTPS URL", service.ErrValidation)
		}
	}
	kind := strings.TrimSpace(conn.Secret["kind"])
	switch kind {
	case "token":
		return requireSecretKeys(conn.Secret, []string{"kind", "token"}, nil)
	case "github_app":
		if err := requireSecretKeys(conn.Secret, []string{"kind", "app_id", "installation_id", "private_key"}, nil); err != nil {
			return err
		}
		if _, err := strconv.ParseInt(strings.TrimSpace(conn.Secret["app_id"]), 10, 64); err != nil {
			return fmt.Errorf("%w: secret app_id must be numeric", service.ErrValidation)
		}
		if _, err := strconv.ParseInt(strings.TrimSpace(conn.Secret["installation_id"]), 10, 64); err != nil {
			return fmt.Errorf("%w: secret installation_id must be numeric", service.ErrValidation)
		}
		if _, err := parseRSAPrivateKey(conn.Secret["private_key"]); err != nil {
			return err
		}
		return nil
	default:
		return fmt.Errorf("%w: secret kind must be token or github_app", service.ErrValidation)
	}
}

func parseRSAPrivateKey(raw string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(raw)))
	if block == nil {
		return nil, fmt.Errorf("%w: secret private_key must be a PEM RSA key", service.ErrValidation)
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: secret private_key must be a PEM RSA key", service.ErrValidation)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("%w: secret private_key must be an RSA key", service.ErrValidation)
	}
	return key, nil
}

func (t *githubTarget) baseURL(conn syncengine.Connection) string {
	if base := strings.TrimRight(strings.TrimSpace(conn.BaseURL), "/"); base != "" {
		return base
	}
	return githubAPI
}

func (t *githubTarget) authorize(ctx context.Context, conn syncengine.Connection) (http.Header, error) {
	header := http.Header{}
	header.Set("X-GitHub-Api-Version", "2022-11-28")
	header.Set("Accept", "application/vnd.github+json")
	if strings.TrimSpace(conn.Secret["kind"]) == "github_app" {
		token, err := t.installationToken(ctx, conn)
		if err != nil {
			return nil, err
		}
		header.Set("Authorization", "Bearer "+token)
		return header, nil
	}
	token := strings.TrimSpace(conn.Secret["token"])
	if token == "" {
		return nil, syncengine.Permanent(fmt.Errorf("secret requires token"))
	}
	header.Set("Authorization", "Bearer "+token)
	return header, nil
}

// installationToken mints and caches a GitHub App installation token.
func (t *githubTarget) installationToken(ctx context.Context, conn syncengine.Connection) (string, error) {
	appID := strings.TrimSpace(conn.Secret["app_id"])
	installationID := strings.TrimSpace(conn.Secret["installation_id"])
	cacheKey := appID + "/" + installationID

	t.mu.Lock()
	cached, ok := t.tokens[cacheKey]
	t.mu.Unlock()
	if ok && cached.token != "" && t.now().Add(time.Minute).Before(cached.expires) {
		return cached.token, nil
	}

	key, err := parseRSAPrivateKey(conn.Secret["private_key"])
	if err != nil {
		return "", syncengine.Permanent(err)
	}
	now := t.now()
	jwt, err := signRS256JWT(key, appID, now.Add(-time.Minute), now.Add(9*time.Minute))
	if err != nil {
		return "", syncengine.Permanent(err)
	}
	header := http.Header{}
	header.Set("Authorization", "Bearer "+jwt)
	header.Set("Accept", "application/vnd.github+json")
	endpoint := fmt.Sprintf("%s/app/installations/%s/access_tokens", t.baseURL(conn), url.PathEscape(installationID))
	resp, payload, err := doJSON(ctx, t.client, http.MethodPost, endpoint, header, nil, nil)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return "", statusError(resp.StatusCode, resp.Header, payload)
	}
	var decoded struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil || strings.TrimSpace(decoded.Token) == "" {
		return "", fmt.Errorf("decode installation token: %w", err)
	}
	expires := decoded.ExpiresAt
	if expires.IsZero() {
		expires = now.Add(30 * time.Minute)
	}
	t.mu.Lock()
	t.tokens[cacheKey] = githubInstallToken{token: decoded.Token, expires: expires}
	t.mu.Unlock()
	return decoded.Token, nil
}

// signRS256JWT builds the GitHub App assertion.
func signRS256JWT(key *rsa.PrivateKey, issuer string, issuedAt, expiresAt time.Time) (string, error) {
	encode := func(value any) (string, error) {
		encoded, err := json.Marshal(value)
		if err != nil {
			return "", err
		}
		return base64.RawURLEncoding.EncodeToString(encoded), nil
	}
	header, err := encode(map[string]string{"alg": "RS256", "typ": "JWT"})
	if err != nil {
		return "", fmt.Errorf("encode jwt header: %w", err)
	}
	claims, err := encode(map[string]any{"iat": issuedAt.Unix(), "exp": expiresAt.Unix(), "iss": issuer})
	if err != nil {
		return "", fmt.Errorf("encode jwt claims: %w", err)
	}
	signingInput := header + "." + claims
	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("sign jwt: %w", err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

type githubRelease struct {
	ID        int64  `json:"id"`
	HTMLURL   string `json:"html_url"`
	UploadURL string `json:"upload_url"`
}

func (t *githubTarget) Publish(ctx context.Context, conn syncengine.Connection, payload syncengine.ReleasePayload) (syncengine.Result, error) {
	owner, name, err := repositoryParts(payload.Link.Repository)
	if err != nil {
		return syncengine.Result{}, syncengine.Permanent(err)
	}
	header, err := t.authorize(ctx, conn)
	if err != nil {
		return syncengine.Result{}, err
	}
	body := map[string]any{
		"tag_name":   payload.Version,
		"name":       payload.Title,
		"body":       payload.Notes,
		"draft":      payload.Draft,
		"prerelease": payload.Prerelease,
	}
	base := t.baseURL(conn)
	releasePath := fmt.Sprintf("/repos/%s/%s/releases", url.PathEscape(owner), url.PathEscape(name))
	var release githubRelease
	resp, responsePayload, err := doJSON(ctx, t.client, http.MethodPost, base+releasePath, header, body, &release)
	if err != nil {
		return syncengine.Result{}, err
	}
	switch {
	case resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices:
	case resp.StatusCode == http.StatusUnprocessableEntity:
		// The tag already exists: upsert the existing release.
		release, err = t.upsert(ctx, base, releasePath, header, payload)
		if err != nil {
			return syncengine.Result{}, err
		}
	default:
		return syncengine.Result{}, githubError(resp.StatusCode, resp.Header, responsePayload, t.now)
	}
	result := syncengine.Result{ExternalID: strconv.FormatInt(release.ID, 10), ExternalURL: release.HTMLURL}
	if payload.Asset != nil && !payload.Draft && release.UploadURL != "" {
		if err := t.uploadAsset(ctx, release.UploadURL, header, payload.Asset); err != nil {
			return syncengine.Result{}, err
		}
	}
	return result, nil
}

func (t *githubTarget) upsert(ctx context.Context, base, releasePath string, header http.Header, payload syncengine.ReleasePayload) (githubRelease, error) {
	var existing githubRelease
	tagPath := releasePath + "/tags/" + url.PathEscape(payload.Version)
	resp, responsePayload, err := doJSON(ctx, t.client, http.MethodGet, base+tagPath, header, nil, &existing)
	if err != nil {
		return githubRelease{}, err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return githubRelease{}, githubError(resp.StatusCode, resp.Header, responsePayload, t.now)
	}
	body := map[string]any{
		"name":       payload.Title,
		"body":       payload.Notes,
		"draft":      payload.Draft,
		"prerelease": payload.Prerelease,
	}
	patchPath := fmt.Sprintf("%s/%d", releasePath, existing.ID)
	resp, responsePayload, err = doJSON(ctx, t.client, http.MethodPatch, base+patchPath, header, body, &existing)
	if err != nil {
		return githubRelease{}, err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return githubRelease{}, githubError(resp.StatusCode, resp.Header, responsePayload, t.now)
	}
	return existing, nil
}

func (t *githubTarget) uploadAsset(ctx context.Context, uploadURL string, header http.Header, asset *syncengine.Asset) error {
	endpoint := strings.ReplaceAll(uploadURL, "{?name,label}", "")
	separator := "?"
	if strings.Contains(endpoint, "?") {
		separator = "&"
	}
	endpoint += separator + "name=" + url.QueryEscape(asset.Name)
	reader, err := asset.Open(ctx)
	if err != nil {
		return fmt.Errorf("open release asset: %w", err)
	}
	defer reader.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, reader)
	if err != nil {
		return fmt.Errorf("create asset upload request: %w", err)
	}
	for key, values := range header {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	if mimeType := strings.TrimSpace(asset.MimeType); mimeType != "" {
		req.Header.Set("Content-Type", mimeType)
	}
	req.ContentLength = asset.Size
	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("upload release asset: %w", err)
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		return nil
	}
	if resp.StatusCode == http.StatusUnprocessableEntity {
		// An asset with this name already exists; a re-sync is a no-op.
		return nil
	}
	return githubError(resp.StatusCode, resp.Header, payload, t.now)
}

func (t *githubTarget) Yank(ctx context.Context, conn syncengine.Connection, payload syncengine.ReleasePayload, previous syncengine.Result) error {
	owner, name, err := repositoryParts(payload.Link.Repository)
	if err != nil {
		return syncengine.Permanent(err)
	}
	header, err := t.authorize(ctx, conn)
	if err != nil {
		return err
	}
	base := t.baseURL(conn)
	releasePath := fmt.Sprintf("/repos/%s/%s/releases", url.PathEscape(owner), url.PathEscape(name))
	releaseID := strings.TrimSpace(previous.ExternalID)
	if releaseID == "" {
		var existing githubRelease
		tagPath := releasePath + "/tags/" + url.PathEscape(payload.Version)
		resp, responsePayload, err := doJSON(ctx, t.client, http.MethodGet, base+tagPath, header, nil, &existing)
		if err != nil {
			return err
		}
		if resp.StatusCode == http.StatusNotFound {
			return syncengine.Permanent(errNoRelease)
		}
		if resp.StatusCode >= http.StatusBadRequest {
			return githubError(resp.StatusCode, resp.Header, responsePayload, t.now)
		}
		releaseID = strconv.FormatInt(existing.ID, 10)
	}
	// GitHub has no delete semantics that keep the tag, so yanking returns the
	// release to draft: it leaves the public release list while the tag and
	// assets stay intact.
	resp, payload2, err := doJSON(ctx, t.client, http.MethodPatch, base+releasePath+"/"+url.PathEscape(releaseID), header, map[string]any{"draft": true}, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return githubError(resp.StatusCode, resp.Header, payload2, t.now)
	}
	return nil
}

// githubError maps a GitHub error response. A throttled response is retryable
// with the reset delay; other client errors are permanent.
func githubError(status int, header http.Header, body []byte, now func() time.Time) error {
	remaining := strings.TrimSpace(header.Get("X-RateLimit-Remaining"))
	if (status == http.StatusForbidden || status == http.StatusTooManyRequests) && remaining == "0" {
		retryAfter := retryAfterSeconds(header)
		if retryAfter <= 0 {
			retryAfter = time.Minute
		}
		if reset := strings.TrimSpace(header.Get("X-RateLimit-Reset")); reset != "" {
			if seconds, err := strconv.ParseInt(reset, 10, 64); err == nil {
				if untilReset := time.Unix(seconds, 0).Sub(now()); untilReset > retryAfter {
					retryAfter = untilReset
				}
			}
		}
		if retryAfter < time.Second {
			retryAfter = time.Second
		}
		return &syncengine.RateLimitError{RetryAfter: retryAfter}
	}
	return statusError(status, header, body)
}
