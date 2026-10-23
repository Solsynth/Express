package app

import (
	"fmt"
	"strings"

	"github.com/minio/minio-go/v7"
	"google.golang.org/grpc"
	gen "src.solsynth.dev/sosys/go/proto"

	"src.solsynth.dev/solsynth/distribution/internal/config"
	"src.solsynth.dev/solsynth/distribution/internal/database"
	"src.solsynth.dev/solsynth/distribution/internal/develop"
	"src.solsynth.dev/solsynth/distribution/internal/developadapter"
	"src.solsynth.dev/solsynth/distribution/internal/module"
	"src.solsynth.dev/solsynth/distribution/internal/service"
)

// portalModulePrefix identifies the compiled-in developer-portal modules.
const portalModulePrefix = "app.solian.develop."

// portalEnabled reports whether the resolved module set includes any portal
// module, so a deployment without the portal never dials its dependencies.
func portalEnabled(resolution *module.Resolution) bool {
	if resolution == nil {
		return false
	}
	for _, factory := range resolution.Factories {
		if strings.HasPrefix(factory.ID, portalModulePrefix) {
			return true
		}
	}
	return false
}

// composeDevelop builds the developer-portal domain when the portal is part of
// the resolved module set. It returns (nil, nil, nil) otherwise.
func composeDevelop(
	cfg *config.Config,
	db *database.DB,
	authConn, sphereConn *grpc.ClientConn,
	artifacts service.ArtifactStore,
	resolution *module.Resolution,
) (*develop.Service, *developadapter.Connections, error) {
	if !portalEnabled(resolution) {
		return nil, nil, nil
	}
	connections, err := developadapter.Dial(cfg, authConn, sphereConn)
	if err != nil {
		return nil, nil, fmt.Errorf("dial developer portal dependencies: %w", err)
	}
	service, err := connections.Service(cfg, db)
	if err != nil {
		connections.Close()
		return nil, nil, fmt.Errorf("compose developer portal: %w", err)
	}
	// Mini app archives share the process artifact pool under their own prefix.
	if store, ok := artifacts.(interface {
		Client() (*minio.Client, string)
	}); ok {
		client, bucket := store.Client()
		service.Packages = developadapter.NewPackageStore(cfg, client, bucket)
	}
	service.Profiles = connections.ProfileDirectory()
	return service, connections, nil
}

// registerPortalGRPC publishes DyCustomAppService when the portal owns custom
// apps. Passport (board widgets) and Wallet (products) already call it.
func registerPortalGRPC(registry *module.GRPCRegistry, service *develop.Service, enabled map[string]bool) error {
	if registry == nil || service == nil {
		return nil
	}
	if !enabled["app.solian.develop.apps"] {
		return nil
	}
	return registry.Register("app.solian.develop.apps", "proto.DyCustomAppService", func(server *grpc.Server) {
		gen.RegisterDyCustomAppServiceServer(server, &develop.CustomAppGRPC{Service: service})
	})
}
