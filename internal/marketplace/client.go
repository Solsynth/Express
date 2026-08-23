// Package marketplace provides a small HTTP client for the DistributionCenter marketplace API.
package marketplace

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Client calls the public and publisher-authenticated marketplace endpoints.
type Client struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

// NewClient validates baseURL and returns a client using http.DefaultClient.
func NewClient(baseURL, token string) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("base URL must be an absolute HTTP(S) URL without query or fragment")
	}
	if strings.TrimRight(parsed.Path, "/") == "/api" || strings.HasSuffix(strings.TrimRight(parsed.Path, "/"), "/api") {
		return nil, fmt.Errorf("base URL must not include the /api path")
	}
	return &Client{BaseURL: baseURL, Token: strings.TrimSpace(token), HTTPClient: http.DefaultClient}, nil
}

// APIError contains the response returned by a non-2xx API request.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	if strings.TrimSpace(e.Body) == "" {
		return fmt.Sprintf("marketplace API returned %s", http.StatusText(e.StatusCode))
	}
	return fmt.Sprintf("marketplace API returned %s: %s", http.StatusText(e.StatusCode), strings.TrimSpace(e.Body))
}

// Do executes an API request and returns its response body. A nil body means
// that the request has no body; a 204 response returns an empty body.
func (c *Client) Do(ctx context.Context, method, endpoint string, query url.Values, body []byte) (json.RawMessage, error) {
	if c == nil {
		return nil, errors.New("marketplace client is nil")
	}
	requestURL := c.BaseURL + "/" + strings.TrimLeft(endpoint, "/")
	parsed, err := url.Parse(requestURL)
	if err != nil {
		return nil, fmt.Errorf("build marketplace URL: %w", err)
	}
	if query != nil {
		parsed.RawQuery = query.Encode()
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, parsed.String(), reader)
	if err != nil {
		return nil, fmt.Errorf("create marketplace request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("marketplace request: %w", err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read marketplace response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, &APIError{StatusCode: resp.StatusCode, Body: string(responseBody)}
	}
	if len(bytes.TrimSpace(responseBody)) == 0 {
		return nil, nil
	}
	return json.RawMessage(responseBody), nil
}

func (c *Client) get(ctx context.Context, endpoint string, query url.Values) (json.RawMessage, error) {
	return c.Do(ctx, http.MethodGet, endpoint, query, nil)
}

func (c *Client) post(ctx context.Context, endpoint string, body []byte) (json.RawMessage, error) {
	return c.Do(ctx, http.MethodPost, endpoint, nil, body)
}

func (c *Client) put(ctx context.Context, endpoint string, body []byte) (json.RawMessage, error) {
	return c.Do(ctx, http.MethodPut, endpoint, nil, body)
}

func (c *Client) delete(ctx context.Context, endpoint string) (json.RawMessage, error) {
	return c.Do(ctx, http.MethodDelete, endpoint, nil, nil)
}

func segment(value string) string { return url.PathEscape(strings.TrimSpace(value)) }

// ListMarketplaceApps lists public marketplace products.
func (c *Client) ListMarketplaceApps(ctx context.Context, query url.Values) (json.RawMessage, error) {
	return c.get(ctx, "/api/marketplace/apps", query)
}

// ListPublisherApps lists a publisher's public products.
func (c *Client) ListPublisherApps(ctx context.Context, publisher string) (json.RawMessage, error) {
	return c.get(ctx, "/api/publishers/"+segment(publisher)+"/apps", nil)
}

func (c *Client) GetProduct(ctx context.Context, product string) (json.RawMessage, error) {
	return c.get(ctx, "/api/products/"+segment(product), nil)
}

func (c *Client) CreateProduct(ctx context.Context, publisher string, body []byte) (json.RawMessage, error) {
	return c.post(ctx, "/api/publishers/"+segment(publisher)+"/products", body)
}

func (c *Client) UpdateProduct(ctx context.Context, product string, body []byte) (json.RawMessage, error) {
	return c.put(ctx, "/api/products/"+segment(product), body)
}

func (c *Client) DeleteProduct(ctx context.Context, product string) error {
	_, err := c.delete(ctx, "/api/products/"+segment(product))
	return err
}

func (c *Client) ListChannels(ctx context.Context, product string) (json.RawMessage, error) {
	return c.get(ctx, "/api/products/"+segment(product)+"/channels", nil)
}

func (c *Client) CreateChannel(ctx context.Context, product string, body []byte) (json.RawMessage, error) {
	return c.post(ctx, "/api/products/"+segment(product)+"/channels", body)
}

func (c *Client) UpdateChannel(ctx context.Context, product, channel string, body []byte) (json.RawMessage, error) {
	return c.put(ctx, "/api/products/"+segment(product)+"/channels/"+segment(channel), body)
}

func (c *Client) DeleteChannel(ctx context.Context, product, channel string) error {
	_, err := c.delete(ctx, "/api/products/"+segment(product)+"/channels/"+segment(channel))
	return err
}

func (c *Client) ListReleases(ctx context.Context, product string, managed bool, query url.Values) (json.RawMessage, error) {
	endpoint := "/api/products/" + segment(product) + "/releases"
	if managed {
		endpoint += "/manage"
	}
	return c.get(ctx, endpoint, query)
}

func (c *Client) CreateRelease(ctx context.Context, product string, body []byte) (json.RawMessage, error) {
	return c.post(ctx, "/api/products/"+segment(product)+"/releases", body)
}

func (c *Client) UpdateRelease(ctx context.Context, product, release string, body []byte) (json.RawMessage, error) {
	return c.put(ctx, "/api/products/"+segment(product)+"/releases/"+segment(release), body)
}

func (c *Client) DeleteRelease(ctx context.Context, product, release string) error {
	_, err := c.delete(ctx, "/api/products/"+segment(product)+"/releases/"+segment(release))
	return err
}

func (c *Client) PublishRelease(ctx context.Context, product, release string) (json.RawMessage, error) {
	return c.post(ctx, "/api/products/"+segment(product)+"/releases/"+segment(release)+"/publish", nil)
}

func (c *Client) YankRelease(ctx context.Context, product, release string) (json.RawMessage, error) {
	return c.post(ctx, "/api/products/"+segment(product)+"/releases/"+segment(release)+"/yank", nil)
}

func (c *Client) AttachArtifact(ctx context.Context, product, release string, body []byte) (json.RawMessage, error) {
	return c.post(ctx, "/api/products/"+segment(product)+"/releases/"+segment(release)+"/artifacts", body)
}

// UpdateCheckRequest is accepted by the POST update-check endpoint.
type UpdateCheckRequest struct {
	Version        string `json:"version"`
	Channel        string `json:"channel"`
	OS             string `json:"os"`
	Architecture   string `json:"architecture"`
	InstallationID string `json:"installation_id,omitempty"`
	OSVersion      string `json:"os_version,omitempty"`
	ClientVersion  string `json:"client_version,omitempty"`
	Locale         string `json:"locale,omitempty"`
}

func (c *Client) CheckUpdate(ctx context.Context, product string, request UpdateCheckRequest) (json.RawMessage, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("encode update check: %w", err)
	}
	return c.post(ctx, "/api/products/"+segment(product)+"/update/check", body)
}

