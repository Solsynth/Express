// Package integrations ships the compiled-in release destinations. Each
// adapter is a module that provides the syncTargets port, so enabling or
// disabling a destination is a config change, never a code change.
package integrations

import (
	"context"
	"net/http"
	"strings"
	"time"

	"src.solsynth.dev/solsynth/distribution/internal/module"
	syncengine "src.solsynth.dev/solsynth/distribution/internal/sync"
)

// GitHub returns the GitHub Releases adapter module.
func GitHub() module.Factory {
	return adapter(syncengine.GitHubProviderID, func(client *http.Client) syncengine.Target {
		return NewGitHub(client)
	})
}

// GitLab returns the GitLab Releases adapter module.
func GitLab() module.Factory {
	return adapter(syncengine.GitLabProviderID, func(client *http.Client) syncengine.Target {
		return NewGitLab(client)
	})
}

// Gitea returns the Gitea Releases adapter module.
func Gitea() module.Factory {
	return adapter(syncengine.GiteaProviderID, func(client *http.Client) syncengine.Target {
		return NewGitea(client)
	})
}

// Webhook returns the generic HTTP webhook adapter module.
func Webhook() module.Factory {
	return adapter(syncengine.WebhookProviderID, func(client *http.Client) syncengine.Target {
		return NewWebhook(client)
	})
}

func adapter(id string, build func(*http.Client) syncengine.Target) module.Factory {
	return module.Factory{
		ID:             id,
		Kind:           module.KindIntegration,
		DefaultEnabled: true,
		// A destination without the sync engine has no consumer, so disabling
		// sync removes the adapters too.
		Requires:     []string{syncengine.ModuleID},
		Provides:     []module.PortID{syncengine.PortSyncTargets},
		Capabilities: []string{strings.TrimPrefix(id, "app.solian.sync.")},
		New: func() module.Instance {
			return &instance{id: id, target: build(&http.Client{Timeout: 30 * time.Second})}
		},
	}
}

// instance is one constructed adapter. It only publishes its target; the sync
// engine drives every call.
type instance struct {
	id     string
	target syncengine.Target
}

func (i *instance) Configure(context.Context, *module.Host) error { return nil }

func (i *instance) Stop(context.Context) error { return nil }

func (i *instance) ProvidePorts(_ context.Context, host *module.Host) error {
	if i.target == nil || host == nil || host.Ports == nil {
		return nil
	}
	module.Provide[syncengine.Target](host.Ports, syncengine.PortSyncTargets, i.id, module.KindIntegration, i.target)
	return nil
}
