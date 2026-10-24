package config

import (
	"path/filepath"
	"testing"
)

// validConfig is a config that satisfies every required field, so a test can
// vary one picker value and see that value's own validation. Validate checks
// the whole config, so an empty one fails on the database before it ever
// reaches the picker.
func validConfig() *Config {
	cfg := Default()
	cfg.Database.DSN = "file::memory:?cache=shared"
	cfg.Auth.Target = "stargate:9090"
	cfg.Sphere.Target = "sphere:9090"
	cfg.S3.Endpoint = "https://s3.test"
	cfg.S3.AccessKey = "k"
	cfg.S3.SecretKey = "s"
	cfg.S3.Bucket = "b"
	return cfg
}

// TestPickerEnvOverrides proves a deployment can configure the cross-domain
// picker entirely from the environment, the way the README promises for every
// setting. The picker is a deployment decision made at deploy time, so a
// container that cannot mount a config file still has to be able to set it.
func TestPickerEnvOverrides(t *testing.T) {
	t.Setenv("CONFIG_PATH", filepath.Join(t.TempDir(), "missing.toml"))
	t.Setenv("DISTRIBUTION_PICKER_CLIENT_ID", "app.solian.snfs")
	t.Setenv("DISTRIBUTION_PICKER_FRAME_URL", "https://fs.solian.app/picker")
	t.Setenv("DISTRIBUTION_HANDOFF_BASE_URL", "https://id.solian.app")
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
	if cfg.Picker.ClientID != "app.solian.snfs" {
		t.Fatalf("picker client id = %q", cfg.Picker.ClientID)
	}
	if cfg.Picker.FrameURL != "https://fs.solian.app/picker" {
		t.Fatalf("picker frame url = %q", cfg.Picker.FrameURL)
	}
	if !cfg.PickerEnabled() {
		t.Fatal("picker must be enabled once both fields are set")
	}
}

// TestPickerEnabledRequiresBoth proves half the pair leaves the feature off
// rather than half-working. A client id with no frame URL has nothing to embed,
// and a frame URL with no client id has no code to redeem.
func TestPickerEnabledRequiresBoth(t *testing.T) {
	var empty Config
	if empty.PickerEnabled() {
		t.Fatal("an empty config must not enable the picker")
	}
	half := Config{}
	half.Picker.ClientID = "app.solian.snfs"
	if half.PickerEnabled() {
		t.Fatal("a client id alone must not enable the picker")
	}
	other := Config{}
	other.Picker.FrameURL = "https://fs.solian.app/picker"
	if other.PickerEnabled() {
		t.Fatal("a frame URL alone must not enable the picker")
	}
	both := Config{}
	both.Picker.ClientID = "app.solian.snfs"
	both.Picker.FrameURL = "https://fs.solian.app/picker"
	if !both.PickerEnabled() {
		t.Fatal("both fields must enable the picker")
	}
}

// TestPickerClientRequiresIssuer proves a half-configured picker fails at
// load rather than answering 503 at runtime. The issuer is what redeems the
// code, so a client id without one is a mistake the operator should be told
// about once, at startup.
func TestPickerClientRequiresIssuer(t *testing.T) {
	cfg := validConfig()
	cfg.Picker.ClientID = "app.solian.snfs"
	cfg.Picker.FrameURL = "https://fs.solian.app/picker"

	err := cfg.Validate()
	if err == nil {
		t.Fatal("a picker client without the issuer must be refused")
	}
}

// TestPickerFrameURLValidated pins the shape a frame URL must take. The issuer
// refuses a target whose host the picker client is not registered for, so a
// malformed value would otherwise surface as a refused hand-off at runtime
// rather than as a startup error.
func TestPickerFrameURLValidated(t *testing.T) {
	valid := []string{
		"https://fs.solian.app/picker",
		"http://localhost:5173/picker",
	}
	for _, frameURL := range valid {
		cfg := validConfig()
		cfg.Picker.FrameURL = frameURL
		if err := cfg.Validate(); err != nil {
			t.Fatalf("Validate() rejected %q: %v", frameURL, err)
		}
	}

	invalid := []string{
		"fs.solian.app/picker",           // no scheme
		"fs.solian.app",                  // no scheme and no path
		"//fs.solian.app/picker",         // scheme-relative
		"https://fs.solian.app/pick?x=1", // query is not a target the issuer can validate
	}
	for _, frameURL := range invalid {
		cfg := validConfig()
		cfg.Picker.FrameURL = frameURL
		if err := cfg.Validate(); err == nil {
			t.Fatalf("Validate() accepted %q, want a rejection", frameURL)
		}
	}
}
