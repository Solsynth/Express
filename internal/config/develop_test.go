package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

// TestExampleConfigHasDevelopBlock proves the shipped template parses into the
// develop configuration, so an operator can copy it as-is.
func TestExampleConfigHasDevelopBlock(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "config.example.toml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := Default()
	if err := toml.Unmarshal(data, cfg); err != nil {
		t.Fatalf("parse example config: %v", err)
	}
	if cfg.Develop.Files.Target != "drive:9090" {
		t.Fatalf("develop.files.target = %q", cfg.Develop.Files.Target)
	}
	if cfg.Develop.Profiles.Target != "passport:9090" {
		t.Fatalf("develop.profiles.target = %q", cfg.Develop.Profiles.Target)
	}
	if cfg.Develop.Packages.KeyPrefix != "plugins" || cfg.Develop.Packages.MaxBytes != 5242880 {
		t.Fatalf("develop.packages = %+v", cfg.Develop.Packages)
	}
}

// TestDevelopDefaults proves an empty config still yields a usable package
// store configuration.
func TestDevelopDefaults(t *testing.T) {
	cfg := Default()
	if cfg.Develop.Packages.KeyPrefix != "plugins" {
		t.Fatalf("key prefix = %q", cfg.Develop.Packages.KeyPrefix)
	}
	if cfg.Develop.Packages.MaxBytes != 5*1024*1024 {
		t.Fatalf("max bytes = %d", cfg.Develop.Packages.MaxBytes)
	}
}

// TestDevelopPackagesValidation proves the package settings are validated.
func TestDevelopPackagesValidation(t *testing.T) {
	cfg := Default()
	cfg.Database.DSN = "file::memory:?cache=shared"
	cfg.Auth.Target = "stargate:9090"
	cfg.Sphere.Target = "sphere:9090"
	cfg.S3.Endpoint = "https://s3.test"
	cfg.S3.AccessKey = "k"
	cfg.S3.SecretKey = "s"
	cfg.S3.Bucket = "b"

	cfg.Develop.Packages.MaxBytes = -1
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() accepted a negative maxBytes")
	}
	cfg.Develop.Packages.MaxBytes = 1024
	cfg.Develop.Packages.PublicBaseURL = "not-a-url"
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() accepted a relative publicBaseUrl")
	}
	cfg.Develop.Packages.PublicBaseURL = "https://cdn.example.test/plugins"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() rejected a valid URL: %v", err)
	}
}

// TestDevelopEnvOverrides proves the environment can configure the portal.
func TestDevelopEnvOverrides(t *testing.T) {
	t.Setenv("CONFIG_PATH", filepath.Join(t.TempDir(), "missing.toml"))
	t.Setenv("DISTRIBUTION_DEVELOP_FILES_TARGET", "drive.internal:9090")
	t.Setenv("DISTRIBUTION_DEVELOP_PROFILES_TARGET", "passport.internal:9090")
	t.Setenv("DISTRIBUTION_DEVELOP_PACKAGES_MAX_BYTES", "1048576")
	t.Setenv("DISTRIBUTION_DATABASE_DSN", "file::memory:?cache=shared")
	t.Setenv("DISTRIBUTION_AUTH_TARGET", "stargate:9090")
	t.Setenv("DISTRIBUTION_SPHERE_TARGET", "sphere:9090")
	t.Setenv("DISTRIBUTION_S3_ENDPOINT", "https://s3.test")
	t.Setenv("DISTRIBUTION_S3_ACCESS_KEY", "k")
	t.Setenv("DISTRIBUTION_S3_SECRET_KEY", "s")
	t.Setenv("DISTRIBUTION_S3_BUCKET", "b")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Develop.Files.Target != "drive.internal:9090" {
		t.Fatalf("files target = %q", cfg.Develop.Files.Target)
	}
	if cfg.Develop.Profiles.Target != "passport.internal:9090" {
		t.Fatalf("profiles target = %q", cfg.Develop.Profiles.Target)
	}
	if cfg.Develop.Packages.MaxBytes != 1048576 {
		t.Fatalf("max bytes = %d", cfg.Develop.Packages.MaxBytes)
	}
}
