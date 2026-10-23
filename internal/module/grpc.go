package module

import (
	"fmt"
	"sort"
	"sync"

	"google.golang.org/grpc"
)

// GRPCRegistry collects the gRPC services modules contribute. The process
// builds its gRPC server after every module has configured, so modules register
// here instead of holding a *grpc.Server they do not own.
//
// Registration is ordered by module ID so a deployment's service set is
// deterministic regardless of map iteration.
type GRPCRegistry struct {
	mu       sync.Mutex
	entries  []grpcEntry
	services map[string]bool
}

type grpcEntry struct {
	ModuleID string
	Service  string
	Register func(*grpc.Server)
}

// NewGRPCRegistry creates an empty registry.
func NewGRPCRegistry() *GRPCRegistry {
	return &GRPCRegistry{services: map[string]bool{}}
}

// Register adds one service implementation. A duplicate service name is an
// error: two modules publishing the same gRPC contract would make the winner
// depend on module order.
func (r *GRPCRegistry) Register(moduleID, service string, register func(*grpc.Server)) error {
	if r == nil {
		return fmt.Errorf("gRPC registry is not configured")
	}
	if register == nil {
		return fmt.Errorf("service %q has no register function", service)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.services[service] {
		return fmt.Errorf("gRPC service %q is already registered", service)
	}
	r.services[service] = true
	r.entries = append(r.entries, grpcEntry{ModuleID: moduleID, Service: service, Register: register})
	return nil
}

// Apply registers every collected service on the server.
func (r *GRPCRegistry) Apply(server *grpc.Server) {
	if r == nil || server == nil {
		return
	}
	r.mu.Lock()
	entries := make([]grpcEntry, len(r.entries))
	copy(entries, r.entries)
	r.mu.Unlock()
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].ModuleID != entries[j].ModuleID {
			return entries[i].ModuleID < entries[j].ModuleID
		}
		return entries[i].Service < entries[j].Service
	})
	for _, entry := range entries {
		entry.Register(server)
	}
}

// Services lists the registered service names, ordered by module then name.
func (r *GRPCRegistry) Services() []string {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make([]string, 0, len(r.entries))
	for _, entry := range r.entries {
		names = append(names, entry.Service)
	}
	sort.Strings(names)
	return names
}
