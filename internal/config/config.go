// Package config loads DistributionCenter configuration.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// Config is the process configuration shared by the HTTP, gRPC, and service
// discovery surfaces.
type Config struct {
	ServiceName string `toml:"serviceName"`
	Version     string `toml:"version"`

	HTTP struct {
		Port    string `toml:"port"`
		BaseURL string `toml:"baseUrl"`
	} `toml:"http"`

	// Web configures hosting of the built SPA (web/dist). The zero value
	// disables static hosting and leaves the server API-only; the Docker image
	// enables it for /app/web/dist.
	Web struct {
		DistPath string `toml:"distPath"`
	} `toml:"web"`

	GRPC struct {
		Port     string `toml:"port"`
		UseTLS   bool   `toml:"useTLS"`
		CertFile string `toml:"certFile"`
		KeyFile  string `toml:"keyFile"`
	} `toml:"grpc"`

	Discovery struct {
		Enabled           bool   `toml:"enabled"`
		Target            string `toml:"target"`
		RegistrationToken string `toml:"registrationToken"`
		Service           string `toml:"service"`
		InstanceID        string `toml:"instanceId"`
		HttpEndpoint      string `toml:"httpEndpoint"`
		GrpcEndpoint      string `toml:"grpcEndpoint"`
		LeaseSeconds      int    `toml:"leaseSeconds"`
		Weight            int    `toml:"weight"`
	} `toml:"discovery"`

	Database struct {
		DSN string `toml:"dsn"`
	} `toml:"database"`

	Auth struct {
		Target        string `toml:"target"`
		UseTLS        bool   `toml:"useTLS"`
		TLSSkipVerify bool   `toml:"tlsSkipVerify"`
	} `toml:"auth"`

	// Handoff configures the cross-domain session hand-off with the identity
	// issuer. It is optional: an empty BaseURL or ClientID keeps the console
	// API-only and the hand-off routes answer 503.
	Handoff HandoffConfig `toml:"handoff"`

	Sphere struct {
		Target        string `toml:"target"`
		UseTLS        bool   `toml:"useTLS"`
		TLSSkipVerify bool   `toml:"tlsSkipVerify"`
	} `toml:"sphere"`

	S3 struct {
		Endpoint  string `toml:"endpoint"`
		AccessKey string `toml:"accessKey"`
		SecretKey string `toml:"secretKey"`
		Bucket    string `toml:"bucket"`
		Region    string `toml:"region"`
	} `toml:"s3"`

	Releases struct {
		ArtifactRetention int `toml:"artifactRetention"`
	} `toml:"releases"`

	Eventbus struct {
		URL string `toml:"url"`
	} `toml:"eventbus"`

	Analytics struct {
		Enabled bool   `toml:"enabled"`
		Salt    string `toml:"salt"`
	} `toml:"analytics"`

	Modules struct {
		Enabled  []string                  `toml:"enabled"`
		Disabled []string                  `toml:"disabled"`
		Settings map[string]map[string]any `toml:"settings"`
	} `toml:"modules"`

	// Ports selects the module that provides a shared port
	// (port id -> module id).
	Ports map[string]string `toml:"ports"`

	Sync SyncConfig `toml:"sync"`
}

// HandoffConfig names the relying-app side of the identity hand-off.
type HandoffConfig struct {
	// BaseURL is the issuer's HTTP base (e.g. https://id.solian.app); the
	// redeem and renew endpoints are appended to it.
	BaseURL string `toml:"baseUrl"`
	// ClientID is the app's registered id on the issuer.
	ClientID string `toml:"clientId"`
	// ClientSecret proves the redemption belongs to this app. Empty makes it
	// a public client, which the issuer only accepts with PKCE.
	ClientSecret string `toml:"clientSecret"`
	// CookieDomain is the domain this console sets its own session cookie on
	// (e.g. dx.solsynth.dev). Empty keeps the cookie host-only.
	CookieDomain string `toml:"cookieDomain"`
	// CookieSecure marks the cookies https-only. Keep it true outside local
	// development, where a Secure cookie is silently dropped over http.
	CookieSecure bool `toml:"cookieSecure"`
}

