package module

import (
	"context"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type greeting interface{ Hello() string }

type greeter struct{ name string }

func (g greeter) Hello() string { return g.name }

func TestPortsSelectByName(t *testing.T) {
	registry := NewPortRegistry()
	Provide[greeting](registry, "greet", "app.solian.sync.github", KindIntegration, greeter{name: "github"})
	Provide[greeting](registry, "greet", "app.solian.sync.gitlab", KindIntegration, greeter{name: "gitlab"})

	value, ok := Select[greeting](registry, "greet", "app.solian.sync.gitlab")
	if !ok || value.Hello() != "gitlab" {
		t.Fatalf("Select() = %v, %v; want the gitlab provider", value, ok)
	}
	if _, ok := Select[greeting](registry, "greet", "app.solian.sync.missing"); ok {
		t.Fatal("Select() accepted an unknown module")
	}
	if _, ok := Get[greeting](registry, "greet"); ok {
		t.Fatal("Get() resolved an ambiguous port")
	}
	_, err := registry.Resolve("greet")
	if err == nil {
		t.Fatal("Resolve() accepted an ambiguous port")
	}
	for _, want := range []string{"app.solian.sync.github", "app.solian.sync.gitlab", "[ports]"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Resolve() error = %v, want it to contain %q", err, want)
		}
	}

	registry.Select(map[string]string{"greet": "app.solian.sync.github"})
	value, ok = Get[greeting](registry, "greet")
	if !ok || value.Hello() != "github" {
		t.Fatalf("Get() after Select = %v, %v; want the github provider", value, ok)
	}
}

func TestPortsFeatureBeatsIntegration(t *testing.T) {
	registry := NewPortRegistry()
	Provide[greeting](registry, "greet", "app.integration", KindIntegration, greeter{name: "integration"})
	Provide[greeting](registry, "greet", "app.feature", KindFeature, greeter{name: "feature"})

	value, ok := Get[greeting](registry, "greet")
	if !ok || value.Hello() != "feature" {
		t.Fatalf("Get() = %v, %v; want the feature provider", value, ok)
	}
	providers := registry.Providers("greet")
	if len(providers) != 2 || providers[0].ModuleID != "app.feature" || providers[1].ModuleID != "app.integration" {
		t.Fatalf("Providers() = %v, want both providers sorted by module ID", providers)
	}
	if registry.Has("missing") {
		t.Fatal("Has() reported a port that was never provided")
	}
}

func TestMigrateSchemaAppliesModelsAndHooksInOrder(t *testing.T) {
	var order []string
	factories := []Factory{
		factory("app.a", func(f *Factory) {
			f.SQL = []SchemaHook{func(context.Context, *gorm.DB) error { order = append(order, "a"); return nil }}
		}),
		factory("app.b", func(f *Factory) {
			f.SQL = []SchemaHook{func(context.Context, *gorm.DB) error { order = append(order, "b"); return nil }}
		}),
	}
	if err := MigrateSchema(context.Background(), nil, factories); err == nil {
		t.Fatal("MigrateSchema() accepted a nil database")
	}
	db, err := gorm.Open(sqlite.Open("file:module-schema?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := MigrateSchema(context.Background(), db, factories); err != nil {
		t.Fatalf("MigrateSchema() error = %v", err)
	}
	if strings.Join(order, ",") != "a,b" {
		t.Fatalf("hook order = %v, want a,b", order)
	}
}
