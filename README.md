# DistributionCenter

DistributionCenter is the release control plane for Solsynth Express products. It stores publisher-owned products, channels, release metadata, and artifact references exposed through the marketplace API.

The repository also contains the `express` CLI for managing marketplace products and releases.

## Requirements

- Go 1.26.5 or newer
- A running DistributionCenter API
- A Sphere access token for publisher operations
- A product upload key for CI artifact upload and restricted publishing workflows

## Build and run

Build the service and CLI:

```bash
make build
make build-cli
```

The CLI binary is written to:

```text
bin/express
```
Install directly with Go:

```bash
go install src.solsynth.dev/solsynth/distribution/cmd/express@latest
```

This installs `express` into `GOBIN`, or `GOPATH/bin` when `GOBIN` is not set. Ensure that directory is on your `PATH`.

From a local checkout:

```bash
go install ./cmd/express
```

Show command help without configuring a server:

```bash
bin/express --help
```

The default API base URL is `https://api.solian.app/dist`. Do not include `/api` in the URL; override the default with `--base-url`, `DISTRIBUTION_API_URL`, `DISTRIBUTION_BASE_URL`, or `SOL_SYNTH_EXPRESS_URL`.

```bash
bin/express apps list

bin/express \
  --base-url https://distribution.example.com \
  apps list
```

## Authentication

### Interactive login

```bash
express auth login
```

The default login uses Solian's OAuth device authorization flow with PKCE. It prints a verification URL and user code; finish sign-in in a browser and `express` waits for the access token. The requested OAuth scope is `*`.

The OAuth public client ID must be registered with the identity service. Provide it with `--client-id` or `SOL_SYNTH_EXPRESS_CLIENT_ID`:

```bash
express auth login \
  --base-url https://distribution.example.com \
  --client-id YOUR_REGISTERED_CLIENT_ID
```

The discovery document defaults to `https://solian.app/.well-known/openid-configuration`. Override it with `--oidc-discovery-url` or `SOL_SYNTH_EXPRESS_OIDC_DISCOVERY_URL`.

For scripts, direct token login remains available:

```bash
express auth login \
  --base-url https://distribution.example.com \
  --token "$SPHERE_ACCESS_TOKEN"
```

Stored credentials are kept at:

```text
~/Library/Application Support/dev.solsynth.express/config.json
```

The file is created with owner-only permissions. Override the path with either option:

```bash
express --config ~/.config/express/auth.json auth login
SOL_SYNTH_EXPRESS_CONFIG=~/.config/express/auth.json express auth status
```

Inspect authentication state without printing the token:

```bash
express auth status
```

Remove stored credentials:

```bash
express auth logout
```

Credential precedence is:

1. `--token` and `--base-url`
2. Environment variables
3. Stored credentials

Supported environment variables:

| Variable | Purpose |
| --- | --- |
| `DISTRIBUTION_API_URL` | DistributionCenter base URL |
| `DISTRIBUTION_BASE_URL` | Alternate base URL variable |
| `SOL_SYNTH_EXPRESS_URL` | Solsynth Express base URL |
| `DISTRIBUTION_TOKEN` | Bearer token or product upload key |
| `SPHERE_ACCESS_TOKEN` | Sphere bearer token |
| `SOL_SYNTH_EXPRESS_CLIENT_ID` | Registered OAuth public client ID |
| `SOL_SYNTH_EXPRESS_OIDC_DISCOVERY_URL` | OpenID Connect discovery URL |
| `OAUTH_CLIENT_ID` | Alternate OAuth client ID variable |
| `OIDC_DISCOVERY_URL` | Alternate discovery URL variable |
| `SOL_SYNTH_EXPRESS_CONFIG` | Credential file path |

## Complete publisher workflow

The CLI supports the complete publisher release lifecycle:

1. Create or update the product.
2. Create a custom channel when needed.
3. Create a draft release.
4. Upload and attach one or more platform artifacts.
5. Publish the release.

Example:

```bash
express auth login \
  --client-id YOUR_REGISTERED_CLIENT_ID

express product create \
  --publisher acme \
  --body product.json

express channel create \
  --product PRODUCT_ID \
  --body channel.json

express release create \
  --product PRODUCT_ID \
  --body release.json

express artifact upload \
  --product PRODUCT_ID \
  --version 1.4.0 \
  --file ./client.tar.gz \
  --platform macos \
  --architecture arm64

express release publish \
  --product PRODUCT_ID \
  --id RELEASE_ID
```

`artifact upload` automatically prepares the upload URL, calculates SHA-256, uploads the bytes, and attaches the artifact. Repeat it for each platform and architecture before publishing.

## Interactive experience

When connected to a terminal, `express` uses concise prompts for product, channel, and release creation when `--body` is omitted. Artifact uploads report preparation, upload, and attachment progress.

Omitting `--product` on commands that need it (channel, release, artifact, update, metrics, upload-key) opens an interactive filterable product picker. Press `/` to filter by name, arrow keys to navigate, Enter to select, Esc to cancel. Omitting `--version` on artifact upload reads the current git tag.

Informational output is colorized automatically in terminals. JSON responses remain plain on stdout for scripting. Disable color with `NO_COLOR=1` or force a mode with:

```bash
EXPRESS_COLOR=always express apps list
EXPRESS_COLOR=never express apps list
```

The CLI intentionally uses text-only output without emoji.

## Marketplace commands

All API responses are printed as formatted JSON.

### Public catalog

```bash
express apps list \
  --sort updated_at \
  --order desc \
  --limit 20 \
  --offset 0

express publisher-apps list --publisher acme
```

### Products

Mutation bodies are read from a JSON file. Use `--body -` to read from standard input.

