import { request, requestWithTotal } from './client'
import type {
  AppProduct,
  AppStatusWord,
  BoardApp,
  BoardWidget,
  BotApiKey,
  BotChatConfig,
  BotView,
  CustomApp,
  CustomAppLinks,
  CustomAppOauthConfig,
  CustomAppSecret,
  DevProject,
  Developer,
  DeveloperQuota,
  DeveloperStats,
  DiscoveryApp,
  MiniApp,
  PackageUpload,
  PortalScope,
} from './types'

/**
 * The developer portal's HTTP surface.
 *
 * Two things separate it from the marketplace API:
 *
 * - Every private route is scoped by the developer the account acts for and the
 *   project it acts on. The server reads them from the `dev` and `proj` query
 *   parameters, and a request that names no project is rejected, so the scope
 *   is not optional plumbing: it is part of every call.
 * - The permission nodes differ too. A portal control is gated on the node the
 *   server checks (`dev.projects.create`, `custom.apps.secrets.manage`, …),
 *   so a control is hidden rather than failing with a 403.
 */
/**
 * The scope every private portal call carries: the developer the account acts
 * for and the project it acts on. The server reads both from the query
 * string, so they travel with every request.
 */
function scoped(
  path: string,
  scope: PortalScope,
  query: Record<string, string | number | boolean | null | undefined> = {},
) {
  return { path, query: { dev: scope.dev, proj: scope.proj, ...query } }
}

// ── Developers and projects ──

export interface ProjectWrite {
  slug?: string
  name?: string
  description?: string
}

export const developerApi = {
  /** The developers the account belongs to. */
  list: () => request<Developer[]>('/developers'),
  /** One developer by publisher name or id. */
  get: (name: string) => request<Developer>(`/developers/${encodeURIComponent(name)}`),
  /** The public counters of one developer. */
  stats: (name: string) =>
    request<DeveloperStats>(`/developers/${encodeURIComponent(name)}/stats`),
  /** Joins a publisher to the developer program. Owner only. */
  enroll: (name: string) =>
    request<Developer>(`/developers/${encodeURIComponent(name)}/enroll`, { method: 'POST' }),
  /** The account's bot quota. */
  quota: () => request<DeveloperQuota>('/develop/quota'),
}

export const projectApi = {
  list: (scope: PortalScope) => {
    const target = scoped('/private/projects', scope)
    return request<DevProject[]>(target.path, { query: target.query })
  },
  get: (scope: PortalScope, projectId: string) => {
    const target = scoped(`/private/projects/${projectId}`, scope)
    return request<DevProject>(target.path, { query: target.query })
  },
  create: (scope: PortalScope, body: ProjectWrite) => {
    const target = scoped('/private/projects', scope)
    return request<DevProject>(target.path, { method: 'POST', body, query: target.query })
  },
  update: (scope: PortalScope, projectId: string, body: ProjectWrite) => {
    const target = scoped(`/private/projects/${projectId}`, scope)
    return request<DevProject>(target.path, { method: 'PUT', body, query: target.query })
  },
  remove: (scope: PortalScope, projectId: string) => {
    const target = scoped(`/private/projects/${projectId}`, scope)
    return request<void>(target.path, { method: 'DELETE', query: target.query })
  },
}

// ── Custom apps ──

export interface CustomAppWrite {
  slug?: string
  name?: string
  description?: string
  picture_id?: string
  background_id?: string
  status?: AppStatusWord
  links?: CustomAppLinks
  oauth_config?: CustomAppOauthConfig
  board_widgets?: BoardWidget[]
}

export interface SecretWrite {
  description?: string
  expires_in?: string
  type?: string
}

export interface BoardWidgetWrite {
  key: string
  name: string
  description?: string
  is_enabled?: boolean
  renderer_type?: string
  field_types?: BoardWidget['field_types']
  required_fields?: string[]
  max_payload_bytes?: number | null
  allow_multiple?: boolean
}

export interface OrderWrite {
  identifier: string
  currency: string
  amount: number
  expiration_hours?: number
  remarks?: string
  meta?: Record<string, unknown>
}

export const customAppApi = {
  list: (scope: PortalScope) => {
    const target = scoped('/private/apps', scope)
    return request<CustomApp[]>(target.path, { query: target.query })
  },
  get: (scope: PortalScope, appId: string) => {
    const target = scoped(`/private/apps/${appId}`, scope)
    return request<CustomApp>(target.path, { query: target.query })
  },
  create: (scope: PortalScope, body: CustomAppWrite) => {
    const target = scoped('/private/apps', scope)
    return request<CustomApp>(target.path, { method: 'POST', body, query: target.query })
  },
  update: (scope: PortalScope, appId: string, body: CustomAppWrite) => {
    const target = scoped(`/private/apps/${appId}`, scope)
    return request<CustomApp>(target.path, { method: 'PATCH', body, query: target.query })
  },
  remove: (scope: PortalScope, appId: string) => {
    const target = scoped(`/private/apps/${appId}`, scope)
    return request<void>(target.path, { method: 'DELETE', query: target.query })
  },
  /** Opens a wallet order for a purchase that is not a catalogued product. */
  createOrder: (scope: PortalScope, appId: string, body: OrderWrite) => {
    const target = scoped(`/private/apps/${appId}/orders`, scope)
    return request<unknown>(target.path, { method: 'POST', body, query: target.query })
  },
}

