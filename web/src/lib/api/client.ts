import { translate } from '@/lib/i18n'

/** A typed failure from the DistributionCenter API. */
export class ExpressError extends Error {
  readonly status: number
  readonly detail?: string

  constructor(status: number, message: string, detail?: string) {
    super(message)
    this.name = 'ExpressError'
    this.status = status
    this.detail = detail
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
}

type Method = 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'

export type QueryValue = string | number | boolean | null | undefined

export interface RequestOptions {
  method?: Method
  body?: unknown
  query?: Record<string, QueryValue>
  signal?: AbortSignal
}

let unauthorizedHandler: (() => void) | null = null

/** Installed by the session store so every 401 lands in one place. */
export function onUnauthorized(handler: () => void) {
  unauthorizedHandler = handler
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
    const payload = (await response.json()) as { error?: string; message?: string }
    const message = payload.message || payload.error || fallback.message
    return new ExpressError(response.status, message, payload.error)
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

  const error = await parseError(response)
  if (error.status === 401) unauthorizedHandler?.()
  throw error
}

/**
 * Sends one request to the DistributionCenter API. The session travels in the
 * HttpOnly AuthToken cookie Stargate sets on the shared cookie domain, so the
 * console never stores tokens.
 */
export async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' }
  if (options.body !== undefined) headers['Content-Type'] = 'application/json'

  const response = await send(buildUrl(path, options.query), {
    method: options.method ?? 'GET',
    headers,
    credentials: 'same-origin',
    body: options.body === undefined ? undefined : JSON.stringify(options.body),
    signal: options.signal,
  })

  return readJson<T>(response)
}
