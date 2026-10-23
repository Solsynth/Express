import { request } from './client'
import type {
  Channel,
  ConsoleSession,
  CreatedUploadKey,
  MarketplacePage,
  ModulesResponse,
  PreparedUpload,
  Product,
  ProductDetail,
  ReleasePage,
  SyncConnection,
  SyncJob,
  SyncJobPage,
  SyncLink,
  UploadKey,
  UsageMetrics,
} from './types'

// ── Session ──

/**
 * The signed-in account, its publisher memberships, and the permission nodes
 * it holds. The console uses the permission list to hide surfaces the account
 * cannot operate, so a control never ends in a 403.
 *
 * `redeem` is the hand-off exchange: it answers with the same body as `me`,
 * so the console loads one shape whether it arrived signed in or just
 * exchanged a code. `refresh` and `logout` opt out of the 401 retry: a
 * refused refresh is the answer, not a reason to refresh again.
 */
export const sessionApi = {
  me: () => request<ConsoleSession>('/console/session'),
  redeem: (code: string) =>
    request<ConsoleSession>('/auth/session', {
      method: 'POST',
      body: { code },
      retryOnUnauthorized: false,
    }),
  refresh: () =>
    request<{ expires_in: number; refresh_expires_in?: number }>('/auth/refresh', {
      method: 'POST',
      retryOnUnauthorized: false,
    }),
  logout: () =>
    request<void>('/auth/logout', { method: 'POST', retryOnUnauthorized: false }),
}

export const modulesApi = {
  state: () => request<ModulesResponse>('/modules'),
}

// ── Public catalog ──

export const catalogApi = {
  apps: (query: { sort?: string; order?: string; limit?: number; offset?: number } = {}) =>
    request<MarketplacePage>('/marketplace/apps', { query }),
  publisherApps: (publisher: string) =>
    request<{ data: Product[] }>(`/publishers/${encodeURIComponent(publisher)}/apps`),
  product: (productId: string) => request<ProductDetail>(`/products/${productId}`),
}

// ── Products ──

export interface ProductWrite {
  slug: string
  name: string
  names?: Record<string, string>
  description?: string
  descriptions?: Record<string, string>
  icon?: unknown
  background?: unknown
  previews?: unknown[]
}

export const productApi = {
  create: (publisher: string, body: ProductWrite) =>
    request<Product>(`/publishers/${encodeURIComponent(publisher)}/products`, {
      method: 'POST',
      body,
    }),
  update: (productId: string, body: ProductWrite) =>
    request<Product>(`/products/${productId}`, { method: 'PUT', body }),
  remove: (productId: string) =>
    request<void>(`/products/${productId}`, { method: 'DELETE' }),
}

// ── Channels ──

export interface ChannelWrite {
  name?: string
  display_name?: string
  display_names?: Record<string, string>
  description?: string
  descriptions?: Record<string, string>
  artifact_retention?: number | null
}

export const channelApi = {
  list: (productId: string) =>
    request<{ data: Channel[] }>(`/products/${productId}/channels`),
  create: (productId: string, body: ChannelWrite) =>
    request<Channel>(`/products/${productId}/channels`, { method: 'POST', body }),
  update: (productId: string, channelId: string, body: ChannelWrite) =>
    request<Channel>(`/products/${productId}/channels/${channelId}`, { method: 'PUT', body }),
  remove: (productId: string, channelId: string) =>
    request<void>(`/products/${productId}/channels/${channelId}`, { method: 'DELETE' }),
}

// ── Releases ──

export interface ReleaseWrite {
  version?: string
  channels?: string[]
  title?: string
  titles?: Record<string, string>
  release_notes?: string
  descriptions?: Record<string, string>
  metadata?: Record<string, unknown>
  force_update?: boolean
  attachments?: unknown[]
}

export const releaseApi = {
  /** The public listing: published releases only. */
  list: (
    productId: string,
    query: { channel?: string; platform?: string; architecture?: string; limit?: number; offset?: number } = {},
  ) => request<ReleasePage>(`/products/${productId}/releases`, { query }),
  /** The management listing: drafts, published, and yanked, with storage fields. */
  manage: (productId: string, query: { channel?: string; limit?: number; offset?: number } = {}) =>
    request<ReleasePage>(`/products/${productId}/releases/manage`, { query }),
  create: (productId: string, body: ReleaseWrite) =>
    request<ReleasePage['data'][number]>(`/products/${productId}/releases`, {
      method: 'POST',
      body,
    }),
  update: (productId: string, releaseId: string, body: ReleaseWrite) =>
    request<ReleasePage['data'][number]>(`/products/${productId}/releases/${releaseId}`, {
      method: 'PUT',
      body,
    }),
  publish: (productId: string, releaseId: string) =>
    request<ReleasePage['data'][number]>(`/products/${productId}/releases/${releaseId}/publish`, {
      method: 'POST',
    }),
  yank: (productId: string, releaseId: string) =>
    request<ReleasePage['data'][number]>(`/products/${productId}/releases/${releaseId}/yank`, {
      method: 'POST',
    }),
  remove: (productId: string, releaseId: string) =>
    request<void>(`/products/${productId}/releases/${releaseId}`, { method: 'DELETE' }),
}