export const secretApi = {
  list: (scope: PortalScope, appId: string) => {
    const target = scoped(`/private/apps/${appId}/secrets`, scope)
    return request<CustomAppSecret[]>(target.path, { query: target.query })
  },
  get: (scope: PortalScope, appId: string, secretId: string) => {
    const target = scoped(`/private/apps/${appId}/secrets/${secretId}`, scope)
    return request<CustomAppSecret>(target.path, { query: target.query })
  },
  create: (scope: PortalScope, appId: string, body: SecretWrite) => {
    const target = scoped(`/private/apps/${appId}/secrets`, scope)
    return request<CustomAppSecret>(target.path, { method: 'POST', body, query: target.query })
  },
  rotate: (scope: PortalScope, appId: string, secretId: string, body: SecretWrite = {}) => {
    const target = scoped(`/private/apps/${appId}/secrets/${secretId}/rotate`, scope)
    return request<CustomAppSecret>(target.path, { method: 'POST', body, query: target.query })
  },
  remove: (scope: PortalScope, appId: string, secretId: string) => {
    const target = scoped(`/private/apps/${appId}/secrets/${secretId}`, scope)
    return request<void>(target.path, { method: 'DELETE', query: target.query })
  },
}

export const boardApi = {
  list: (scope: PortalScope, appId: string) => {
    const target = scoped(`/private/apps/${appId}/board`, scope)
    return request<BoardWidget[]>(target.path, { query: target.query })
  },
  create: (scope: PortalScope, appId: string, body: BoardWidgetWrite) => {
    const target = scoped(`/private/apps/${appId}/board`, scope)
    return request<BoardWidget>(target.path, { method: 'POST', body, query: target.query })
  },
  update: (scope: PortalScope, appId: string, key: string, body: BoardWidgetWrite) => {
    const target = scoped(`/private/apps/${appId}/board/${encodeURIComponent(key)}`, scope)
    return request<BoardWidget>(target.path, { method: 'PUT', body, query: target.query })
  },
  remove: (scope: PortalScope, appId: string, key: string) => {
    const target = scoped(`/private/apps/${appId}/board/${encodeURIComponent(key)}`, scope)
    return request<void>(target.path, { method: 'DELETE', query: target.query })
  },
}

// ── App products ──

export interface AppProductWrite {
  identifier?: string
  display_name?: string
  description?: string
  currency?: string
  price?: number
  picture_id?: string
  background_id?: string
  recurrence?: string
  group_identifier?: string
  fulfillment?: { is_address_required?: boolean; required_scopes?: string[] }
  state?: { is_enabled?: boolean; stock_mode?: string; stock_quantity?: number }
}

export const appProductApi = {
  list: (scope: PortalScope, appId: string) => {
    const target = scoped(`/private/apps/${appId}/products`, scope)
    return request<AppProduct[]>(target.path, { query: target.query })
  },
  get: (scope: PortalScope, appId: string, productId: string) => {
    const target = scoped(`/private/apps/${appId}/products/${productId}`, scope)
    return request<AppProduct>(target.path, { query: target.query })
  },
  create: (scope: PortalScope, appId: string, body: AppProductWrite) => {
    const target = scoped(`/private/apps/${appId}/products`, scope)
    return request<AppProduct>(target.path, { method: 'POST', body, query: target.query })
  },
  update: (scope: PortalScope, appId: string, productId: string, body: AppProductWrite) => {
    const target = scoped(`/private/apps/${appId}/products/${productId}`, scope)
    return request<AppProduct>(target.path, { method: 'PATCH', body, query: target.query })
  },
  remove: (scope: PortalScope, appId: string, productId: string) => {
    const target = scoped(`/private/apps/${appId}/products/${productId}`, scope)
    return request<void>(target.path, { method: 'DELETE', query: target.query })
  },
}

// ── Bots ──

export interface BotWrite {
  name?: string
  nick?: string
  slug?: string
  language?: string
  first_name?: string
  middle_name?: string
  last_name?: string
  gender?: string
  pronouns?: string
  time_zone?: string
  location?: string
  bio?: string
  birthday?: number
  picture_id?: string
  background_id?: string
  is_active?: boolean
}

export interface BotChatConfigWrite {
  commands?: BotChatConfig['commands']
  auto_approve_dm?: boolean
  auto_approve_group_chat?: boolean
  support_chat?: boolean
  subscribed_events?: string[]
}

