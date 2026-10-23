package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	gen "src.solsynth.dev/sosys/go/proto"

	"src.solsynth.dev/solsynth/distribution/internal/config"
	"src.solsynth.dev/solsynth/distribution/internal/core"
	"src.solsynth.dev/solsynth/distribution/internal/database"
	"src.solsynth.dev/solsynth/distribution/internal/eventbus"
	"src.solsynth.dev/solsynth/distribution/internal/httpserver"
	"src.solsynth.dev/solsynth/distribution/internal/integration"
	"src.solsynth.dev/solsynth/distribution/internal/module"
	"src.solsynth.dev/solsynth/distribution/internal/modules/standard"
	"src.solsynth.dev/solsynth/distribution/internal/service"
)

// App is the composed process. Modules are resolved from the compiled set and
// the [modules] config before any of them is constructed.
type App struct {
	Config             *config.Config
	Database           *database.DB
	HTTPServer         *httpserver.Server
	ReleaseService     *service.ReleaseService
	PublisherDirectory service.PublisherDirectory
	ArtifactStore      service.ArtifactStore
	SphereConn         *grpc.ClientConn
	AuthConn           *grpc.ClientConn
	EventBus           *eventbus.Bus
	Host               *module.Host
	Factories          []module.Factory
	Modules            []module.Instance
}

// New composes the process. The logger is threaded into module.Host so modules
// log through the process handler.
func New(cfg *config.Config, log *slog.Logger) (*App, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	if log == nil {
		log = slog.Default()
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	db, err := database.Open(cfg)
	if err != nil {
		return nil, err
	}
	cleanup := func() { _ = db.Close() }
	if err := db.AutoMigrate(); err != nil {
		cleanup()
		return nil, fmt.Errorf("migrate database: %w", err)
	}

	// Resolve the module set before touching external dependencies so an
	// invalid [modules] block fails fast.
	resolution, err := module.Resolve(standard.Modules(), cfg.Modules.Enabled, cfg.Modules.Disabled)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("resolve modules: %w", err)
	}
	for _, warning := range resolution.Warnings {
		log.Warn("module disabled", "reason", warning)
	}
	if err := module.MigrateSchema(context.Background(), db.DB, resolution.Factories); err != nil {
		cleanup()
		return nil, fmt.Errorf("migrate module schema: %w", err)
	}

	httpServer := httpserver.New(cfg)
	authConn, err := integration.Dial(cfg.Auth.Target, cfg.Auth.UseTLS, cfg.Auth.TLSSkipVerify)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("dial auth: %w", err)
	}
	sphereConn, err := integration.Dial(cfg.Sphere.Target, cfg.Sphere.UseTLS, cfg.Sphere.TLSSkipVerify)
	if err != nil {
		_ = authConn.Close()
		cleanup()
		return nil, fmt.Errorf("dial sphere: %w", err)
	}
	publishers := integration.NewSphereDirectory(
		gen.NewDyAuthServiceClient(authConn),
		gen.NewDyPublisherServiceClient(sphereConn),
	)
	artifacts, err := integration.NewS3Store(cfg)
	if err != nil {
		_ = sphereConn.Close()
		_ = authConn.Close()
		cleanup()
		return nil, fmt.Errorf("initialize s3 artifact store: %w", err)
	}
	var events *eventbus.Bus
	if cfg.Eventbus.URL != "" {
		events, err = eventbus.Connect(cfg.Eventbus.URL)
		if err != nil {
			_ = sphereConn.Close()
			_ = authConn.Close()
			cleanup()
			return nil, fmt.Errorf("connect event bus: %w", err)
		}
	}
	releases := service.NewPublisherReleaseService(db.DB, publishers, artifacts, events)
	releases.SetPermissionClient(gen.NewDyPermissionServiceClient(authConn))
	releases.ConfigureAnalytics(cfg.Analytics.Enabled, cfg.Analytics.Salt)
	releases.ConfigureArtifactRetention(cfg.Releases.ArtifactRetention)

	// The catalog is the always-on surface, so it registers before modules and
	// module routes share the same /api group.
	httpserver.RegisterPublisherRoutes(httpServer.Engine, releases, publishers, cfg)

	services := &core.Services{Config: cfg, DB: db, Releases: releases, Publishers: publishers, Artifacts: artifacts, Events: events, Log: log}
	host := &module.Host{
		Core:     services,
		Engine:   httpServer.Engine,
		API:      httpServer.Engine.Group("/api"),
		Ports:    module.NewPortRegistry(),
		Enabled:  resolution.Enabled,
		Settings: cfg.Modules.Settings,
		Log:      log,
	}
	host.Ports.Select(cfg.Ports)

	app := &App{
		Config:             cfg,
		Database:           db,
		HTTPServer:         httpServer,
		ReleaseService:     releases,
		PublisherDirectory: publishers,
		ArtifactStore:      artifacts,
		SphereConn:         sphereConn,
		AuthConn:           authConn,
		EventBus:           events,
		Host:               host,
		Factories:          resolution.Factories,
	}
	registerModuleRoutes(httpServer.Engine, resolution.Factories)

	if err := app.startModules(context.Background()); err != nil {
		app.Stop(context.Background())
		return nil, err
	}
	return app, nil
}

