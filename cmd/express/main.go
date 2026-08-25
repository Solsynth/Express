// Command express manages DistributionCenter marketplace products and releases.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"src.solsynth.dev/sosys/distribution/internal/marketplace"
)

const defaultDistributionBaseURL = "https://api.solian.app/dist"
const usage = `express manages Solsynth Express marketplace resources.

Usage:
  express [global flags] <resource> <action> [flags]

Authentication:
  express auth login       Browser-based OAuth device login with PKCE
  express auth status      Show credential source without exposing token
  express auth logout      Remove stored credentials

Resources and actions:
  apps list                         List public marketplace apps
  publisher-apps list               List a publisher's public apps
  product get|create|update|delete  Read or manage a product
  channel list|create|update|delete Manage product channels
  release list|manage|create|update|delete|publish|yank
  artifact upload|attach            Upload or attach release artifacts
  update check|resolve              Check for a published update
  metrics                           Read publisher metrics
  upload-key list|create|revoke     Manage product upload keys

Mutation commands accept --body FILE for a JSON request body. Use --body - to
read JSON from stdin. Tokens can come from auth login, --token, or
DISTRIBUTION_TOKEN / SPHERE_ACCESS_TOKEN.
`

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, colorize(os.Stderr, ansiRed+ansiBold, "express: "+err.Error()))
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	global := flag.NewFlagSet("express", flag.ContinueOnError)
	global.SetOutput(stderr)
	baseURL := global.String("base-url", "", "DistributionCenter base URL")
	token := global.String("token", "", "Sphere access token or product upload key")
	configPath := global.String("config", "", "credential file path")
	clientID := global.String("client-id", firstEnv("SOL_SYNTH_EXPRESS_CLIENT_ID", "OAUTH_CLIENT_ID"), "registered OAuth public client ID")
	discoveryURL := global.String("oidc-discovery-url", firstEnv("SOL_SYNTH_EXPRESS_OIDC_DISCOVERY_URL", "OIDC_DISCOVERY_URL", defaultOIDCDiscoveryURL), "OpenID Connect discovery URL")
	scope := global.String("scope", "*", "OAuth scope")
	timeout := global.Duration("timeout", 30*time.Second, "HTTP request timeout (0 disables timeout)")
	help := global.Bool("help", false, "show help")
	if err := global.Parse(args); err != nil {
		return err
	}
	remaining := global.Args()
	if *help || len(remaining) == 0 || remaining[0] == "help" {
		fmt.Fprint(stdout, usage)
		return nil
	}

	if remaining[0] == "auth" {
		return authCommand(remaining[1:], stdout, stderr, *configPath, *baseURL, *token, *clientID, *discoveryURL, *scope)
	}

	storePath, err := authStorePath(*configPath)
	if err != nil {
		return err
	}
	stored, err := loadStoredAuth(storePath)
	if err != nil {
		return err
	}
	resolvedBaseURL := strings.TrimSpace(*baseURL)
	if resolvedBaseURL == "" {
		resolvedBaseURL = firstEnv("DISTRIBUTION_API_URL", "DISTRIBUTION_BASE_URL", "SOL_SYNTH_EXPRESS_URL")
	}
	if resolvedBaseURL == "" {
		resolvedBaseURL = stored.BaseURL
	}
	if resolvedBaseURL == "" {
		resolvedBaseURL = defaultDistributionBaseURL
	}
	resolvedToken := strings.TrimSpace(*token)
	if resolvedToken == "" {
		resolvedToken = firstEnv("DISTRIBUTION_TOKEN", "SPHERE_ACCESS_TOKEN")
	}
	if resolvedToken == "" {
		resolvedToken = stored.Token
	}
	client, err := marketplace.NewClient(resolvedBaseURL, resolvedToken)
	if err != nil {
		return err
	}
	ctx, cancel := marketplace.ContextWithTimeout(context.Background(), *timeout)
	defer cancel()
	return dispatch(ctx, client, remaining, stdout, stderr)
}

func firstEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func dispatch(ctx context.Context, client *marketplace.Client, args []string, stdout, stderr io.Writer) error {
	switch args[0] {
	case "apps":
		return appsCommand(ctx, client, args[1:], stdout, stderr)
	case "publisher-apps":
		return publisherAppsCommand(ctx, client, args[1:], stdout, stderr)
	case "product":
		return productCommand(ctx, client, args[1:], stdout, stderr)
	case "channel":
		return channelCommand(ctx, client, args[1:], stdout, stderr)
	case "release":
		return releaseCommand(ctx, client, args[1:], stdout, stderr)
	case "artifact":
		return artifactCommand(ctx, client, args[1:], stdout, stderr)
	case "update":
		return updateCommand(ctx, client, args[1:], stdout, stderr)
	case "metrics":
		return metricsCommand(ctx, client, args[1:], stdout, stderr)
	case "upload-key":
		return uploadKeyCommand(ctx, client, args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown resource %q; use --help", args[0])
	}
}

func newCommand(name string, args []string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

func requireAction(args []string, resource string) (string, []string, error) {
	if len(args) == 0 {
		return "", nil, fmt.Errorf("%s action is required", resource)
	}
	return args[0], args[1:], nil
}

func requireValue(name, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("--%s is required", name)
	}
	return nil
}

func bodyFromFlag(path string) ([]byte, error) {
	if path == "" {
		return nil, errors.New("--body is required")
	}
	var data []byte
	var err error
	if path == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, fmt.Errorf("read body %s: %w", path, err)
	}
	if !json.Valid(data) {
		return nil, fmt.Errorf("body %s is not valid JSON", path)
	}
	return data, nil
}

func outputJSON(stdout io.Writer, raw []byte) error {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, raw, "", "  "); err == nil {
		_, err = fmt.Fprintln(stdout, pretty.String())
		return err
	}
	_, err := fmt.Fprintln(stdout, string(raw))
	return err
}

func outputOK(stdout io.Writer) error { return outputJSON(stdout, []byte(`{"ok":true}`)) }

func appsCommand(ctx context.Context, client *marketplace.Client, args []string, stdout, stderr io.Writer) error {
	action, rest, err := requireAction(args, "apps")
	if err != nil {
		return err
	}
	if action != "list" {
		return fmt.Errorf("unknown apps action %q", action)
	}
	fs := newCommand("apps list", rest, stderr)
	sortBy := fs.String("sort", "updated_at", "sort by updated_at, name, or created_at")
	order := fs.String("order", "desc", "sort order: asc or desc")
	limit := fs.Int("limit", 20, "number of apps")
	offset := fs.Int("offset", 0, "pagination offset")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	query := url.Values{"sort": {*sortBy}, "order": {*order}, "limit": {strconv.Itoa(*limit)}, "offset": {strconv.Itoa(*offset)}}
	raw, err := client.ListMarketplaceApps(ctx, query)
	if err != nil {
		return err
	}
	return outputJSON(stdout, raw)
}

func publisherAppsCommand(ctx context.Context, client *marketplace.Client, args []string, stdout, stderr io.Writer) error {
	action, rest, err := requireAction(args, "publisher-apps")
	if err != nil {
		return err
	}
	if action != "list" {
		return fmt.Errorf("unknown publisher-apps action %q", action)
	}
	fs := newCommand("publisher-apps list", rest, stderr)
	publisher := fs.String("publisher", "", "publisher name")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if err := requireValue("publisher", *publisher); err != nil {
		return err
	}
	raw, err := client.ListPublisherApps(ctx, *publisher)
	if err != nil {
		return err
	}
	return outputJSON(stdout, raw)
}

func productCommand(ctx context.Context, client *marketplace.Client, args []string, stdout, stderr io.Writer) error {
	action, rest, err := requireAction(args, "product")
	if err != nil {
		return err
	}
	fs := newCommand("product "+action, rest, stderr)
	id := fs.String("id", "", "product ID")
	publisher := fs.String("publisher", "", "publisher name (create only)")
	bodyPath := fs.String("body", "", "JSON request body file, or - for stdin")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	switch action {
	case "get":
		if err := requireValue("id", *id); err != nil {
			return err
		}
		raw, err := client.GetProduct(ctx, *id)
		if err != nil {
			return err
		}
		return outputJSON(stdout, raw)
	case "create":
		prompted, err := valueOrPrompt(stderr, "publisher", *publisher, "Publisher name")
		if err != nil {
			return err
		}
		*publisher = prompted
		body, err := bodyFromFlagOrPrompt(*bodyPath, stderr, promptProductBody)
		if err != nil {
			return err
		}
		raw, err := client.CreateProduct(ctx, *publisher, body)
		if err != nil {
			return err
		}
		return outputJSON(stdout, raw)
	case "update":
		if err := requireValue("id", *id); err != nil {
			return err
		}
		body, err := bodyFromFlag(*bodyPath)
		if err != nil {
			return err
		}
		raw, err := client.UpdateProduct(ctx, *id, body)
		if err != nil {
			return err
		}
		return outputJSON(stdout, raw)
	case "delete":
		if err := requireValue("id", *id); err != nil {
			return err
		}
		if err := client.DeleteProduct(ctx, *id); err != nil {
			return err
		}
		return outputOK(stdout)
	default:
		return fmt.Errorf("unknown product action %q", action)
	}
}