export const botApi = {
  list: (scope: PortalScope) => {
    const target = scoped('/private/bots', scope)
    return request<BotView[]>(target.path, { query: target.query })
  },
  get: (scope: PortalScope, botId: string) => {
    const target = scoped(`/private/bots/${botId}`, scope)
    return request<BotView>(target.path, { query: target.query })
  },
  create: (scope: PortalScope, body: BotWrite) => {
    const target = scoped('/private/bots', scope)
    return request<BotView>(target.path, { method: 'POST', body, query: target.query })
  },
  update: (scope: PortalScope, botId: string, body: BotWrite) => {
    const target = scoped(`/private/bots/${botId}`, scope)
    return request<BotView>(target.path, { method: 'PATCH', body, query: target.query })
  },
  remove: (scope: PortalScope, botId: string) => {
    const target = scoped(`/private/bots/${botId}`, scope)
    return request<void>(target.path, { method: 'DELETE', query: target.query })
  },
  keys: (scope: PortalScope, botId: string) => {
    const target = scoped(`/private/bots/${botId}/keys`, scope)
    return request<BotApiKey[]>(target.path, { query: target.query })
  },
  key: (scope: PortalScope, botId: string, keyId: string) => {
    const target = scoped(`/private/bots/${botId}/keys/${keyId}`, scope)
    return request<BotApiKey>(target.path, { query: target.query })
  },
  createKey: (scope: PortalScope, botId: string, label: string) => {
    const target = scoped(`/private/bots/${botId}/keys`, scope)
    return request<BotApiKey>(target.path, { method: 'POST', body: { label }, query: target.query })
  },
  rotateKey: (scope: PortalScope, botId: string, keyId: string) => {
    const target = scoped(`/private/bots/${botId}/keys/${keyId}/rotate`, scope)
    return request<BotApiKey>(target.path, { method: 'POST', query: target.query })
  },
  removeKey: (scope: PortalScope, botId: string, keyId: string) => {
    const target = scoped(`/private/bots/${botId}/keys/${keyId}`, scope)
    return request<void>(target.path, { method: 'DELETE', query: target.query })
  },
  chat: (scope: PortalScope, botId: string) => {
    const target = scoped(`/private/bots/${botId}/chat`, scope)
    return request<BotChatConfig>(target.path, { query: target.query })
  },
  saveChat: (scope: PortalScope, botId: string, body: BotChatConfigWrite) => {
    const target = scoped(`/private/bots/${botId}/chat`, scope)
    return request<BotChatConfig>(target.path, { method: 'PUT', body, query: target.query })
  },
  patchChatManifest: (scope: PortalScope, botId: string, body: BotChatConfigWrite) => {
    const target = scoped(`/private/bots/${botId}/chat/manifest`, scope)
    return request<BotChatConfig>(target.path, { method: 'POST', body, query: target.query })
  },
}

// ── Mini apps ──

export interface MiniAppWrite {
  slug?: string
  stage?: string
  manifest?: MiniApp['manifest']
  icon_id?: string
  background_id?: string
}

export const miniAppApi = {
  list: (scope: PortalScope) => {
    const target = scoped('/private/miniapps', scope)
    return request<MiniApp[]>(target.path, { query: target.query })
  },
  get: (scope: PortalScope, id: string) => {
    const target = scoped(`/private/miniapps/${id}`, scope)
    return request<MiniApp>(target.path, { query: target.query })
  },
  create: (scope: PortalScope, body: MiniAppWrite) => {
    const target = scoped('/private/miniapps', scope)
    return request<MiniApp>(target.path, { method: 'POST', body, query: target.query })
  },
  update: (scope: PortalScope, id: string, body: MiniAppWrite) => {
    const target = scoped(`/private/miniapps/${id}`, scope)
    return request<MiniApp>(target.path, { method: 'PATCH', body, query: target.query })
  },
  remove: (scope: PortalScope, id: string) => {
    const target = scoped(`/private/miniapps/${id}`, scope)
    return request<void>(target.path, { method: 'DELETE', query: target.query })
  },
  /** Uploads the package archive. The server reads the file from the `file` field. */
  uploadPackage: (scope: PortalScope, id: string, file: File) => {
    const target = scoped(`/private/miniapps/${id}/package`, scope)
    const form = new FormData()
    form.append('file', file, file.name)
    return request<PackageUpload>(target.path, { method: 'POST', form, query: target.query })
  },
}

// ── Public catalogs ──

export const portalCatalogApi = {
  customApps: (query: { take?: number; offset?: number; search?: string } = {}) =>
    requestWithTotal<DiscoveryApp[]>('/custom-apps', { query }),
  boardApps: (query: { take?: number; offset?: number; slug?: string } = {}) =>
    request<BoardApp[]>('/custom-apps/board', { query }),
  miniApps: (query: { take?: number; offset?: number; search?: string } = {}) =>
    requestWithTotal<MiniApp[]>('/miniapps', { query }),
}