// startModules instantiates, wires, and configures every resolved module. All
// providers publish before any module configures, so a consumer resolves ports
// regardless of module order.
func (a *App) startModules(ctx context.Context) error {
	instances := make([]module.Instance, 0, len(a.Factories))
	for _, factory := range a.Factories {
		if factory.New == nil {
			a.Modules = instances
			return fmt.Errorf("module %s has no constructor", factory.ID)
		}
		instance := factory.New()
		if instance == nil {
			a.Modules = instances
			return fmt.Errorf("module %s returned a nil instance", factory.ID)
		}
		instances = append(instances, instance)
	}
	a.Modules = instances

	for index, factory := range a.Factories {
		provider, ok := a.Modules[index].(module.PortProvider)
		if !ok {
			continue
		}
		if err := provider.ProvidePorts(ctx, a.Host); err != nil {
			return &module.BuildError{Module: factory.ID, Err: err}
		}
	}
	for index, factory := range a.Factories {
		if err := a.Modules[index].Configure(ctx, a.Host); err != nil {
			return &module.BuildError{Module: factory.ID, Err: err}
		}
	}
	return nil
}

// moduleView is one entry of GET /api/modules.
type moduleView struct {
	ID           string   `json:"id"`
	Kind         string   `json:"kind"`
	Capabilities []string `json:"capabilities,omitempty"`
}

func registerModuleRoutes(engine *gin.Engine, factories []module.Factory) {
	if engine == nil {
		return
	}
	views := make([]moduleView, 0, len(factories))
	for _, factory := range factories {
		kind := string(factory.Kind)
		if kind == "" {
			kind = string(module.KindFeature)
		}
		views = append(views, moduleView{ID: factory.ID, Kind: kind, Capabilities: factory.Capabilities})
	}
	capabilities := module.Capabilities(factories)
	engine.GET("/api/modules", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"modules": views, "capabilities": capabilities})
	})
}

func (a *App) Start(context.Context) error { return nil }

// Stop tears down modules in reverse configuration order, then the event bus,
// gRPC connections, and the database.
func (a *App) Stop(ctx context.Context) error {
	if a == nil {
		return nil
	}
	for index := len(a.Modules) - 1; index >= 0; index-- {
		if err := a.Modules[index].Stop(ctx); err != nil {
			a.log().Warn("stop module", "index", index, "error", err)
		}
	}
	if a.EventBus != nil {
		a.EventBus.Close()
	}
	if a.AuthConn != nil {
		_ = a.AuthConn.Close()
	}
	if a.SphereConn != nil {
		_ = a.SphereConn.Close()
	}
	if a.Database != nil {
		return a.Database.Close()
	}
	return nil
}

func (a *App) log() *slog.Logger {
	if a != nil && a.Host != nil && a.Host.Log != nil {
		return a.Host.Log
	}
	return slog.Default()
}

var _ = strings.TrimSpace
