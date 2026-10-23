import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ExpressError, request, requestWithTotal } from '@/lib/api/client'
import { customAppApi, developerApi, miniAppApi, projectApi } from '@/lib/api/portal'

/**
 * The developer portal's wire contract.
 *
 * These pin the parts of the portal API a caller cannot see from its own code:
 * the scope parameters every private route carries, the enum asymmetry between
 * a read and a write, the `X-Total` listing header, and the multipart upload.
 * A regression here is a request the server silently refuses — a 400 that reads
 * as a console bug, or a list that shows nothing because the scope was dropped.
 */
describe('portal wire contract', () => {
  let calls: { url: string; init: RequestInit }[] = []

  /** Answers every request with a body, recording what was sent. */
  function respond(body: unknown, headers: Record<string, string> = {}, status = 200) {
    return vi.fn(async (url: string, init: RequestInit = {}) => {
      calls.push({ url, init })
      return new Response(body === undefined ? '' : JSON.stringify(body), {
        status,
        headers: { 'Content-Type': 'application/json', ...headers },
      })
    })
  }

  function sentBody(): Record<string, unknown> {
    const last = calls[calls.length - 1]
    return last?.init.body ? (JSON.parse(String(last.init.body)) as Record<string, unknown>) : {}
  }

  function sentUrl(): string {
    return calls[calls.length - 1]?.url ?? ''
  }

  beforeEach(() => {
    calls = []
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  describe('scope', () => {
    it('sends dev and proj on every private call', async () => {
      vi.stubGlobal('fetch', respond([]))

      await projectApi.list({ dev: 'acme', proj: 'p-1' })

      const query = new URLSearchParams(sentUrl().split('?')[1])
      expect(query.get('dev')).toBe('acme')
      expect(query.get('proj')).toBe('p-1')
    })

    it('keeps the scope alongside an extra query parameter', async () => {
      vi.stubGlobal('fetch', respond([]))

      await projectApi.list({ dev: 'acme', proj: 'p-1' })

      // The scope is not dropped when a listing passes paging through.
      expect(sentUrl()).toContain('dev=acme')
      expect(sentUrl()).toContain('proj=p-1')
    })

    it('prefixes every portal path with /api', async () => {
      vi.stubGlobal('fetch', respond({}))

      await developerApi.get('acme')

      expect(sentUrl().startsWith('/api/developers/acme')).toBe(true)
    })

    it('encodes a publisher name that needs it', async () => {
      vi.stubGlobal('fetch', respond({}))

      await developerApi.enroll('solsynth inc')

      expect(sentUrl()).toContain('/developers/solsynth%20inc/enroll')
    })
  })

  describe('enum asymmetry', () => {
    /**
     * A stored record serializes its enum as a number, but the input struct
     * takes a word. Sending the ordinal back is a 400 the server cannot
     * describe usefully, so the write path must always send the word.
     */
    it('sends an app status as a word, not the ordinal it was read as', async () => {
      vi.stubGlobal('fetch', respond({ id: 'a-1', status: 2 }))

      const stored = await customAppApi.get({ dev: 'acme', proj: 'p-1' }, 'a-1')
      expect(stored.status).toBe(2)

      await customAppApi.update({ dev: 'acme', proj: 'p-1' }, 'a-1', { status: 'production' })

      expect(sentBody().status).toBe('production')
      expect(typeof sentBody().status).toBe('string')
    })

    it('sends a mini app stage as a word', async () => {
      vi.stubGlobal('fetch', respond({ id: 'm-1', stage: 1 }))

      await miniAppApi.update({ dev: 'acme', proj: 'p-1' }, 'm-1', { stage: 'production' })

      expect(sentBody().stage).toBe('production')
    })
  })

  describe('listings', () => {
    it('reads the total from the X-Total header', async () => {
      vi.stubGlobal('fetch', respond([], { 'X-Total': '42' }))

      const result = await requestWithTotal<unknown[]>('/developers')

      expect(result.total).toBe(42)
    })

    it('reports a missing total as null rather than a number', async () => {
      vi.stubGlobal('fetch', respond([]))

      const result = await requestWithTotal<unknown[]>('/developers')

      expect(result.total).toBeNull()
    })

    it('keeps a bare array body, since the portal sends no envelope', async () => {
      vi.stubGlobal('fetch', respond([{ id: 'd-1' }]))

      const result = await requestWithTotal<{ id: string }[]>('/developers')

      expect(result.data).toEqual([{ id: 'd-1' }])
    })
  })

  describe('uploads', () => {
    it('sends a package as multipart under the file field', async () => {
      vi.stubGlobal('fetch', respond({ key: 'k', sha256: 'abc', size: 3 }))

      const file = new File(['zip'], 'plugin.zip', { type: 'application/zip' })
      await miniAppApi.uploadPackage({ dev: 'acme', proj: 'p-1' }, 'm-1', file)

      const init = calls[calls.length - 1].init
      expect(init.body).toBeInstanceOf(FormData)
      const form = init.body as FormData
      expect(form.get('file')).toBeInstanceOf(File)
      expect((form.get('file') as File).name).toBe('plugin.zip')
    })
  })

  describe('errors', () => {
    /**
     * The portal answers errors in its own envelope — `{ code, message }` —
     * rather than the marketplace's `{ error }`. A step-up refusal carries the
     * code and the factors an account may use, so the console can prompt
     * instead of printing a refusal.
     */
    it('carries the portal error code and step-up factors', async () => {
      vi.stubGlobal(
        'fetch',
        vi.fn(async () =>
          new Response(
            JSON.stringify({
              code: 'AUTH_SUDO_REQUIRED',
              message: 'step-up authentication is required',
              status: 403,
              factor_types: ['password'],
            }),
            { status: 403, headers: { 'Content-Type': 'application/json' } },
          ),
        ),
      )

      const caught: unknown = await request('/private/apps/a-1', { method: 'DELETE' }).catch(
        (error: unknown) => error,
      )

      expect(caught).toBeInstanceOf(ExpressError)
      const refusal = caught as ExpressError
      expect(refusal.code).toBe('AUTH_SUDO_REQUIRED')
      expect(refusal.needsElevation).toBe(true)
      expect(refusal.factorTypes).toEqual(['password'])
      // A step-up refusal is still a 403, so the ordinary getter agrees.
      expect(refusal.isForbidden).toBe(true)
    })

    it('does not read an ordinary refusal as a step-up prompt', async () => {
      vi.stubGlobal(
        'fetch',
        vi.fn(async () =>
          new Response(JSON.stringify({ code: 'FORBIDDEN', message: 'no', status: 403 }), {
            status: 403,
            headers: { 'Content-Type': 'application/json' },
          }),
        ),
      )

      const caught: unknown = await request('/private/apps', { method: 'POST', body: {} }).catch(
        (error: unknown) => error,
      )

      expect(caught).toBeInstanceOf(ExpressError)
      expect((caught as ExpressError).needsElevation).toBe(false)
    })
  })
})