func channelCommand(ctx context.Context, client *marketplace.Client, args []string, stdout, stderr io.Writer) error {
	action, rest, err := requireAction(args, "channel")
	if err != nil {
		return err
	}
	fs := newCommand("channel "+action, rest, stderr)
	product := fs.String("product", "", "product ID")
	channel := fs.String("id", "", "channel ID")
	bodyPath := fs.String("body", "", "JSON request body file, or - for stdin")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	promptedProduct, err := valueOrPrompt(stderr, "product", *product, "Product ID")
	if err != nil {
		return err
	}
	*product = promptedProduct
	switch action {
	case "list":
		raw, err := client.ListChannels(ctx, *product)
		if err != nil {
			return err
		}
		return outputJSON(stdout, raw)
	case "create":
		body, err := bodyFromFlagOrPrompt(*bodyPath, stderr, promptChannelBody)
		if err != nil {
			return err
		}
		raw, err := client.CreateChannel(ctx, *product, body)
		if err != nil {
			return err
		}
		return outputJSON(stdout, raw)
	case "update":
		if err := requireValue("id", *channel); err != nil {
			return err
		}
		body, err := bodyFromFlag(*bodyPath)
		if err != nil {
			return err
		}
		raw, err := client.UpdateChannel(ctx, *product, *channel, body)
		if err != nil {
			return err
		}
		return outputJSON(stdout, raw)
	case "delete":
		if err := requireValue("id", *channel); err != nil {
			return err
		}
		if err := client.DeleteChannel(ctx, *product, *channel); err != nil {
			return err
		}
		return outputOK(stdout)
	default:
		return fmt.Errorf("unknown channel action %q", action)
	}
}

func releaseCommand(ctx context.Context, client *marketplace.Client, args []string, stdout, stderr io.Writer) error {
	action, rest, err := requireAction(args, "release")
	if err != nil {
		return err
	}
	fs := newCommand("release "+action, rest, stderr)
	product := fs.String("product", "", "product ID")
	release := fs.String("id", "", "release ID or version")
	bodyPath := fs.String("body", "", "JSON request body file, or - for stdin")
	channel := fs.String("channel", "", "channel filter")
	platform := fs.String("platform", "", "platform filter")
	architecture := fs.String("architecture", "", "architecture filter")
	limit := fs.Int("limit", 20, "number of releases")
	offset := fs.Int("offset", 0, "pagination offset")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	promptedProduct, err := valueOrPrompt(stderr, "product", *product, "Product ID")
	if err != nil {
		return err
	}
	*product = promptedProduct
	switch action {
	case "list", "manage":
		query := url.Values{"limit": {strconv.Itoa(*limit)}, "offset": {strconv.Itoa(*offset)}}
		if *channel != "" {
			query.Set("channel", *channel)
		}
		if *platform != "" {
			query.Set("platform", *platform)
		}
		if *architecture != "" {
			query.Set("architecture", *architecture)
		}
		raw, err := client.ListReleases(ctx, *product, action == "manage", query)
		if err != nil {
			return err
		}
		return outputJSON(stdout, raw)
	case "create":
		body, err := bodyFromFlagOrPrompt(*bodyPath, stderr, promptReleaseBody)
		if err != nil {
			return err
		}
		raw, err := client.CreateRelease(ctx, *product, body)
		if err != nil {
			return err
		}
		return outputJSON(stdout, raw)
	case "update":
		if err := requireValue("id", *release); err != nil {
			return err
		}
		body, err := bodyFromFlag(*bodyPath)
		if err != nil {
			return err
		}
		raw, err := client.UpdateRelease(ctx, *product, *release, body)
		if err != nil {
			return err
		}
		return outputJSON(stdout, raw)
	case "delete":
		if err := requireValue("id", *release); err != nil {
			return err
		}
		if err := client.DeleteRelease(ctx, *product, *release); err != nil {
			return err
		}
		return outputOK(stdout)
	case "publish":
		if err := requireValue("id", *release); err != nil {
			return err
		}
		raw, err := client.PublishRelease(ctx, *product, *release)
		if err != nil {
			return err
		}
		return outputJSON(stdout, raw)
	case "yank":
		if err := requireValue("id", *release); err != nil {
			return err
		}
		raw, err := client.YankRelease(ctx, *product, *release)
		if err != nil {
			return err
		}
		return outputJSON(stdout, raw)
	default:
		return fmt.Errorf("unknown release action %q", action)
	}
}

