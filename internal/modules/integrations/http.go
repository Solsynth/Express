package integrations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"src.solsynth.dev/solsynth/distribution/internal/service"
	syncengine "src.solsynth.dev/solsynth/distribution/internal/sync"
)

// maxErrorBody caps how much of a destination error body is quoted back to a
// publisher.
const maxErrorBody = 4 << 10

// requireSecretKeys rejects secrets with missing or unexpected keys so a
// typo cannot silently disable authentication.
func requireSecretKeys(secret map[string]string, required, optional []string) error {
	allowed := make(map[string]bool, len(required)+len(optional))
	for _, key := range required {
		allowed[key] = true
		if strings.TrimSpace(secret[key]) == "" {
			return fmt.Errorf("%w: secret requires %s", service.ErrValidation, key)
		}
	}
	for _, key := range optional {
		allowed[key] = true
	}
	for key := range secret {
		if !allowed[key] {
			return fmt.Errorf("%w: secret has unknown key %s", service.ErrValidation, key)
		}
	}
	return nil
}

// validateAbsoluteURL accepts an absolute http(s) URL.
func validateAbsoluteURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("%w: base_url must be an absolute HTTP(S) URL", service.ErrValidation)
	}
	return nil
}

// repositoryParts splits "owner/name".
func repositoryParts(repository string) (string, string, error) {
	parts := strings.Split(strings.TrimSpace(repository), "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", fmt.Errorf("%w: repository must be owner/name", service.ErrValidation)
	}
	return parts[0], parts[1], nil
}

// doJSON performs a JSON request and decodes a JSON response into out when it
// is non-nil.
func doJSON(ctx context.Context, client *http.Client, method, endpoint string, headers http.Header, body any, out any) (*http.Response, []byte, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, nil, fmt.Errorf("encode %s request: %w", method, err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, nil, fmt.Errorf("create %s request: %w", method, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	for key, values := range headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	resp, err := httpClient(client).Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("%s %s: %w", method, endpoint, err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resp, nil, fmt.Errorf("read %s response: %w", method, err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return resp, payload, nil
	}
	if out != nil && len(bytes.TrimSpace(payload)) > 0 {
		if err := json.Unmarshal(payload, out); err != nil {
			return resp, payload, fmt.Errorf("decode %s response: %w", method, err)
		}
	}
	return resp, payload, nil
}

func httpClient(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func errorBody(payload []byte) string {
	text := strings.TrimSpace(string(payload))
	if len(text) > maxErrorBody {
		text = text[:maxErrorBody]
	}
	return text
}

// apiMessage extracts a destination error message for a permanent failure.
func apiMessage(payload []byte) string {
	var decoded struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(payload, &decoded); err == nil {
		if message := strings.TrimSpace(decoded.Message); message != "" {
			return message
		}
		if message := strings.TrimSpace(decoded.Error); message != "" {
			return message
		}
	}
	if body := errorBody(payload); body != "" {
		return body
	}
	return "destination rejected the request"
}

// retryAfterSeconds parses a Retry-After header in seconds.
func retryAfterSeconds(header http.Header) time.Duration {
	raw := strings.TrimSpace(header.Get("Retry-After"))
	if raw == "" {
		return 0
	}
	seconds, err := time.ParseDuration(raw + "s")
	if err != nil || seconds <= 0 {
		return 0
	}
	return seconds
}

// permanentStatus reports whether a 4xx status should never be retried.
func permanentStatus(status int) bool {
	switch status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
		http.StatusNotFound, http.StatusUnprocessableEntity, http.StatusGone:
		return true
	}
	return false
}

// isRateLimit reports a throttle response.
func isRateLimit(status int) bool {
	return status == http.StatusTooManyRequests
}

// rateLimitError builds a retryable throttle error.
func rateLimitError(header http.Header) error {
	retryAfter := retryAfterSeconds(header)
	if retryAfter <= 0 {
		retryAfter = time.Minute
	}
	return &syncengine.RateLimitError{RetryAfter: retryAfter}
}

// statusError maps a destination error response to a retryable or permanent
// error.
func statusError(status int, header http.Header, payload []byte) error {
	if isRateLimit(status) {
		return rateLimitError(header)
	}
	if status >= http.StatusInternalServerError {
		return fmt.Errorf("destination returned %d: %s", status, errorBody(payload))
	}
	if permanentStatus(status) {
		return syncengine.Permanent(fmt.Errorf("destination returned %d: %s", status, apiMessage(payload)))
	}
	return fmt.Errorf("destination returned %d: %s", status, errorBody(payload))
}

// appendArtifactURL adds a download link to notes for destinations that cannot
// upload assets.
func appendArtifactURL(notes, assetURL string) string {
	assetURL = strings.TrimSpace(assetURL)
	if assetURL == "" {
		return notes
	}
	line := "Artifact: " + assetURL
	if strings.TrimSpace(notes) == "" {
		return line
	}
	return strings.TrimRight(notes, "\n") + "\n\n" + line
}

var errNoRelease = errors.New("release was never synced; nothing to yank")
