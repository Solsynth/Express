import { translate } from '@/lib/i18n'

/** A typed failure from the DistributionCenter API. */
export class ExpressError extends Error {
  readonly status: number
  readonly detail?: string
  /** Machine code from the API, e.g. `AUTH_SUDO_REQUIRED` on the portal. */
  readonly code?: string
  /** Factor types an account may use to satisfy a step-up requirement. */
  readonly factorTypes?: string[]

  constructor(
    status: number,
    message: string,
    detail?: string,
    code?: string,
    factorTypes?: string[],
  ) {
    super(message)
    this.name = 'ExpressError'
    this.status = status
    this.detail = detail
    this.code = code
    this.factorTypes = factorTypes
  }

  /** True when the session is missing or no longer valid. */
  get isUnauthenticated(): boolean {
    return this.status === 401
  }

  /** True when the account may not perform the action. */
  get isForbidden(): boolean {
    return this.status === 403
  }

  get isNotFound(): boolean {
    return this.status === 404
  }

  /** True when the deployment cannot reach a dependency it needs. */
  get isUnavailable(): boolean {
    return this.status === 503
  }

  /**
   * True when the action needs an elevated session. The developer portal
   * answers this with `AUTH_SUDO_REQUIRED`, which is a step-up prompt rather
   * than a plain refusal.
   */
  get needsElevation(): boolean {
    return this.status === 403 && this.code === 'AUTH_SUDO_REQUIRED'
  }
}

type Method = 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'

export type QueryValue = string | number | boolean | null | undefined

export interface RequestOptions {
  method?: Method
  body?: unknown
  /** A multipart body. It replaces `body` and sets its own content type. */
  form?: FormData
  query?: Record<string, QueryValue>
  signal?: AbortSignal
  /**
   * Retry once through the refresh route after a 401. Off for the session
   * routes themselves: a refused refresh must not start a refresh.
   */
  retryOnUnauthorized?: boolean
}

let unauthorizedHandler: (() => void) | null = null
let refreshHandler: (() => Promise<boolean>) | null = null
let refreshing: Promise<boolean> | null = null

/** Installed by the session store so every 401 lands in one place. */
export function onUnauthorized(handler: () => void) {
  unauthorizedHandler = handler
}

/**
 * Installed by the session store. The access token is short-lived, so a 401
 * on an ordinary request means "renew", not "signed out": this trades the
 * refresh cookie for a fresh pair and reports whether the request is worth
 * repeating.
 */
export function onSessionRefresh(handler: () => Promise<boolean>) {
  refreshHandler = handler
}

/**
 * One refresh shared by every concurrent 401. Without the shared promise a
 * page that loads several panels would spend the refresh token once per panel
 * and rotate it out from under the others.
 */
function refreshSession(): Promise<boolean> {
  if (!refreshing) {
    const attempt = refreshHandler ? refreshHandler() : Promise.resolve(false)
    refreshing = attempt
      .catch(() => false)
      .finally(() => {
        refreshing = null
      })
  }
  return refreshing
}

export function buildUrl(path: string, query?: Record<string, QueryValue>): string {
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(query ?? {})) {
    if (value === undefined || value === null || value === '') continue
    params.set(key, String(value))
  }
  const suffix = params.size ? `?${params.toString()}` : ''
  return `/api${path}${suffix}`
}

/**
 * Reads a JSON body. Delete routes answer 204, and parsing an empty body as
 * JSON would throw and abort the caller.
 */
async function readJson<T>(response: Response): Promise<T> {
  if (response.status === 204) return undefined as T
  const text = await response.text()
  if (!text) return undefined as T
  return JSON.parse(text) as T
}

async function parseError(response: Response): Promise<ExpressError> {
  const fallback = new ExpressError(
    response.status,
    response.statusText || translate('apiError.requestFailed'),
  )
  try {
    const payload = (await response.json()) as {
      error?: string
      message?: string
      code?: string
      factor_types?: string[]
    }
    const message = payload.message || payload.error || fallback.message
    return new ExpressError(
      response.status,
      message,
      payload.error,
      payload.code,
      payload.factor_types,
    )
  } catch {
    return fallback
  }
}

async function send(url: string, init: RequestInit): Promise<Response> {
  let response: Response
  try {
    response = await fetch(url, init)
  } catch (caught) {
    if (caught instanceof DOMException && caught.name === 'AbortError') throw caught
    throw new ExpressError(0, translate('apiError.unreachable'))
  }
  if (response.ok) return response
  throw await parseError(response)
}

/**
 * Sends one request to the DistributionCenter API. The session travels in the
 * HttpOnly AuthToken cookie this console set from the hand-off, so the console
 * never stores tokens.
 *
 * A 401 is not the end of a session: the access token is short-lived, so the
 * request is repeated once behind a single shared refresh. Only a refresh that
 * is itself refused reaches the signed-out handler.
 */
export async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const response = await perform(path, options)
  return readJson<T>(response)
}

/**
 * Sends a request and reads the listing's total count.
 *
 * The developer portal answers a listing with a bare array and the total in
 * the `X-Total` header, so a portal listing cannot use the marketplace's
 * `{ data, total }` envelope. This is the one call that reads a header.
 */
export async function requestWithTotal<T>(
  path: string,
  options: RequestOptions = {},
): Promise<{ data: T; total: number | null }> {
  const response = await perform(path, options)
  const data = await readJson<T>(response)
  const header = response.headers.get('X-Total')
  const total = header === null ? null : Number(header)
  return { data, total: Number.isFinite(total) ? total : null }
}

/**
 * Performs one request, renewing the session once if the token expired.
 *
 * The refresh is shared across concurrent callers, so a page that loads
 * several panels spends the refresh token once rather than once per panel.
 */
async function perform(path: string, options: RequestOptions): Promise<Response> {
  const headers: Record<string, string> = { Accept: 'application/json' }
  if (options.body !== undefined) headers['Content-Type'] = 'application/json'

  const init: RequestInit = {
    method: options.method ?? 'GET',
    headers,
    credentials: 'same-origin',
    body:
      options.form !== undefined
        ? options.form
        : options.body === undefined
          ? undefined
          : JSON.stringify(options.body),
    signal: options.signal,
  }
  const url = buildUrl(path, options.query)

  try {
    return await send(url, init)
  } catch (caught) {
    const unauthenticated = caught instanceof ExpressError && caught.isUnauthenticated
    if (!unauthenticated || options.retryOnUnauthorized === false) {
      if (unauthenticated) unauthorizedHandler?.()
      throw caught
    }
    if (!(await refreshSession())) {
      unauthorizedHandler?.()
      throw caught
    }
  }

  // The retry is the last word: a second 401 means the fresh token was
  // refused too, so the session really is over.
  try {
    return await send(url, init)
  } catch (caught) {
    if (caught instanceof ExpressError && caught.isUnauthenticated) unauthorizedHandler?.()
    throw caught
  }
}
