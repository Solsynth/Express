# Developer portal

DistributionCenter hosts the developer portal: the surface where a publisher
enrolls as a developer, organizes projects, and publishes custom apps, app
products, bot accounts, and mini apps. It replaces the standalone Develop
service, and keeps the same permission nodes and HTTP paths, so existing
clients and grants carry over.

The portal is opt-in per module. A deployment that disables every
`app.solian.develop.*` module never dials the portal's extra gRPC dependencies
and never migrates its tables.

## Modules

| Module ID | Capability | Owns |
|---|---|---|
| `app.solian.develop.developers` | `developers` | developers, projects, quota |
| `app.solian.develop.apps` | `custom-apps` | custom apps, secrets, board widgets |
| `app.solian.develop.bots` | `bots` | bot registry, API keys, chat config |
| `app.solian.develop.miniapps` | `miniapps` | mini app metadata and packages |
| `app.solian.develop.products` | `app-products` | app products and sale state |

`apps` requires `developers`; `bots`, `miniapps` and `products` require their
parent module. Disabling a parent removes its dependents with a warning, so a
half-composed portal is not reachable.

## Identity model

The portal owns no accounts and no publishers:

- **Publishers** live in Sphere. `GET /api/publishers/...` and
  `IsPublisherMember` decide who may act for a publisher.
- **Accounts** live in Stargate. The `AuthToken` cookie or a bearer token
  authenticates every console call.
- **Bots** are accounts in Stargate's `DyBotAccountReceiverService`; the portal
  keeps the local registry row and chat config.
- **Board payloads** are written by Passport through `DyProfileService`.

A developer is one publisher enrolled in the program. Enrollment requires the
**owner** role; the rest of the console requires **editor**, and project
update/delete requires **manager**. Reads require **viewer**.

## Routes

Console routes (`/api/private/...`) require an account. `dev` and `proj` are
query parameters naming the developer and project.

```text
GET    /api/developers                          developers the account belongs to
GET    /api/developers/{name}                   one developer
GET    /api/developers/{name}/stats             public counters
POST   /api/developers/{name}/enroll            join the program (owner only)
GET    /api/develop/quota                       bot quota for the account

GET    /api/private/projects                    projects of a developer
GET    /api/private/projects/{id}               one project
POST   /api/private/projects                    create
PUT    /api/private/projects/{id}               update
DELETE /api/private/projects/{id}               delete
```

Custom apps. `PATCH` accepts a partial body; `board_widgets` replaces the whole
widget set when present.

```text
GET    /api/private/apps                        apps of a project
GET    /api/private/apps/{appId}                one app
POST   /api/private/apps                        create
PATCH  /api/private/apps/{appId}                update
DELETE /api/private/apps/{appId}                delete (sudo)
GET    /api/private/apps/{appId}/secrets        list secrets
POST   /api/private/apps/{appId}/secrets        create secret (sudo)
GET    /api/private/apps/{appId}/secrets/{id}   one secret
DELETE /api/private/apps/{appId}/secrets/{id}   delete secret (sudo)
POST   /api/private/apps/{appId}/secrets/{id}/rotate  rotate secret (sudo)
POST   /api/private/apps/{appId}/orders         open an ad-hoc wallet order
GET    /api/private/apps/{appId}/board          list widgets
POST   /api/private/apps/{appId}/board          create widget
PUT    /api/private/apps/{appId}/board/{key}    update widget
DELETE /api/private/apps/{appId}/board/{key}    delete widget
GET    /api/private/apps/{appId}/products       list products
POST   /api/private/apps/{appId}/products       create product
PATCH  /api/private/apps/{appId}/products/{id}  update product
DELETE /api/private/apps/{appId}/products/{id}  delete product
```

Bots:

```text
GET    /api/private/bots                        bots of a project
POST   /api/private/bots                        create (sudo)
PATCH  /api/private/bots/{botId}                update
DELETE /api/private/bots/{botId}                delete (sudo)
GET    /api/private/bots/{botId}/keys           list API keys
POST   /api/private/bots/{botId}/keys           create key (sudo)
POST   /api/private/bots/{botId}/keys/{id}/rotate     rotate key (sudo)
DELETE /api/private/bots/{botId}/keys/{id}      revoke key (sudo)
GET    /api/private/bots/{botId}/chat           chat config
PUT    /api/private/bots/{botId}/chat           replace chat config
POST   /api/private/bots/{botId}/chat/manifest  patch chat manifest
```