// HandoffEnabled reports whether the issuer hand-off is configured. The
// console then signs in through the issuer instead of a bearer token.
func (c *Config) HandoffEnabled() bool {
	return c != nil && strings.TrimSpace(c.Handoff.BaseURL) != "" && strings.TrimSpace(c.Handoff.ClientID) != ""
}

// SyncConfig tunes the release sync engine.
type SyncConfig struct {
	// EncryptionKey seals provider credentials at rest: base64 of a 32-byte
	// key, or 64 hex characters. Required while the sync module is enabled.
	EncryptionKey string `toml:"encryptionKey"`
	// Durations are strings because go-toml/v2 has no time.Duration decoder;
	// Validate parses them.
	PollInterval  string `toml:"pollInterval"`
	RetryBase     string `toml:"retryBase"`
	RetryCap      string `toml:"retryCap"`
	MaxAttempts   int    `toml:"maxAttempts"`
	MaxAssetBytes int64  `toml:"maxAssetBytes"`
	BatchSize     int    `toml:"batchSize"`
}

// Validate checks the required dependencies for the durable marketplace
// service. Optional discovery and event-bus settings are intentionally not
// included.
func (c *Config) Validate() error {
	required := []struct {
		name  string
		value string
	}{
		{"database.dsn", c.Database.DSN},
		{"auth.target", c.Auth.Target},
		{"sphere.target", c.Sphere.Target},
		{"s3.endpoint", c.S3.Endpoint},
		{"s3.accessKey", c.S3.AccessKey},
		{"s3.secretKey", c.S3.SecretKey},
		{"s3.bucket", c.S3.Bucket},
	}
	for _, item := range required {
		if strings.TrimSpace(item.value) == "" {
			return fmt.Errorf("%s is required", item.name)
		}
	}
	if baseURL := strings.TrimSpace(c.HTTP.BaseURL); baseURL != "" {
		parsed, err := url.Parse(baseURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return fmt.Errorf("http.baseUrl must be an absolute HTTP(S) URL with an optional path prefix")
		}
		if strings.TrimRight(parsed.Path, "/") == "/api" || strings.HasSuffix(strings.TrimRight(parsed.Path, "/"), "/api") {
			return fmt.Errorf("http.baseUrl must not include the /api path")
		}
	}
	if baseURL := strings.TrimSpace(c.Handoff.BaseURL); baseURL != "" {
		parsed, err := url.Parse(baseURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return fmt.Errorf("handoff.baseUrl must be an absolute HTTP(S) URL")
		}
	}
	// A cookie domain is a bare domain, never an origin: a browser silently
	// drops a cookie whose Domain does not domain-match the response host.
	if domain := strings.TrimSpace(c.Handoff.CookieDomain); domain != "" {
		if strings.ContainsAny(domain, "/:") {
			return fmt.Errorf("handoff.cookieDomain must be a bare domain such as dx.solsynth.dev")
		}
	}
	if c.Releases.ArtifactRetention < 0 {
		return fmt.Errorf("releases.artifactRetention must be zero or greater")
	}
	for _, list := range []struct {
		name string
		ids  []string
	}{
		{"modules.enabled", c.Modules.Enabled},
		{"modules.disabled", c.Modules.Disabled},
	} {
		for _, id := range list.ids {
			if strings.TrimSpace(id) == "" {
				return fmt.Errorf("%s must not contain an empty module ID", list.name)
			}
		}
	}
	for _, item := range []struct {
		name  string
		value string
	}{
		{"sync.pollInterval", c.Sync.PollInterval},
		{"sync.retryBase", c.Sync.RetryBase},
		{"sync.retryCap", c.Sync.RetryCap},
	} {
		duration, err := time.ParseDuration(strings.TrimSpace(item.value))
		if err != nil {
			return fmt.Errorf("%s must be a Go duration such as 30s", item.name)
		}
		if duration <= 0 {
			return fmt.Errorf("%s must be greater than zero", item.name)
		}
	}
	if c.Sync.MaxAttempts < 1 {
		return fmt.Errorf("sync.maxAttempts must be at least 1")
	}
	if c.Sync.MaxAssetBytes < 0 {
		return fmt.Errorf("sync.maxAssetBytes must be zero or greater")
	}
	if c.Sync.BatchSize < 1 {
		return fmt.Errorf("sync.batchSize must be at least 1")
	}
	return nil
}