func artifactCommand(ctx context.Context, client *marketplace.Client, args []string, stdout, stderr io.Writer) error {
	action, rest, err := requireAction(args, "artifact")
	if err != nil {
		return err
	}
	fs := newCommand("artifact "+action, rest, stderr)
	product := fs.String("product", "", "product ID")
	release := fs.String("release", "", "release ID or version")
	bodyPath := fs.String("body", "", "JSON request body file, or - for stdin")
	filePath := fs.String("file", "", "local artifact file")
	fileName := fs.String("file-name", "", "artifact file name")
	mimeType := fs.String("mime-type", "", "artifact MIME type")
	channel := fs.String("channel", "stable", "channel for a new draft")
	version := fs.String("version", "", "release version")
	platform := fs.String("platform", "", "artifact platform")
	architecture := fs.String("architecture", "", "artifact architecture")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	promptedProduct, err := valueOrPrompt(stderr, "product", *product, "Product ID")
	if err != nil {
		return err
	}
	*product = promptedProduct
	switch action {
	case "attach":
		if err := requireValue("release", *release); err != nil {
			return err
		}
		body, err := bodyFromFlag(*bodyPath)
		if err != nil {
			return err
		}
		raw, err := client.AttachArtifact(ctx, *product, *release, body)
		if err != nil {
			return err
		}
		return outputJSON(stdout, raw)
	case "upload":
		promptedFile, err := valueOrPrompt(stderr, "file", *filePath, "Artifact file path")
		if err != nil {
			return err
		}
		*filePath = promptedFile
		promptedVersion, err := valueOrPrompt(stderr, "version", *version, "Release version")
		if err != nil {
			return err
		}
		*version = promptedVersion
		if *fileName == "" {
			*fileName = filepath.Base(*filePath)
		}
		if *mimeType == "" {
			*mimeType = mime.TypeByExtension(filepath.Ext(*fileName))
			if *mimeType == "" {
				*mimeType = "application/octet-stream"
			}
		}
		file, err := os.Open(*filePath)
		if err != nil {
			return fmt.Errorf("open artifact: %w", err)
		}
		defer file.Close()
		digest, size, err := marketplace.SHA256File(file)
		if err != nil {
			return err
		}
		_ = statusLine(stderr, ansiCyan, fmt.Sprintf("Preparing upload for %s (%d bytes)...", *fileName, size))
		preparation, err := client.PrepareUpload(ctx, *product, map[string]any{"file_name": *fileName, "mime_type": *mimeType, "sha256": digest, "channel": *channel, "version": *version})
		if err != nil {
			return err
		}
		if err := client.PutArtifact(ctx, preparation.UploadURL, *fileName, *mimeType, digest, file, size); err != nil {
			return err
		}
		_ = statusLine(stderr, ansiGreen, "Artifact uploaded. Attaching it to the release...")
		attachBody, err := json.Marshal(map[string]any{"object_key": preparation.ObjectKey, "platform": *platform, "architecture": *architecture})
		if err != nil {
			return err
		}
		raw, err := client.AttachArtifact(ctx, *product, preparation.ReleaseID, attachBody)
		if err != nil {
			return err
		}
		_ = statusLine(stderr, ansiGreen+ansiBold, "Artifact attached successfully.")
		return outputJSON(stdout, raw)
	default:
		return fmt.Errorf("unknown artifact action %q", action)
	}
}

