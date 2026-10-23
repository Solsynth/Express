// Package core holds the service bundle modules receive through module.Host.
// It deliberately imports nothing from internal/module or any module package
// so the dependency direction stays module -> core.
package core

import (
	"log/slog"

	"src.solsynth.dev/solsynth/distribution/internal/config"
	"src.solsynth.dev/solsynth/distribution/internal/database"
	"src.solsynth.dev/solsynth/distribution/internal/eventbus"
	"src.solsynth.dev/solsynth/distribution/internal/service"
)

// Services is the process-wide dependency set every module may use.
//
// The sync service is intentionally absent: internal/sync imports
// internal/module, so a *sync.Service field here would close an import cycle.
// The sync feature module owns its service instance.
type Services struct {
	Config     *config.Config
	DB         *database.DB
	Releases   *service.ReleaseService
	Publishers service.PublisherDirectory
	Artifacts  service.ArtifactStore
	Events     *eventbus.Bus
	Log        *slog.Logger
}