func (c *Client) ResolveUpdate(ctx context.Context, product string, query url.Values) (json.RawMessage, error) {
	return c.get(ctx, "/api/products/"+segment(product)+"/update", query)
}

func (c *Client) Metrics(ctx context.Context, product string, query url.Values) (json.RawMessage, error) {
	return c.get(ctx, "/api/products/"+segment(product)+"/metrics", query)
}

func (c *Client) CreateUploadKey(ctx context.Context, product string, body []byte) (json.RawMessage, error) {
	return c.post(ctx, "/api/products/"+segment(product)+"/upload-api-keys", body)
}

func (c *Client) ListUploadKeys(ctx context.Context, product string) (json.RawMessage, error) {
	return c.get(ctx, "/api/products/"+segment(product)+"/upload-api-keys", nil)
}

func (c *Client) RevokeUploadKey(ctx context.Context, product, key string) error {
	_, err := c.delete(ctx, "/api/products/"+segment(product)+"/upload-api-keys/"+segment(key))
	return err
}

// UploadPreparation is returned by the upload-url endpoint.
type UploadPreparation struct {
	ObjectKey string `json:"object_key"`
	UploadURL string `json:"upload_url"`
	ReleaseID string `json:"release_id"`
	Version   string `json:"version"`
}

func (c *Client) PrepareUpload(ctx context.Context, product string, request any) (UploadPreparation, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return UploadPreparation{}, fmt.Errorf("encode upload preparation: %w", err)
	}
	raw, err := c.post(ctx, "/api/products/"+segment(product)+"/artifacts/upload-url", body)
	if err != nil {
		return UploadPreparation{}, err
	}
	var preparation UploadPreparation
	if err := json.Unmarshal(raw, &preparation); err != nil {
		return UploadPreparation{}, fmt.Errorf("decode upload preparation: %w", err)
	}
	if preparation.UploadURL == "" || preparation.ReleaseID == "" || preparation.ObjectKey == "" {
		return UploadPreparation{}, errors.New("upload preparation response is missing upload_url, release_id, or object_key")
	}
	return preparation, nil
}

// PutArtifact uploads file bytes to the presigned URL returned by PrepareUpload.
func (c *Client) PutArtifact(ctx context.Context, uploadURL, fileName, mimeType, sha256Digest string, file io.Reader, size int64) error {
	_ = fileName // The filename is part of upload preparation, not a signed PUT header.
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, file)
	if err != nil {
		return fmt.Errorf("create artifact upload request: %w", err)
	}
	req.Header.Set("Content-Type", mimeType)
	req.Header.Set("x-amz-meta-sha256", sha256Digest)
	if size >= 0 {
		req.ContentLength = size
	}
	// Only headers documented by the API are sent to the presigned URL.
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("upload artifact: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
		return &APIError{StatusCode: resp.StatusCode, Body: string(responseBody)}
	}
	return nil
}

// SHA256File returns the lowercase digest and size of a file.
func SHA256File(file *os.File) (digest string, size int64, err error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", 0, fmt.Errorf("seek artifact: %w", err)
	}
	hasher := sha256.New()
	size, err = io.Copy(hasher, file)
	if err != nil {
		return "", 0, fmt.Errorf("hash artifact: %w", err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", 0, fmt.Errorf("rewind artifact: %w", err)
	}
	return hex.EncodeToString(hasher.Sum(nil)), size, nil
}

// ContextWithTimeout applies a timeout when duration is positive.
func ContextWithTimeout(parent context.Context, duration time.Duration) (context.Context, context.CancelFunc) {
	if duration <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, duration)
}
