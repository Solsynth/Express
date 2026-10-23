package integrations

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"src.solsynth.dev/solsynth/distribution/internal/service"
	syncengine "src.solsynth.dev/solsynth/distribution/internal/sync"
)

type giteaTarget struct {
	client *http.Client
}

// NewGitea returns the Gitea release adapter. It performs no I/O.
func NewGitea(client *http.Client) syncengine.Target {
	return &giteaTarget{client: httpClient(client)}
}

func (t *giteaTarget) Provider() string { return syncengine.GiteaProviderID }

func (t *giteaTarget) Validate(conn syncengine.Connection) error {
	// Gitea is self-hosted, so the base URL is mandatory.
	if strings.TrimSpace(conn.BaseURL) == "" {
		return fmt.Errorf("%w: base_url is required for Gitea", service.ErrValidation)
	}
	if err := validateAbsoluteURL(conn.BaseURL); err != nil {
		return err
	}
	return requireSecretKeys(conn.Secret, []string{"token"}, nil)
}

func (t *giteaTarget) apiURL(conn syncengine.Connection) string {
	return strings.TrimRight(strings.TrimSpace(conn.BaseURL), "/") + "/api/v1"
}

func (t *giteaTarget) header(conn syncengine.Connection) (http.Header, error) {
	token := strings.TrimSpace(conn.Secret["token"])
	if token == "" {
		return nil, syncengine.Permanent(fmt.Errorf("secret requires token"))
	}
	header := http.Header{}
	header.Set("Authorization", "token "+token)
	return header, nil
}

type giteaRelease struct {
	ID        int64  `json:"id"`
	TagName   string `json:"tag_name"`
	HTMLURL   string `json:"html_url"`
	UploadURL string `json:"upload_url"`
}

func (t *giteaTarget) Publish(ctx context.Context, conn syncengine.Connection, payload syncengine.ReleasePayload) (syncengine.Result, error) {
	owner, name, err := repositoryParts(payload.Link.Repository)
	if err != nil {
		return syncengine.Result{}, syncengine.Permanent(err)
	}
	header, err := t.header(conn)
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
	base := t.apiURL(conn)
	releasePath := fmt.Sprintf("/repos/%s/%s/releases", url.PathEscape(owner), url.PathEscape(name))
	var release giteaRelease
	resp, responsePayload, err := doJSON(ctx, t.client, http.MethodPost, base+releasePath, header, body, &release)
	if err != nil {
		return syncengine.Result{}, err
	}
	switch {
	case resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices:
	case resp.StatusCode == http.StatusConflict:
		release, err = t.upsert(ctx, base, releasePath, header, payload)
		if err != nil {
			return syncengine.Result{}, err
		}
	default:
		return syncengine.Result{}, statusError(resp.StatusCode, resp.Header, responsePayload)
	}
	result := syncengine.Result{ExternalID: strconv.FormatInt(release.ID, 10), ExternalURL: release.HTMLURL}
	if payload.Asset != nil && !payload.Draft && release.UploadURL != "" {
		if err := t.uploadAsset(ctx, base, release.UploadURL, header, payload.Asset); err != nil {
			return syncengine.Result{}, err
		}
	}
	return result, nil
}

func (t *giteaTarget) upsert(ctx context.Context, base, releasePath string, header http.Header, payload syncengine.ReleasePayload) (giteaRelease, error) {
	var existing giteaRelease
	tagPath := releasePath + "/tags/" + url.PathEscape(payload.Version)
	resp, responsePayload, err := doJSON(ctx, t.client, http.MethodGet, base+tagPath, header, nil, &existing)
	if err != nil {
		return giteaRelease{}, err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return giteaRelease{}, statusError(resp.StatusCode, resp.Header, responsePayload)
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
		return giteaRelease{}, err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return giteaRelease{}, statusError(resp.StatusCode, resp.Header, responsePayload)
	}
	return existing, nil
}

func (t *giteaTarget) uploadAsset(ctx context.Context, base, uploadURL string, header http.Header, asset *syncengine.Asset) error {
	endpoint := uploadURL
	if !strings.HasPrefix(endpoint, "http") {
		endpoint = base + "/" + strings.TrimLeft(uploadURL, "/")
	}
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
	return statusError(resp.StatusCode, resp.Header, payload)
}

func (t *giteaTarget) Yank(ctx context.Context, conn syncengine.Connection, payload syncengine.ReleasePayload, previous syncengine.Result) error {
	owner, name, err := repositoryParts(payload.Link.Repository)
	if err != nil {
		return syncengine.Permanent(err)
	}
	header, err := t.header(conn)
	if err != nil {
		return err
	}
	releaseID := strings.TrimSpace(previous.ExternalID)
	if releaseID == "" {
		var existing giteaRelease
		base := t.apiURL(conn)
		tagPath := fmt.Sprintf("/repos/%s/%s/releases/tags/%s", url.PathEscape(owner), url.PathEscape(name), url.PathEscape(payload.Version))
		resp, responsePayload, err := doJSON(ctx, t.client, http.MethodGet, base+tagPath, header, nil, &existing)
		if err != nil {
			return err
		}
		if resp.StatusCode == http.StatusNotFound {
			return syncengine.Permanent(errNoRelease)
		}
		if resp.StatusCode >= http.StatusBadRequest {
			return statusError(resp.StatusCode, resp.Header, responsePayload)
		}
		releaseID = strconv.FormatInt(existing.ID, 10)
	}
	endpoint := fmt.Sprintf("%s/repos/%s/%s/releases/%s", t.apiURL(conn), url.PathEscape(owner), url.PathEscape(name), url.PathEscape(releaseID))
	resp, responsePayload, err := doJSON(ctx, t.client, http.MethodDelete, endpoint, header, nil, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return statusError(resp.StatusCode, resp.Header, responsePayload)
	}
	return nil
}
