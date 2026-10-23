package integrations

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"src.solsynth.dev/solsynth/distribution/internal/service"
	syncengine "src.solsynth.dev/solsynth/distribution/internal/sync"
)

const gitlabAPI = "https://gitlab.com/api/v4"

type gitlabTarget struct {
	client *http.Client
}

// NewGitLab returns the GitLab release adapter. It performs no I/O.
func NewGitLab(client *http.Client) syncengine.Target {
	return &gitlabTarget{client: httpClient(client)}
}

func (t *gitlabTarget) Provider() string { return syncengine.GitLabProviderID }

func (t *gitlabTarget) Validate(conn syncengine.Connection) error {
	if baseURL := strings.TrimSpace(conn.BaseURL); baseURL != "" {
		if err := validateAbsoluteURL(baseURL); err != nil {
			return err
		}
	}
	return requireSecretKeys(conn.Secret, []string{"token"}, nil)
}

func (t *gitlabTarget) baseURL(conn syncengine.Connection) string {
	if base := strings.TrimRight(strings.TrimSpace(conn.BaseURL), "/"); base != "" {
		return base
	}
	return gitlabAPI
}

func (t *gitlabTarget) header(conn syncengine.Connection) (http.Header, error) {
	token := strings.TrimSpace(conn.Secret["token"])
	if token == "" {
		return nil, syncengine.Permanent(fmt.Errorf("secret requires token"))
	}
	header := http.Header{}
	header.Set("PRIVATE-TOKEN", token)
	return header, nil
}

type gitlabRelease struct {
	TagName string `json:"tag_name"`
	Links   struct {
		Self string `json:"self"`
	} `json:"_links"`
}

func (t *gitlabTarget) Publish(ctx context.Context, conn syncengine.Connection, payload syncengine.ReleasePayload) (syncengine.Result, error) {
	repository := strings.TrimSpace(payload.Link.Repository)
	if repository == "" {
		return syncengine.Result{}, syncengine.Permanent(fmt.Errorf("%w: repository is required", service.ErrValidation))
	}
	header, err := t.header(conn)
	if err != nil {
		return syncengine.Result{}, err
	}
	projectPath := "/projects/" + url.PathEscape(repository)
	// GitLab releases cannot carry uploaded assets, so the artifact download
	// link travels in the description.
	body := map[string]any{
		"name":        payload.Title,
		"tag_name":    payload.Version,
		"description": appendArtifactURL(payload.Notes, payload.AssetURL),
	}
	base := t.baseURL(conn)
	var release gitlabRelease
	resp, responsePayload, err := doJSON(ctx, t.client, http.MethodPost, base+projectPath+"/releases", header, body, &release)
	if err != nil {
		return syncengine.Result{}, err
	}
	switch {
	case resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices:
	case resp.StatusCode == http.StatusConflict:
		resp, responsePayload, err = doJSON(ctx, t.client, http.MethodPut, base+projectPath+"/releases/"+url.PathEscape(payload.Version), header, body, &release)
		if err != nil {
			return syncengine.Result{}, err
		}
		if resp.StatusCode >= http.StatusBadRequest {
			return syncengine.Result{}, statusError(resp.StatusCode, resp.Header, responsePayload)
		}
	default:
		return syncengine.Result{}, statusError(resp.StatusCode, resp.Header, responsePayload)
	}
	return syncengine.Result{ExternalID: payload.Version, ExternalURL: release.Links.Self}, nil
}

func (t *gitlabTarget) Yank(ctx context.Context, conn syncengine.Connection, payload syncengine.ReleasePayload, previous syncengine.Result) error {
	repository := strings.TrimSpace(payload.Link.Repository)
	if repository == "" {
		return syncengine.Permanent(fmt.Errorf("%w: repository is required", service.ErrValidation))
	}
	header, err := t.header(conn)
	if err != nil {
		return err
	}
	tag := strings.TrimSpace(previous.ExternalID)
	if tag == "" {
		tag = payload.Version
	}
	endpoint := t.baseURL(conn) + "/projects/" + url.PathEscape(repository) + "/releases/" + url.PathEscape(tag)
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
