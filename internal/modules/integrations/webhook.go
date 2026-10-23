package integrations

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"src.solsynth.dev/solsynth/distribution/internal/service"
	syncengine "src.solsynth.dev/solsynth/distribution/internal/sync"
)

type webhookTarget struct {
	client *http.Client
}

// NewWebhook returns the generic webhook adapter. It is the extension point
// for destinations that speak HTTP (Discord, Slack, an internal mirror) and
// needs no new Go code.
func NewWebhook(client *http.Client) syncengine.Target {
	return &webhookTarget{client: httpClient(client)}
}

func (t *webhookTarget) Provider() string { return syncengine.WebhookProviderID }

func (t *webhookTarget) Validate(conn syncengine.Connection) error {
	if err := requireSecretKeys(conn.Secret, []string{"url"}, []string{"secret"}); err != nil {
		return err
	}
	raw := strings.TrimSpace(conn.Secret["url"])
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("%w: secret url must be an absolute HTTP(S) URL", service.ErrValidation)
	}
	return nil
}

// envelope is the webhook body. It carries everything a receiver needs to
// mirror or announce a release without calling back into Express.
type envelope struct {
	Event    string        `json:"event"`
	Delivery string        `json:"delivery,omitempty"`
	Provider string        `json:"provider"`
	Product  webhookRef    `json:"product"`
	Release  webhookDetail `json:"release"`
}

type webhookRef struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
}

type webhookDetail struct {
	ID          string   `json:"id"`
	Version     string   `json:"version"`
	Title       string   `json:"title"`
	Notes       string   `json:"notes"`
	Channels    []string `json:"channels"`
	Prerelease  bool     `json:"prerelease"`
	Status      string   `json:"status,omitempty"`
	DownloadURL string   `json:"download_url"`
	// Artifacts is structured rather than pre-rendered: a receiver composes
	// its own message, so it gets the fields, not a markdown table.
	Artifacts []webhookArtifact `json:"artifacts"`
}

type webhookArtifact struct {
	Name         string `json:"name"`
	Platform     string `json:"platform"`
	Architecture string `json:"architecture"`
	Size         int64  `json:"size"`
	URL          string `json:"url"`
}

func (t *webhookTarget) Publish(ctx context.Context, conn syncengine.Connection, payload syncengine.ReleasePayload) (syncengine.Result, error) {
	return t.send(ctx, conn, payload, "release.published", "")
}

func (t *webhookTarget) Yank(ctx context.Context, conn syncengine.Connection, payload syncengine.ReleasePayload, _ syncengine.Result) error {
	_, err := t.send(ctx, conn, payload, "release.yanked", "yanked")
	return err
}

func (t *webhookTarget) send(ctx context.Context, conn syncengine.Connection, payload syncengine.ReleasePayload, event, status string) (syncengine.Result, error) {
	endpoint := strings.TrimSpace(conn.Secret["url"])
	if endpoint == "" {
		return syncengine.Result{}, syncengine.Permanent(fmt.Errorf("secret requires url"))
	}
	body, err := json.Marshal(envelope{
		Event:    event,
		Delivery: payload.ReleaseID,
		Provider: "webhook",
		Product:  webhookRef{ID: payload.ProductID, Slug: payload.ProductSlug, Name: payload.ProductName},
		Release: webhookDetail{
			ID:          payload.ReleaseID,
			Version:     payload.Version,
			Title:       payload.Title,
			Notes:       payload.Notes,
			Channels:    payload.Channels,
			Prerelease:  payload.Prerelease,
			Status:      status,
			DownloadURL: payload.AssetURL,
			Artifacts:   webhookArtifacts(payload.Artifacts),
		},
	})
	if err != nil {
		return syncengine.Result{}, fmt.Errorf("encode webhook body: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return syncengine.Result{}, fmt.Errorf("create webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Express-Event", event)
	req.Header.Set("X-Express-Delivery", payload.ReleaseID)
	if secret := strings.TrimSpace(conn.Secret["secret"]); secret != "" {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		req.Header.Set("X-Express-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return syncengine.Result{}, fmt.Errorf("deliver webhook: %w", err)
	}
	defer resp.Body.Close()
	responsePayload, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	switch {
	case resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices:
	case resp.StatusCode == http.StatusGone:
		// The endpoint retired: treat as delivered so the job stops retrying.
	case resp.StatusCode == http.StatusTooManyRequests:
		return syncengine.Result{}, rateLimitError(resp.Header)
	case resp.StatusCode == http.StatusRequestTimeout:
		return syncengine.Result{}, fmt.Errorf("webhook timed out")
	case resp.StatusCode >= http.StatusInternalServerError:
		return syncengine.Result{}, fmt.Errorf("webhook returned %d: %s", resp.StatusCode, errorBody(responsePayload))
	default:
		return syncengine.Result{}, syncengine.Permanent(fmt.Errorf("webhook returned %d: %s", resp.StatusCode, errorBody(responsePayload)))
	}
	return syncengine.Result{ExternalURL: endpoint}, nil
}

// webhookArtifacts flattens the payload artifacts for the envelope. The slice
// is always non-nil so a receiver can range over it without a nil check.
func webhookArtifacts(artifacts []syncengine.ArtifactLink) []webhookArtifact {
	out := make([]webhookArtifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		out = append(out, webhookArtifact{
			Name:         artifact.Name,
			Platform:     artifact.Platform,
			Architecture: artifact.Architecture,
			Size:         artifact.Size,
			URL:          artifact.URL,
		})
	}
	return out
}