// Default returns local-development values for non-composition tests. The
// durable application still requires external dependency settings in Load.
func Default() *Config {
	cfg := &Config{
		ServiceName: "distribution",
		Version:     "dev",
	}
	cfg.HTTP.Port = "8080"
	cfg.Releases.ArtifactRetention = 3
	cfg.GRPC.Port = "9090"
	cfg.Discovery.LeaseSeconds = 30
	cfg.Discovery.Weight = 1
	cfg.Analytics.Enabled = true
	cfg.Handoff.CookieSecure = true
	cfg.Sync.PollInterval = "5s"
	cfg.Sync.RetryBase = "30s"
	cfg.Sync.RetryCap = "1h"
	cfg.Sync.MaxAttempts = 8
	cfg.Sync.MaxAssetBytes = 2147483648
	cfg.Sync.BatchSize = 16
	return cfg
}

// Load reads the TOML file at path. A missing file is allowed so the binary
// can be configured entirely through environment variables.
func Load(path string) (*Config, error) {
	if path == "" {
		path = os.Getenv("CONFIG_PATH")
	}
	if path == "" {
		path = "config.example.toml"
	}

	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("read config %s: %w", path, err)
		}
	} else if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	applyEnvOverrides(cfg)
	if cfg.Discovery.Service == "" {
		cfg.Discovery.Service = cfg.ServiceName
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	return cfg, nil
}

