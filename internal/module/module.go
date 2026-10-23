// Package module implements DistributionCenter's compile-time module system.
// It is intentionally small: modules are linked into the binary (Caddy style)
// and the registry decides which of them are mounted at runtime.
package module

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"src.solsynth.dev/solsynth/distribution/internal/core"
)

// Kind separates domain features from ecosystem adapters.
type Kind string

const (
	KindFeature     Kind = "feature"
	KindIntegration Kind = "integration"
)

// PortID names an interface a module consumes or provides.
type PortID string

// PortRequirement declares a port a module needs. Optional ports may be
// absent; the module must then gate the dependent feature.
type PortRequirement struct {
	ID       PortID
	Optional bool
}

// SchemaHook applies schema changes AutoMigrate cannot express (partial
// indexes, extensions, custom constraints, data backfills). Hooks must be
// idempotent and must never drop data.
type SchemaHook func(ctx context.Context, db *gorm.DB) error

// Factory describes one compiled-in module. Everything except New is static
// so the resolver can make decisions before any module is constructed.
type Factory struct {
	ID             string
	Kind           Kind
	DefaultEnabled bool
	Required       bool

	// Requires lists feature modules that must be enabled for this module to
	// be meaningful. Disabling a dependency cascades to its dependents.
	Requires []string

	// Ports are runtime interfaces (local or remote). Provides advertises the
	// ports this module can publish during ProvidePorts.
	Ports    []PortRequirement
	Provides []PortID

	// Models and SQL own the module's schema. They are applied only while the
	// module is enabled.
	Models []any
	SQL    []SchemaHook

	// Capabilities are exposed through GET /api/modules.
	Capabilities []string

	// Importer optionally migrates legacy rows into this module's schema. It
	// is invoked by cmd/distribution-import, never at startup.
	Importer func() Importer

	New func() Instance
}

// Instance is a constructed module.
type Instance interface {
	// Configure wires HTTP routes, consumers and jobs. It runs after core
	// migrations and module AutoMigrate.
	Configure(ctx context.Context, host *Host) error
	// Stop releases resources. It runs in reverse order during shutdown.
	Stop(ctx context.Context) error
}

// PortProvider is an optional instance interface. All providers are called
// before any module's Configure, so consumers can resolve ports regardless of
// module order.
type PortProvider interface {
	ProvidePorts(ctx context.Context, host *Host) error
}

// Host is the module's view of the running process.
type Host struct {
	Core *core.Services

	Engine *gin.Engine
	API    *gin.RouterGroup

	Ports   *PortRegistry
	Enabled map[string]bool
	// Settings is the decoded [modules.settings."<id>"] subtree.
	Settings map[string]map[string]any

	// GRPC collects gRPC service registrations. The process creates the gRPC
	// server after modules configure, so modules register here and the binary
	// applies them once the server exists.
	GRPC *GRPCRegistry

	Log *slog.Logger
}

// ModuleSettings returns the decoded settings for a module ID.
func (h *Host) ModuleSettings(id string) map[string]any {
	if h == nil {
		return nil
	}
	return h.Settings[id]
}

// IsEnabled reports whether a module is part of the resolved set.
func (h *Host) IsEnabled(id string) bool {
	if h == nil {
		return false
	}
	return h.Enabled[id]
}

// BuildError wraps a module configuration failure with its module ID.
type BuildError struct {
	Module string
	Err    error
}

func (e *BuildError) Error() string {
	return fmt.Sprintf("module %s: %v", e.Module, e.Err)
}

func (e *BuildError) Unwrap() error { return e.Err }
