/** Wire types for the DistributionCenter marketplace API. */

/** A cached cloud-file reference (product art, release attachments). */
export interface CloudFile {
  id: string
  name?: string
  file_meta?: Record<string, unknown>
  user_meta?: Record<string, unknown>
  mime_type?: string
  hash?: string
  size?: number
  url?: string
  width?: number
  height?: number
  blurhash?: string
  usage?: string
  created_at?: string
  updated_at?: string
}

export interface Publisher {
  id: string
  name: string
  nick?: string
  bio?: { value: string } | string
  picture?: CloudFile
  background?: CloudFile
  account_id?: string
  rating?: number
  created_at?: string
  updated_at?: string
}

export type LocalizedText = Record<string, string>

export interface Product {
  id: string
  publisher_id: string
  slug: string
  name: string
  names?: LocalizedText
  description?: string
  descriptions?: LocalizedText
  icon?: CloudFile
  background?: CloudFile
  previews?: CloudFile[]
  created_at: string
  updated_at: string
}

export type ReleaseStatus = 'draft' | 'published' | 'yanked'

export interface Artifact {
  id: string
  platform: string
  architecture: string
  file_name: string
  mime_type: string
  download_url?: string
  download_count: number
  expired?: boolean
}

/** Publisher-authenticated artifact: adds the storage fields management needs. */
export interface ManagedArtifact extends Artifact {
  object_key?: string
  size?: number
  hash?: string
}

export interface Release {
  id: string
  product_id: string
  version: string
  channel?: string
  channels: string[]
  release_notes?: string
  title?: string
  titles?: LocalizedText
  metadata?: Record<string, unknown>
  force_update?: boolean
  descriptions?: LocalizedText
  attachments?: CloudFile[]
  status: ReleaseStatus
  created_at?: string
  published_at?: string | null
  download_count: number
  artifacts: Artifact[]
}

/** Management listing: artifacts carry object key, size, and hash. */
export interface ManagedRelease extends Omit<Release, 'artifacts'> {
  artifacts: ManagedArtifact[]
}

export interface Channel {
  id: string
  name: string
  display_name: string
  display_names?: LocalizedText
  description?: string
  descriptions?: LocalizedText
  artifact_retention?: number
  latest?: Release | null
}

export interface MarketplaceApp {
  product: Product
  publisher: Publisher
  latest: Release | null
}

export interface MarketplacePage {
  data: MarketplaceApp[]
  total: number
  limit: number
  offset: number
}

export interface ProductDetail {
  product: Product
  publisher: Publisher
  latest: Release | null
}

export interface ReleasePage {
  data: ManagedRelease[]
  total: number
  limit: number
  offset: number
}

export interface UploadKey {
  id: string
  product_id: string
  name: string
  key_prefix: string
  created_by?: string
  last_used_at?: string
  revoked_at?: string
  created_at: string
}

export interface CreatedUploadKey extends UploadKey {
  /** Plaintext secret. Returned only by the creation response. */
  key: string
}

export interface UsageMetrics {
  from: string
  to: string
  checks: number
  dau: number
  mau: number
  by_version: Record<string, number>
  by_channel: Record<string, number>
  by_platform: Record<string, number>
  by_architecture: Record<string, number>
  by_os_version: Record<string, number>
  by_client_version: Record<string, number>
  by_locale: Record<string, number>
}

export interface PreparedUpload {
  object_key: string
  upload_url: string
  release_id?: string
  version?: string
}

/** The signed-in account and what it may do, from GET /api/console/session. */
export interface ConsoleSession {
  account: {
    id: string
    name: string
    nick?: string
    is_superuser: boolean
    /** Cached picture URL, not a file reference. */
    picture?: string
  }
  publishers: ConsolePublisher[]
  permissions: string[]
}

export interface ConsolePublisher {
  id: string
  name: string
  nick?: string
  role: string
  /** Cached picture URL, not a file reference. */
  picture?: string
}

/** One resolved module and its capabilities, from GET /api/modules. */
export interface ModuleState {
  id: string
  kind: string
  capabilities?: string[]
}

export interface ModulesResponse {
  modules: ModuleState[]
  capabilities: string[]
}

/** One outbound delivery, from the sync module's job listing. */
export interface SyncJob {
  id: string
  link_id: string
  release_id: string
  version: string
  action: string
  state: string
  attempts: number
  next_attempt_at: string
  external_url?: string
  last_error?: string
  provider_available: boolean
  created_at: string
  updated_at: string
}

export interface SyncJobPage {
  data: SyncJob[]
  total: number
  limit: number
  offset: number
}

export interface SyncConnection {
  id: string
  publisher_id: string
  provider: string
  name: string
  base_url?: string
  has_secret: boolean
  created_by?: string
  created_at: string
  updated_at: string
}

export interface SyncLink {
  id: string
  product_id: string
  connection_id: string
  provider: string
  connection: string
  repository: string
  channels?: { include?: string[]; exclude?: string[] }
  draft: boolean
  prerelease: boolean
  enabled: boolean
  last_synced_at?: string
  created_at: string
  updated_at: string
}

// ── Developer portal ──

/**
 * The portal's scope. Every private portal route is scoped by the developer
 * the account acts for and the project it acts on; the server rejects a
 * request that names neither.
 */
export interface PortalScope {
  /** Publisher name or id, sent as `dev`. */
  dev: string
  /** Project id, sent as `proj`. */
  proj: string
}

export interface PortalPublisher {
  id: string
  name: string
  nick?: string
  bio?: string
  picture?: CloudFile
}

/** One publisher enrolled in the developer program. */
export interface Developer {
  id: string
  publisher_id: string
  publisher?: PortalPublisher
}

