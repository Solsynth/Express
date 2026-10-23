package module

import (
	"context"
	"strings"
	"testing"
)

type noopInstance struct{}

func (noopInstance) Configure(context.Context, *Host) error { return nil }
func (noopInstance) Stop(context.Context) error             { return nil }

func factory(id string, opts ...func(*Factory)) Factory {
	f := Factory{ID: id, Kind: KindFeature, DefaultEnabled: true, New: func() Instance { return noopInstance{} }}
	for _, opt := range opts {
		opt(&f)
	}
	return f
}

func requires(ids ...string) func(*Factory) {
	return func(f *Factory) { f.Requires = append(f.Requires, ids...) }
}

func TestResolveCascadesDisabledDependency(t *testing.T) {
	factories := []Factory{factory("a"), factory("b", requires("a"))}
	resolution, err := Resolve(factories, nil, []string{"a"})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolution.Enabled["a"] || resolution.Enabled["b"] {
		t.Fatalf("Enabled = %v, want both disabled", resolution.Enabled)
	}
	if len(resolution.Warnings) != 1 {
		t.Fatalf("Warnings = %v, want one cascade warning", resolution.Warnings)
	}
	if !strings.Contains(resolution.Warnings[0], `"b"`) || !strings.Contains(resolution.Warnings[0], `"a"`) {
		t.Fatalf("Warnings = %v, want it to name b and a", resolution.Warnings)
	}
	if len(resolution.Factories) != 0 {
		t.Fatalf("Factories = %d, want 0", len(resolution.Factories))
	}
}

func TestResolveRejectsCycle(t *testing.T) {
	factories := []Factory{factory("a", requires("b")), factory("b", requires("a"))}
	if _, err := Resolve(factories, nil, nil); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("Resolve() error = %v, want a cycle error", err)
	}
}

func TestResolveRejectsUnknownID(t *testing.T) {
	factories := []Factory{factory("a")}
	_, err := Resolve(factories, nil, []string{"app.example.missing"})
	if err == nil || !strings.Contains(err.Error(), `unknown module "app.example.missing"`) {
		t.Fatalf("Resolve() error = %v, want unknown module error", err)
	}
}

func TestResolveOrdersDependencies(t *testing.T) {
	factories := []Factory{
		factory("app.z"),
		factory("app.b", requires("app.a")),
		factory("app.a"),
	}
	resolution, err := Resolve(factories, nil, nil)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	var order []string
	for _, item := range resolution.Factories {
		order = append(order, item.ID)
	}
	want := []string{"app.a", "app.b", "app.z"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v", order, want)
	}
}

func TestResolveEnabledAllowlistAndRequired(t *testing.T) {
	factories := []Factory{
		factory("app.feature"),
		factory("app.other"),
		factory("app.required", func(f *Factory) { f.Required = true; f.DefaultEnabled = false }),
	}
	resolution, err := Resolve(factories, []string{"app.feature"}, nil)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if !resolution.Enabled["app.feature"] || !resolution.Enabled["app.required"] {
		t.Fatalf("Enabled = %v, want feature and required", resolution.Enabled)
	}
	if resolution.Enabled["app.other"] {
		t.Fatalf("Enabled = %v, want other excluded by the allowlist", resolution.Enabled)
	}
	if _, err := Resolve(factories, nil, []string{"app.required"}); err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("Resolve() error = %v, want required-module rejection", err)
	}
}

func TestCapabilitiesSortedUnique(t *testing.T) {
	factories := []Factory{
		factory("app.a", func(f *Factory) { f.Capabilities = []string{"sync", "github"} }),
		factory("app.b", func(f *Factory) { f.Capabilities = []string{"github", " webhook "} }),
	}
	got := Capabilities(factories)
	want := []string{"github", "sync", "webhook"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("Capabilities() = %v, want %v", got, want)
	}
}
