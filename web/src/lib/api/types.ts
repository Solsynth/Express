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
