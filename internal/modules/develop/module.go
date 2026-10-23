// Package developmodule mounts the developer-portal HTTP surfaces. Each module
// owns one product area so a deployment can enable them independently.
package developmodule

import (
	"context"
	"fmt"

	"src.solsynth.dev/solsynth/distribution/internal/develop"
	"src.solsynth.dev/solsynth/distribution/internal/developapi"
	"src.solsynth.dev/solsynth/distribution/internal/module"
)

// Module IDs. They keep the identity service's product names so an operator's
// mental model and the capability list carry over.
const (
	ModuleDevelopers = "app.solian.develop.developers"
	ModuleApps       = "app.solian.develop.apps"
	ModuleBots       = "app.solian.develop.bots"
	ModuleMiniApps   = "app.solian.develop.miniapps"
	ModuleProducts   = "app.solian.develop.products"
)

// Factories returns every developer-portal module factory.
func Factories() []module.Factory {
	return []module.Factory{
		developersFactory(),
		appsFactory(),
		botsFactory(),
		miniAppsFactory(),
		productsFactory(),
	}
}

// portalModule is the shared shape of the portal modules: an id and the schema
// that module owns.
type portalModule struct {
	id     string
	models []any
}

// portalInstance configures one portal surface.
type portalInstance struct {
	id string
}

func newFactory(spec portalModule, capability string, importer func() module.Importer) module.Factory {
	return module.Factory{
		ID:             spec.id,
		Kind:           module.KindFeature,
		DefaultEnabled: true,
		Models:         spec.models,
		Capabilities:   []string{capability},
		Importer:       importer,
		New:            func() module.Instance { return &portalInstance{id: spec.id} },
	}
}

// Configure mounts the module's routes.
func (p *portalInstance) Configure(_ context.Context, host *module.Host) error {
	if host == nil || host.API == nil {
		return fmt.Errorf("module %s requires the HTTP API group", p.id)
	}
	services, err := portalDeps(host)
	if err != nil {
		return err
	}
	switch p.id {
	case ModuleDevelopers:
		registerDeveloperRoutes(host.API, services)
	case ModuleApps:
		registerAppRoutes(host.API, services)
	case ModuleBots:
		registerBotRoutes(host.API, services)
	case ModuleMiniApps:
		registerMiniAppRoutes(host.API, services)
	case ModuleProducts:
		registerProductRoutes(host.API, services)
	default:
		return fmt.Errorf("unknown developer module %s", p.id)
	}
	return nil
}

// Stop releases nothing: the portal modules own no background work.
func (p *portalInstance) Stop(context.Context) error { return nil }

// portalDeps builds the route dependencies from the process services.
func portalDeps(host *module.Host) (Deps, error) {
	services := host.Core
	if services == nil || services.Develop == nil {
		return Deps{}, fmt.Errorf("developer portal is not composed")
	}
	return Deps{
		Service: services.Develop,
		Deps: developapi.Deps{
			Service:     services.Develop,
			Publishers:  services.Publishers,
			Permissions: services.Develop.Permissions,
			Sudo:        services.Sudo,
		},
		Events:          services.Events,
		MaxPackageBytes: services.Config.Develop.Packages.MaxBytes,
	}, nil
}

// developerModels is the developer-program schema.
func developerModels() []any {
	return []any{&develop.Developer{}, &develop.DevProject{}}
}

// appModels is the custom-app schema, including board widgets and secrets.
func appModels() []any {
	return []any{&develop.CustomApp{}, &develop.CustomAppSecret{}, &develop.BoardWidget{}}
}

func developersFactory() module.Factory {
	return newFactory(portalModule{id: ModuleDevelopers, models: developerModels()}, "developers",
		func() module.Importer { return developersImporter() })
}

func appsFactory() module.Factory {
	factory := newFactory(portalModule{id: ModuleApps, models: appModels()}, "custom-apps",
		func() module.Importer { return appsImporter() })
	factory.Requires = []string{ModuleDevelopers}
	return factory
}

func botsFactory() module.Factory {
	factory := newFactory(portalModule{id: ModuleBots, models: []any{&develop.BotAccount{}, &develop.BotChatConfig{}}}, "bots",
		func() module.Importer { return botsImporter() })
	factory.Requires = []string{ModuleDevelopers}
	return factory
}

func miniAppsFactory() module.Factory {
	factory := newFactory(portalModule{id: ModuleMiniApps, models: []any{&develop.MiniApp{}}}, "miniapps",
		func() module.Importer { return miniAppsImporter() })
	factory.Requires = []string{ModuleDevelopers}
	return factory
}

func productsFactory() module.Factory {
	factory := newFactory(portalModule{id: ModuleProducts, models: []any{&develop.AppProduct{}, &develop.AppProductState{}}}, "app-products",
		func() module.Importer { return productsImporter() })
	factory.Requires = []string{ModuleApps}
	return factory
}