func applyEnvOverrides(cfg *Config) {
	setStr("DISTRIBUTION_SERVICE_NAME", &cfg.ServiceName)
	setStr("DISTRIBUTION_VERSION", &cfg.Version)
	setStr("DISTRIBUTION_HTTP_PORT", &cfg.HTTP.Port)
	setStr("DISTRIBUTION_HTTP_BASE_URL", &cfg.HTTP.BaseURL)
	setStr("DISTRIBUTION_WEB_DIST_PATH", &cfg.Web.DistPath)
	setStr("DISTRIBUTION_GRPC_PORT", &cfg.GRPC.Port)
	setBool("DISTRIBUTION_GRPC_USETLS", &cfg.GRPC.UseTLS)
	setStr("DISTRIBUTION_GRPC_CERT_FILE", &cfg.GRPC.CertFile)
	setStr("DISTRIBUTION_GRPC_KEY_FILE", &cfg.GRPC.KeyFile)
	setBool("DISTRIBUTION_DISCOVERY_ENABLED", &cfg.Discovery.Enabled)
	setStr("DISTRIBUTION_AUTH_TARGET", &cfg.Auth.Target)
	setBool("DISTRIBUTION_AUTH_USETLS", &cfg.Auth.UseTLS)
	setBool("DISTRIBUTION_AUTH_TLS_SKIP_VERIFY", &cfg.Auth.TLSSkipVerify)
	setStr("DISTRIBUTION_DISCOVERY_TARGET", &cfg.Discovery.Target)
	setStr("DISTRIBUTION_DISCOVERY_REGISTRATION_TOKEN", &cfg.Discovery.RegistrationToken)
	setStr("DISTRIBUTION_DISCOVERY_SERVICE", &cfg.Discovery.Service)
	setStr("DISTRIBUTION_DISCOVERY_INSTANCE_ID", &cfg.Discovery.InstanceID)
	setStr("DISTRIBUTION_DISCOVERY_HTTP_ENDPOINT", &cfg.Discovery.HttpEndpoint)
	setStr("DISTRIBUTION_DISCOVERY_GRPC_ENDPOINT", &cfg.Discovery.GrpcEndpoint)
	setStr("DISTRIBUTION_DATABASE_DSN", &cfg.Database.DSN)
	setStr("DISTRIBUTION_SPHERE_TARGET", &cfg.Sphere.Target)
	setBool("DISTRIBUTION_SPHERE_USETLS", &cfg.Sphere.UseTLS)
	setBool("DISTRIBUTION_SPHERE_TLS_SKIP_VERIFY", &cfg.Sphere.TLSSkipVerify)
	setStr("DISTRIBUTION_S3_ENDPOINT", &cfg.S3.Endpoint)
	setStr("DISTRIBUTION_S3_ACCESS_KEY", &cfg.S3.AccessKey)
	setInt("DISTRIBUTION_RELEASES_ARTIFACT_RETENTION", &cfg.Releases.ArtifactRetention)
	setStr("DISTRIBUTION_S3_SECRET_KEY", &cfg.S3.SecretKey)
	setStr("DISTRIBUTION_S3_BUCKET", &cfg.S3.Bucket)
	setStr("DISTRIBUTION_S3_REGION", &cfg.S3.Region)
	setStr("DISTRIBUTION_EVENTBUS_URL", &cfg.Eventbus.URL)
	setBool("DISTRIBUTION_ANALYTICS_ENABLED", &cfg.Analytics.Enabled)
	setStr("DISTRIBUTION_ANALYTICS_SALT", &cfg.Analytics.Salt)
	setList("DISTRIBUTION_MODULES_ENABLED", &cfg.Modules.Enabled)
	setList("DISTRIBUTION_MODULES_DISABLED", &cfg.Modules.Disabled)
	setStr("DISTRIBUTION_SYNC_ENCRYPTION_KEY", &cfg.Sync.EncryptionKey)
	setStr("DISTRIBUTION_SYNC_POLL_INTERVAL", &cfg.Sync.PollInterval)
	setStr("DISTRIBUTION_SYNC_RETRY_BASE", &cfg.Sync.RetryBase)
	setStr("DISTRIBUTION_SYNC_RETRY_CAP", &cfg.Sync.RetryCap)
	setInt("DISTRIBUTION_SYNC_MAX_ATTEMPTS", &cfg.Sync.MaxAttempts)
	setInt64("DISTRIBUTION_SYNC_MAX_ASSET_BYTES", &cfg.Sync.MaxAssetBytes)
	setInt("DISTRIBUTION_SYNC_BATCH_SIZE", &cfg.Sync.BatchSize)
	setStr("DISTRIBUTION_HANDOFF_BASE_URL", &cfg.Handoff.BaseURL)
	setStr("DISTRIBUTION_HANDOFF_CLIENT_ID", &cfg.Handoff.ClientID)
	setStr("DISTRIBUTION_HANDOFF_CLIENT_SECRET", &cfg.Handoff.ClientSecret)
	setStr("DISTRIBUTION_HANDOFF_COOKIE_DOMAIN", &cfg.Handoff.CookieDomain)
	setBool("DISTRIBUTION_HANDOFF_COOKIE_SECURE", &cfg.Handoff.CookieSecure)
}

// setList reads a comma-separated environment list. An empty value leaves the
// configured list untouched.
func setList(key string, dst *[]string) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return
	}
	items := make([]string, 0, 4)
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	*dst = items
}

func setInt64(key string, dst *int64) {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
			*dst = parsed
		}
	}
}

func setStr(key string, dst *string) {
	if value := os.Getenv(key); value != "" {
		*dst = value
	}
}

func setBool(key string, dst *bool) {
	if value := os.Getenv(key); value != "" {
		*dst = value == "true" || value == "1"
	}
}

func setInt(key string, dst *int) {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			*dst = parsed
		}
	}
}
