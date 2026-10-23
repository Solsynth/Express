// Package syncmodule owns the app.solian.distribution.sync feature module. It
// constructs the sync engine, mounts its HTTP surface, and installs the
// outbound hook on the release service.
package syncmodule

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"src.solsynth.dev/solsynth/distribution/internal/config"
	"src.solsynth.dev/solsynth/distribution/internal/database"
	"src.solsynth.dev/solsynth/distribution/internal/httpserver/syncctl"
	"src.solsynth.dev/solsynth/distribution/internal/module"
	"src.solsynth.dev/solsynth/distribution/internal/service"
	syncengine "src.solsynth.dev/solsynth/distribution/internal/sync"
)

// ModuleID is the feature module that owns the sync outbox and routes.
const ModuleID = syncengine.ModuleID

// Factory describes the sync feature module.
func Factory() module.Factory {
	return module.Factory{
		ID:             ModuleID,
		Kind:           module.KindFeature,
		DefaultEnabled: true,
		Ports:          []module.PortRequirement{{ID: syncengine.PortSyncTargets, Optional: true}},
		Models:         []any{&database.SyncConnection{}, &database.SyncLink{}, &database.SyncJob{}},
		Capabilities:   []string{"sync"},
		New:            func() module.Instance { return &instance{} },
	}
}

type instance struct {
	svc *syncengine.Service
}

func (i *instance) Configure(ctx context.Context, host *module.Host) error {
	if host == nil || host.Core == nil {
		return fmt.Errorf("sync module requires the core services")
	}
	key := strings.TrimSpace(host.Core.Config.Sync.EncryptionKey)
	if key == "" {
		return errors.New("sync.encryptionKey is required when " + ModuleID + " is enabled")
	}
	sealer, err := syncengine.NewSealer(key)
	if err != nil {
		return fmt.Errorf("sync.encryptionKey: %w", err)
	}
	options, err := optionsFromConfig(host.Core.Config.Sync)
	if err != nil {
		return err
	}
	options.Releases = host.Core.Releases
	if reader, ok := host.Core.Artifacts.(service.ArtifactReader); ok {
		options.Artifacts = reader
	}
	if host.Core.Config != nil {
		options.DownloadBaseURL = host.Core.Releases.DownloadEndpointBaseURL()
	}

	svc := syncengine.NewService(host.Core.DB.DB, sealer, host.Ports, options, host.Log)
	svc.Configure(host.Enabled)
	i.svc = svc

	host.Core.Releases.SetSyncEnqueuer(svc)
	syncctl.Register(host.API, syncctl.Deps{Service: svc, Releases: host.Core.Releases, Publishers: host.Core.Publishers})
	svc.Start(ctx)
	return nil
}

func (i *instance) Stop(ctx context.Context) error {
	if i.svc == nil {
		return nil
	}
	return i.svc.Stop(ctx)
}

// optionsFromConfig converts the validated config block into engine options.
func optionsFromConfig(cfg config.SyncConfig) (syncengine.Options, error) {
	parse := func(name, value string) (time.Duration, error) {
		value = strings.TrimSpace(value)
		if value == "" {
			return 0, nil
		}
		parsed, err := time.ParseDuration(value)
		if err != nil {
			return 0, fmt.Errorf("sync.%s must be a Go duration: %w", name, err)
		}
		return parsed, nil
	}
	options := syncengine.Options{
		MaxAttempts:   cfg.MaxAttempts,
		MaxAssetBytes: cfg.MaxAssetBytes,
		BatchSize:     cfg.BatchSize,
	}
	var err error
	if options.PollInterval, err = parse("pollInterval", cfg.PollInterval); err != nil {
		return syncengine.Options{}, err
	}
	if options.RetryBase, err = parse("retryBase", cfg.RetryBase); err != nil {
		return syncengine.Options{}, err
	}
	if options.RetryCap, err = parse("retryCap", cfg.RetryCap); err != nil {
		return syncengine.Options{}, err
	}
	return options, nil
}