// ── Artifacts ──

export interface UploadPreparation {
  version: string
  channel?: string
  file_name: string
  mime_type: string
  sha256: string
}

export const artifactApi = {
  prepare: (productId: string, body: UploadPreparation) =>
    request<PreparedUpload>(`/products/${productId}/artifacts/upload-url`, {
      method: 'POST',
      body,
    }),
  attach: (
    productId: string,
    releaseRef: string,
    body: {
      object_key?: string
      download_url?: string
      file_name?: string
      mime_type?: string
      size?: number
      hash?: string
      platform: string
      architecture: string
    },
  ) =>
    request<ReleasePage['data'][number]>(
      `/products/${productId}/releases/${encodeURIComponent(releaseRef)}/artifacts`,
      { method: 'POST', body },
    ),
  /**
   * Uploads bytes straight to the presigned S3 URL. This request must not go
   * through the console's API client: it is a third-party origin with its own
   * signature, and the signed headers have to match the preparation request
   * exactly.
   */
  async put(url: string, file: File, mimeType: string, sha256: string): Promise<void> {
    const response = await fetch(url, {
      method: 'PUT',
      headers: { 'Content-Type': mimeType, 'x-amz-meta-sha256': sha256 },
      body: file,
    })
    if (!response.ok) {
      throw new Error(`Storage rejected the upload (${response.status})`)
    }
  },
}

// ── Upload keys ──

export const uploadKeyApi = {
  list: (productId: string) =>
    request<{ data: UploadKey[] }>(`/products/${productId}/upload-api-keys`),
  create: (productId: string, name: string) =>
    request<CreatedUploadKey>(`/products/${productId}/upload-api-keys`, {
      method: 'POST',
      body: { name },
    }),
  revoke: (productId: string, keyId: string) =>
    request<void>(`/products/${productId}/upload-api-keys/${keyId}`, { method: 'DELETE' }),
}

// ── Metrics ──

export const metricsApi = {
  usage: (productId: string, query: { from?: string; to?: string } = {}) =>
    request<UsageMetrics>(`/products/${productId}/metrics`, { query }),
}

// ── Sync ──

export interface SyncConnectionWrite {
  provider: string
  name: string
  base_url?: string
  secret?: Record<string, string>
}

export interface SyncLinkWrite {
  connection_id: string
  repository: string
  channels?: { include?: string[]; exclude?: string[] }
  draft?: boolean
  prerelease?: boolean
  enabled?: boolean
}

export const syncApi = {
  connections: (publisher: string) =>
    request<{ data: SyncConnection[]; providers: string[] }>(
      `/publishers/${encodeURIComponent(publisher)}/sync/connections`,
    ),
  createConnection: (publisher: string, body: SyncConnectionWrite) =>
    request<SyncConnection>(`/publishers/${encodeURIComponent(publisher)}/sync/connections`, {
      method: 'POST',
      body,
    }),
  updateConnection: (publisher: string, id: string, body: SyncConnectionWrite) =>
    request<SyncConnection>(
      `/publishers/${encodeURIComponent(publisher)}/sync/connections/${id}`,
      { method: 'PUT', body },
    ),
  removeConnection: (publisher: string, id: string) =>
    request<void>(`/publishers/${encodeURIComponent(publisher)}/sync/connections/${id}`, {
      method: 'DELETE',
    }),
  links: (productId: string) =>
    request<{ data: SyncLink[] }>(`/products/${productId}/sync/links`),
  createLink: (productId: string, body: SyncLinkWrite) =>
    request<SyncLink>(`/products/${productId}/sync/links`, { method: 'POST', body }),
  updateLink: (productId: string, id: string, body: SyncLinkWrite) =>
    request<SyncLink>(`/products/${productId}/sync/links/${id}`, { method: 'PUT', body }),
  removeLink: (productId: string, id: string) =>
    request<void>(`/products/${productId}/sync/links/${id}`, { method: 'DELETE' }),
  jobs: (productId: string, query: { state?: string; limit?: number; offset?: number } = {}) =>
    request<SyncJobPage>(`/products/${productId}/sync/jobs`, { query }),
  retryJob: (productId: string, id: string) =>
    request<SyncJob>(`/products/${productId}/sync/jobs/${id}/retry`, { method: 'POST' }),
}