export interface DeveloperStats {
  total_custom_apps: number
}

export interface DevProject {
  id: string
  slug: string
  name: string
  description: string
  developer_id: string
  created_at: string
  updated_at: string
  developer?: Developer
}

export interface BotQuotaRecord {
  bot_id: string
  slug: string
  project_id: string
  project_name: string
  developer_id: string
  developer_name: string
}

export interface DeveloperQuota {
  total: number
  used: number
  remaining: number
  level: number
  perk_level: number
  records: BotQuotaRecord[]
}

// ── Custom apps ──

/** App status words. The API sends the enum as a number; see `appStatus`. */
export type AppStatusWord = 'developing' | 'staging' | 'production' | 'suspended'

export interface CustomAppLinks {
  home_page?: string
  privacy_policy?: string
  terms_of_service?: string
}

export interface CustomAppOauthConfig {
  client_uri?: string
  redirect_uris: string[]
  post_logout_redirect_uris?: string[]
  allowed_scopes: string[]
  allowed_grant_types: string[]
  require_pkce: boolean
  allow_offline_access: boolean
  is_public_client: boolean
}

export interface VerificationMark {
  type: string
  title?: string
  description?: string
  verified_by?: string
  created_at?: string
  updated_at?: string
}

export interface CustomApp {
  id: string
  slug: string
  name: string
  description?: string
  /** Numeric enum: 0 developing, 1 staging, 2 production, 3 suspended. */
  status: number
  picture?: CloudFile
  background?: CloudFile
  verification?: VerificationMark
  oauth_config?: CustomAppOauthConfig
  links?: CustomAppLinks
  payment_wallet_id?: string
  project_id: string
  created_at: string
  updated_at: string
  project?: DevProject
}

export interface CustomAppSecret {
  id: string
  /** Plaintext. Returned only by the create and rotate responses. */
  secret?: string
  description?: string
  expired_at?: string
  type: string
  created_at: string
  updated_at: string
}

export interface BoardFieldType {
  name: string
  type: string
  label?: string
  format?: string
  required: boolean
}

export interface BoardWidget {
  key: string
  name: string
  description: string
  is_enabled: boolean
  renderer_type: string
  payload_type: string
  field_types: BoardFieldType[]
  required_fields: string[]
  max_payload_bytes?: number | null
  allow_multiple: boolean
}

export interface BoardApp {
  id: string
  slug: string
  name: string
  description?: string
  publisher_name: string
  board_widgets: BoardWidgetView[]
}

/** A public board widget: the manifest plus its derived slug. */
export interface BoardWidgetView extends BoardWidget {
  slug: string
}

export interface DiscoveryApp {
  id: string
  slug: string
  title: string
  description?: string
  products_count: number
  widgets_count: number
}

// ── App products ──

export interface ProductFulfillment {
  is_address_required: boolean
  required_scopes: string[]
}

export interface AppProductState {
  id: string
  is_enabled: boolean
  /** Numeric enum: 0 unlimited, 1 daily, 2 weekly, 3 monthly, 4 yearly, 5 manual. */
  stock_mode: number
  stock_quantity?: number | null
  last_restocked_at?: string | null
  last_restocked_quantity?: number | null
  product_id: string
  created_at: string
  updated_at: string
}

export interface AppProduct {
  id: string
  identifier: string
  display_name: string
  description?: string
  currency: string
  price: number
  picture?: CloudFile
  background?: CloudFile
  fulfillment?: ProductFulfillment
  app_id: string
  /** Numeric enum: 0 none, 1 weekly, 2 monthly, 3 yearly. */
  recurrence: number
  group_identifier?: string
  created_at: string
  updated_at: string
  state?: AppProductState
}

// ── Bots ──

export interface BotAccount {
  id: string
  slug: string
  is_active: boolean
  project_id: string
  created_at: string
  updated_at: string
  project?: DevProject
}

/** The bot registry row plus the identity account it points at. */
export interface BotView extends BotAccount {
  account?: unknown
}

export interface BotApiKey {
  id: string
  label: string
  account_id: string
  session_id: string
  /** Plaintext. Returned only by the create and rotate responses. */
  key?: { value: string }
  created_at: { seconds: number; nanos?: number }
  updated_at: { seconds: number; nanos?: number }
}

export interface BotCommandParameter {
  name: string
  description?: string
  required: boolean
  type?: string
}

export interface BotCommand {
  name: string
  description: string
  usage?: string
  parameters?: BotCommandParameter[]
}

export interface BotChatConfig {
  id: string
  commands: BotCommand[]
  auto_approve_dm: boolean
  auto_approve_group_chat: boolean
  support_chat: boolean
  subscribed_events: string[]
  created_at: string
  updated_at: string
}

// ── Mini apps ──

export interface MiniAppManifest {
  id: string
  name: string
  version: string
  entry: string
  background: boolean
  author?: string
  description?: string
  permissions?: string[]
  icon?: string
  homepage?: string
  entry_url?: string
}

export interface MiniApp {
  id: string
  slug: string
  plugin_id?: string
  name?: string
  version: string
  author?: string
  description?: string
  entry_url?: string
  icon?: CloudFile
  background?: CloudFile
  homepage?: string
  package_url?: string
  package_sha256?: string
  package_size?: number
  download_count: number
  /** Numeric enum: 0 development, 1 staging, 2 production. */
  stage: number
  manifest?: MiniAppManifest
  project_id: string
  created_at: string
  updated_at: string
  project?: DevProject
}

/** A public mini app: the record plus the publisher that published it. */
export interface MiniAppView extends MiniApp {
  developer_publisher?: PortalPublisher
}

export interface PackageUpload {
  key: string
  url: string
  file_name: string
  content_type: string
  size: number
  sha256: string
}
