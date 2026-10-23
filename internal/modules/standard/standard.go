// Package standard lists the modules compiled into the DistributionCenter
// binary. Deployments enable or disable them through [modules].
package standard

import (
	"src.solsynth.dev/solsynth/distribution/internal/module"
	developmodule "src.solsynth.dev/solsynth/distribution/internal/modules/develop"
	"src.solsynth.dev/solsynth/distribution/internal/modules/integrations"
	syncmodule "src.solsynth.dev/solsynth/distribution/internal/modules/sync"
)

// Modules returns every compiled-in module factory.
func Modules() []module.Factory {
	factories := []module.Factory{
		syncmodule.Factory(),
		integrations.GitHub(),
		integrations.GitLab(),
		integrations.Gitea(),
		integrations.Webhook(),
	}
	return append(factories, developmodule.Factories()...)
}
