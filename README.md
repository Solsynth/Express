# DistributionCenter

DistributionCenter is the release control plane for Solsynth Express products. It stores publisher-owned products, channels, release metadata, and artifact references exposed through the marketplace API.

It also hosts the developer portal: developers, projects, custom apps, app
products, bot accounts, and mini apps. See `docs/DEVELOPER_PORTAL.md` for the
route contract and `[develop]` configuration.

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

Two processes for console development, with Vite on `:5173` proxying every
backend path:

```bash
make run          # terminal one, Go API on :8080
make web-dev      # terminal two, Vite on :5173
```

`VITE_API_TARGET` overrides the dev proxy target and `VITE_AUTH_URL` the
identity host's sign-in page. Product art is read from the drive, so two more
point a dev or self-hosted console at its own deployment: `VITE_SNFS_API_URL`
(the origin that answers `/drive/files/{id}`, default `https://api.solian.app`)
and `VITE_SNFS_URL` (the origin serving the picker page, default
`https://fs.solian.app`).

Product art is drawn by `sk-media` from `@solsynth/sunkenland`, a public npm
package — no registry token is needed.

Run the console's own checks from `web/`:

```bash
bun run type-check && bun run test:unit --run && bun run build-only
```

The marketplace API contract is documented in [`docs/MARKETPLACE_API.md`](docs/MARKETPLACE_API.md).

## Developer portal

The portal replaces the standalone Develop service and keeps its HTTP paths and
permission nodes, so existing clients and grants carry over. Custom apps moved
to `/api/custom-apps` because the marketplace keeps `/api/apps`.

It is compiled in as five modules and enabled by default:

```toml
[modules]
disabled = [
  "app.solian.develop.developers",
  "app.solian.develop.apps",
  "app.solian.develop.bots",
  "app.solian.develop.miniapps",
  "app.solian.develop.products",
]
```

`GET /api/modules` lists which ones a process mounted. `DyCustomAppService` is
served on the gRPC port while `app.solian.develop.apps` is enabled, which is the
contract Passport and Wallet already call.

To take over an existing Develop deployment, import its tables:

```bash
PGPASSWORD=… PGDRY=1 ./scripts/import-develop.sh   # dry run
PGPASSWORD=… ./scripts/import-develop.sh           # for real
```

The importer is idempotent, never truncates, skips a child whose parent is
absent, and records per-table counters in `module_import_state`. See
`docs/DEVELOPER_PORTAL.md`.

## Web console

`web/` is the publisher console: the marketplace, a publisher's products, and
the release console that uploads artifacts and publishes versions. It is a Vue
3 + Vite single-page app served by DistributionCenter itself: a persistent top
bar, and a publisher/product sidebar on the console routes where the
navigation tree exists.
It also carries the developer portal: the publishers the account enrolled as
developers, their projects, and each project's custom apps, app products, bot
accounts, and mini apps. The portal is gated on the
`app.solian.develop.*` module set, so a deployment with every module disabled
never mounts a portal route and the console hides the entry point.

Build it into `web/dist`:

```bash
make web
```

```toml
[web]
distPath = "web/dist"
```

The service then serves the console at `/` and keeps `/api`, `/artifacts`, and
the health routes for itself. Leaving `distPath` empty keeps the deployment
API-only. Startup logs which state it is in — `console mounted`,
`console disabled`, or `console not mounted` with the reason — so a missing
build does not read as a routing bug.

### Container

The image builds the console in its own stage and places it at `/app/web/dist`,
then runs from `/app` so the default `distPath = "web/dist"` resolves without
extra configuration. Mount a real config at deploy time; the baked
`config.example.toml` is a template and still needs `database.dsn`, the service
targets, and an S3 bucket:

```bash
docker run -p 8080:8080 -p 9090:9090 \
  -e CONFIG_PATH=/app/config.toml \
  -v "$PWD/config.toml:/app/config.toml" \
  ghcr.io/$PACKAGE_OWNER/distribution
```

`CONFIG_PATH` is required here: the loader's default file name is
`config.example.toml` (the baked template), so a mounted `config.toml` is only
read when the variable names it. Mount your file over
`/app/config.example.toml` instead if you would rather not set the variable.
Every setting also has an environment override, so a deployment can skip the
file entirely — `DISTRIBUTION_DATABASE_DSN`, `DISTRIBUTION_AUTH_TARGET`,
`DISTRIBUTION_SPHERE_TARGET`, the `DISTRIBUTION_S3_*` keys,
`DISTRIBUTION_WEB_DIST_PATH` for the console location, and
`DISTRIBUTION_HANDOFF_*` for sign-in.

### Sign-in

The console never shares a cookie with the identity host: a cookie has one
`Domain` attribute and cannot span `id.solian.app` and `dx.solsynth.dev`. The
session moves by a one-time code instead.

1. "Sign in" opens `{handoff.baseUrl}/sign-in?redirectUri={this page}`.
2. The identity console signs the person in on its own domain and redirects
   back to this page with `?code=…`.
3. The console POSTs that code to `/api/auth/session`; this server redeems it
   at the issuer and sets its **own** `AuthToken` and `RefreshToken` cookies on
   `handoff.cookieDomain` — this deployment's domain, not the identity host's.
4. The console strips `code` from the address bar immediately. The tokens never
   appear in a URL.

An expired access token is renewed in place: a 401 makes the console call
`/api/auth/refresh` once (shared by concurrent requests) and repeat the request,
so a short token lifetime does not end a session. `/api/auth/logout` clears this
console's cookies. All three routes answer `503` while `[handoff]` is
unconfigured.

```toml
[handoff]
baseUrl = "https://id.solian.app"
clientId = "app.solian.express"
clientSecret = ""            # empty makes this a public client (PKCE only)
cookieDomain = "dx.solsynth.dev"
cookieSecure = true
```

The client id must be registered with the issuer, with this console's host
listed among its allowed redirect hosts. `DISTRIBUTION_HANDOFF_*` overrides
every field. Bearer-token clients are unaffected: the API still accepts
`Authorization: Bearer` and the cookie on every other route.

### File picker

The product-art picker is an iframe served from the drive's own host, which is
not this console's registrable domain. A `SameSite=Lax` cookie is not sent in
a cross-site iframe, so the picker would always read as signed out, and it
cannot complete a sign-in redirect either — the identity host refuses to be
framed.

The console asks the issuer for a single-use code bound to the **picker's**
client and hands only that to the frame, which redeems it on its own origin
for a bearer token it keeps in memory. A code lives for `[handoff]`'s code
lifetime (60s by default) and is single-use, and the console never holds a
token for the drive — its own session stays an HttpOnly cookie, as before.

```toml
[picker]
clientId = "app.solian.snfs"                   # the picker host's client
frameUrl = "https://fs.solian.app/picker"      # the page to embed
```

Both the client id and the frame URL are required together, and `handoff.baseUrl`
must be set: it names the issuer that redeems the code. The client must be
registered with the issuer with `frameUrl`'s host among its allowed redirect
hosts. Leaving `clientId` empty disables the feature, and the picker then
authenticates by cookie — which works when it shares a site with this console.

`POST /api/picker/code` answers `{code, expiresIn, frameUrl}` and never a
token. It is authenticated by this console's own cookie and refuses a caller
without one. The frame URL is returned rather than taken from the SPA's
build-time value because the issuer validates the redirect target when it
issues the code, and a frame opened at any other address would fail at
redemption.

Console design and copy conventions are documented in
[`docs/DESIGN_SYSTEM.md`](docs/DESIGN_SYSTEM.md).