```bash
express product get --id PRODUCT_ID

express product create \
  --publisher acme \
  --body product.json

express product update \
  --id PRODUCT_ID \
  --body product.json

express product delete --id PRODUCT_ID
```

Example `product.json`:

```json
{
  "slug": "desktop-client",
  "name": "Desktop Client",
  "names": {
    "en-US": "Desktop Client",
    "zh-CN": "桌面客户端"
  },
  "description": "Desktop client",
  "descriptions": {
    "en-US": "Desktop client"
  },
  "previews": []
}
```

### Channels

```bash
express channel list --product PRODUCT_ID
express channel create --product PRODUCT_ID --body channel.json
express channel update --product PRODUCT_ID --id CHANNEL_ID --body channel.json
express channel delete --product PRODUCT_ID --id CHANNEL_ID
```

Example `channel.json`:

```json
{
  "name": "preview",
  "display_name": "Preview",
  "artifact_retention": 1
}
```

Built-in channels are `stable`, `beta`, `nightly`, and `rolling`. Custom channels must be created before they are assigned to releases.

### Releases

```bash
express release list \
  --product PRODUCT_ID \
  --channel stable \
  --platform macos \
  --architecture arm64

express release manage \
  --product PRODUCT_ID \
  --channel stable

express release create \
  --product PRODUCT_ID \
  --body release.json

express release update \
  --product PRODUCT_ID \
  --id RELEASE_ID \
  --body release-update.json

express release publish --product PRODUCT_ID --id RELEASE_ID
express release yank --product PRODUCT_ID --id RELEASE_ID
express release delete --product PRODUCT_ID --id RELEASE_ID
```

Publishing requires at least one complete artifact. Draft releases can be deleted; published releases should be yanked instead.

### Artifact upload

The upload command performs the complete S3 workflow:

1. Calculate the lowercase SHA-256 digest.
2. Request a presigned upload URL.
3. Upload the file with the required metadata headers.
4. Attach the object to the release.

`--version` defaults to the current git tag (leading `v` stripped) when
omitted. `--platform` and `--architecture` default from the build host when
omitted (`darwin` → `macos`, `amd64` → `x86_64`, and so on).

```bash
express artifact upload \
  --product PRODUCT_ID \
  --version 1.4.0 \
  --file ./client.tar.gz \
  --platform macos \
  --architecture arm64
```

From a tagged git checkout, the same command simplifies to:

```bash
express artifact upload \
  --product PRODUCT_ID \
  --file ./client.tar.gz
```

Use `--channel` when the version does not already exist:

```bash
express artifact upload \
  --product PRODUCT_ID \
  --version 1.4.0-beta.1 \
  --channel beta \
  --file ./client.tar.gz \
  --platform macos \
  --architecture arm64
```

To attach an external HTTP(S) artifact or an already-uploaded object, provide the API body directly:

```bash
express artifact attach \
  --product PRODUCT_ID \
  --release RELEASE_ID \
  --body artifact.json
```

Example `artifact.json` for an external download:

```json
{
  "download_url": "https://downloads.example.com/client.tar.gz",
  "file_name": "client.tar.gz",
  "mime_type": "application/gzip",
  "size": 123456,
  "hash": "sha256...",
  "platform": "macos",
  "architecture": "arm64"
}
```

### Update checks and metrics

```bash
express update check \
  --product PRODUCT_ID \
  --version 1.3.0 \
  --channel stable \
  --os macos \
  --architecture arm64 \
  --installation-id INSTALLATION_UUID \
  --os-version 14.5 \
  --client-version 2.8.1 \
  --locale en-US

express update resolve \
  --product PRODUCT_ID \
  --version 1.3.0 \
  --channel stable \
  --os macos \
  --architecture arm64

express metrics --product PRODUCT_ID
```

### Upload keys

Upload keys are product-scoped. The plaintext key is returned only during creation.

```bash
express upload-key create \
  --product PRODUCT_ID \
  --name "GitHub Actions"

express upload-key list --product PRODUCT_ID
express upload-key revoke --product PRODUCT_ID --id KEY_ID
```

Use the returned key through `--token`, `DISTRIBUTION_TOKEN`, or `SPHERE_ACCESS_TOKEN` for upload workflows.

## Development

Run focused CLI tests:

```bash
go test ./cmd/distributionctl ./internal/marketplace
```

Run the complete test and vet checks:

```bash
go test ./...
go vet ./...
```

The marketplace API contract is documented in [`docs/MARKETPLACE_API.md`](docs/MARKETPLACE_API.md).

## Web console

`web/` is the publisher console: the marketplace, a publisher's products, and
the release console that uploads artifacts and publishes versions. It is a Vue
3 + Vite single-page app served by DistributionCenter itself, laid out as a top
bar plus a publisher/product sidebar with one wide work column.

Build it into `web/dist` and point the service at it:

```bash
make web
```

```toml
[web]
distPath = "web/dist"
```

The service then serves the console at `/` and keeps `/api`, `/artifacts`, and
the health routes for itself. Leaving `distPath` empty keeps the deployment
API-only. The Docker image ships the console at `/app/web/dist`, so a container
needs no extra volume.

Two processes for development — the Go API on `:8080`, Vite on `:5173` with
every backend path proxied:

```bash
make run          # terminal one
make web-dev      # terminal two
```

The console reads the session from the `AuthToken` cookie Stargate sets on the
shared cookie domain, so a deployment reaches it signed in. `VITE_AUTH_URL`
overrides the identity host's sign-in page for the signed-out hand-off;
`VITE_API_TARGET` overrides the proxy target.

Console design and copy conventions are documented in
[`docs/DESIGN_SYSTEM.md`](docs/DESIGN_SYSTEM.md).