func updateCommand(ctx context.Context, client *marketplace.Client, args []string, stdout, stderr io.Writer) error {
	action, rest, err := requireAction(args, "update")
	if err != nil {
		return err
	}
	fs := newCommand("update "+action, rest, stderr)
	product := fs.String("product", "", "product ID")
	version := fs.String("version", "", "current client version")
	channel := fs.String("channel", "stable", "release channel")
	platform := fs.String("os", "", "operating system")
	architecture := fs.String("architecture", "", "CPU architecture")
	installation := fs.String("installation-id", "", "installation UUID")
	osVersion := fs.String("os-version", "", "operating system version")
	clientVersion := fs.String("client-version", "", "client version")
	locale := fs.String("locale", "", "BCP-47 locale")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if err := requireValue("product", *product); err != nil {
		return err
	}
	if action != "check" && action != "resolve" {
		return fmt.Errorf("unknown update action %q", action)
	}
	if err := requireValue("version", *version); err != nil {
		return err
	}
	if err := requireValue("channel", *channel); err != nil {
		return err
	}
	if err := requireValue("os", *platform); err != nil {
		return err
	}
	if err := requireValue("architecture", *architecture); err != nil {
		return err
	}
	if action == "check" {
		raw, err := client.CheckUpdate(ctx, *product, marketplace.UpdateCheckRequest{Version: *version, Channel: *channel, OS: *platform, Architecture: *architecture, InstallationID: *installation, OSVersion: *osVersion, ClientVersion: *clientVersion, Locale: *locale})
		if err != nil {
			return err
		}
		return outputJSON(stdout, raw)
	}
	query := url.Values{"current_version": {*version}, "channel": {*channel}, "os": {*platform}, "architecture": {*architecture}}
	if *installation != "" {
		query.Set("installation_id", *installation)
	}
	if *osVersion != "" {
		query.Set("os_version", *osVersion)
	}
	if *clientVersion != "" {
		query.Set("client_version", *clientVersion)
	}
	if *locale != "" {
		query.Set("locale", *locale)
	}
	raw, err := client.ResolveUpdate(ctx, *product, query)
	if err != nil {
		return err
	}
	return outputJSON(stdout, raw)
}

func metricsCommand(ctx context.Context, client *marketplace.Client, args []string, stdout, stderr io.Writer) error {
	fs := newCommand("metrics", args, stderr)
	product := fs.String("product", "", "product ID")
	from := fs.String("from", "", "RFC3339 start time")
	to := fs.String("to", "", "RFC3339 end time")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := requireValue("product", *product); err != nil {
		return err
	}
	query := url.Values{}
	if *from != "" {
		query.Set("from", *from)
	}
	if *to != "" {
		query.Set("to", *to)
	}
	raw, err := client.Metrics(ctx, *product, query)
	if err != nil {
		return err
	}
	return outputJSON(stdout, raw)
}

func uploadKeyCommand(ctx context.Context, client *marketplace.Client, args []string, stdout, stderr io.Writer) error {
	action, rest, err := requireAction(args, "upload-key")
	if err != nil {
		return err
	}
	fs := newCommand("upload-key "+action, rest, stderr)
	product := fs.String("product", "", "product ID")
	key := fs.String("id", "", "upload key ID")
	name := fs.String("name", "", "upload key name")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if err := requireValue("product", *product); err != nil {
		return err
	}
	switch action {
	case "list":
		raw, err := client.ListUploadKeys(ctx, *product)
		if err != nil {
			return err
		}
		return outputJSON(stdout, raw)
	case "create":
		if err := requireValue("name", *name); err != nil {
			return err
		}
		body, err := json.Marshal(map[string]string{"name": *name})
		if err != nil {
			return err
		}
		raw, err := client.CreateUploadKey(ctx, *product, body)
		if err != nil {
			return err
		}
		return outputJSON(stdout, raw)
	case "revoke":
		if err := requireValue("id", *key); err != nil {
			return err
		}
		if err := client.RevokeUploadKey(ctx, *product, *key); err != nil {
			return err
		}
		return outputOK(stdout)
	default:
		return fmt.Errorf("unknown upload-key action %q", action)
	}
}