Mini apps:

```text
GET    /api/private/miniapps                    mini apps of a project
POST   /api/private/miniapps                    create
PATCH  /api/private/miniapps/{id}               update
DELETE /api/private/miniapps/{id}               delete
POST   /api/private/miniapps/{id}/package       upload a ZIP package
```

Public catalogs stay anonymous. Custom apps moved to `/api/custom-apps` so the
marketplace keeps `/api/apps` for products:

```text
GET    /api/custom-apps                         production app catalog
GET    /api/custom-apps/board                   board-capable apps
GET    /api/custom-apps/{slug}                  one app
GET    /api/custom-apps/{slug}/products         its products
GET    /api/custom-apps/{slug}/products/{sku}   one product
GET    /api/miniapps                            published mini apps
GET    /api/miniapps/{slug}                     one published mini app
GET    /api/miniapps/{slug}/package             download its package
GET    /api/bots/{botId}                        bot transparency info
GET    /api/bots/{botId}/developer              its developer
GET    /api/bots/{botId}/chat                   public chat config
GET    /api/bots/{botId}/commands               public command manifest
```

## App-secret routes

A running custom app authenticates with its own API key, not an account:

```text
GET  /api/private/apps/{appId}/board/widgets    the app's own widgets
POST /api/private/apps/{appId}/board/payload    push a board payload
```

The key is read from `Authorization: Bearer <secret>` or `X-App-Secret`. Both
routes require the app to declare the `accounts.profile.board` scope, and the
payload route additionally requires the target account to have authorized the
app for that scope.

## Board payload contract

Every payload field is an envelope:

```json
{ "value": 1200, "label": "Steps today", "format": "integer" }
```

A widget declares `field_types` and `required_fields`; a supplied payload is
validated against both, and the byte size is capped by `max_payload_bytes`.
Field type names are `string`, `number`, `boolean`, `object`, `array`, and
`null`; an unknown type accepts any value.

The canonical widget slug is `{publisher_name}.{app_slug}.{widget_key}`.

## Mini app packages

A package is a ZIP of at most `develop.packages.maxBytes` (5 MiB by default)
containing exactly one `manifest.json`. Entry paths may not be absolute or
contain `..`. The manifest id must be a reverse-domain identifier and is unique
across every mini app.

Archives live in the process S3 bucket under `{keyPrefix}/{miniAppId}/`. When
`develop.packages.publicBaseUrl` is set, responses carry a public
`package_url` and downloads redirect to it; otherwise the portal proxies the
bytes.

## Ad-hoc orders

`POST /api/private/apps/{appId}/orders` opens an order in the wallet for a
purchase that is not a catalogued app product:

```json
{
  "identifier": "sponsor",
  "currency": "CNY",
  "amount": 12.5,
  "expiration_hours": 24,
  "remarks": "thanks",
  "meta": { "tier": "gold" }
}
```

`amount` must be at least `0.001` and `expiration_hours` between 1 and 720
(default 24). The order is attributed to the app's resource identifier
(`developer.app:{appId}`) with `identifier` as the product identifier, and
`meta` is stored as JSON bytes.

## gRPC contract

`DyCustomAppService` is served on the process gRPC port when
`app.solian.develop.apps` is enabled. Passport calls it for board widgets and
Wallet calls it for app products and developer attribution, so those services
keep working against the portal exactly as they did against Develop.

## Configuration

```toml
[develop.files]
target = "drive:9090"          # cloud-file lookups
[develop.accounts]
target = "passport:9090"       # account reads and bot transparency
[develop.profiles]
target = "passport:9090"       # board payload writes
[develop.realms]
target = "passport:9090"       # realm ownership for org publishers
[develop.wallet]
target = "wallet:9090"         # subscription definitions
[develop.actionLog]
target = "stargate:9090"       # audit entries

[develop.packages]
keyPrefix = "plugins"
publicBaseUrl = ""
maxBytes = 5242880
```

`[auth]` and `[sphere]` are shared with the marketplace and already dialed.

## Data migration

The portal uses the process database (`[database] dsn`). To take over an
existing Develop deployment, dump `dyson_develop` and restore its tables into
the DistributionCenter database before enabling the modules: `developers`,
`dev_projects`, `custom_apps`, `custom_app_secrets`, `board_widgets`,
`bot_accounts`, `bot_chat_configs`, `mini_apps`, `app_products`, and
`app_product_states`. `AutoMigrate` then reconciles the schema.
