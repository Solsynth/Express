import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { signInUrl } from '@/lib/auth'
import {
  clearHandoffParams,
  currentPageUrl,
  handoffCode,
  stripHandoffParams,
} from '@/lib/handoff'

/**
 * The hand-off code is a credential with a short life, so the two things that
 * must never go wrong are: a page without a code must not try to redeem one,
 * and a code that has been read must leave the address bar. Both are decided
 * here, before any request is made.
 */
describe('hand-off URL handling', () => {
  beforeEach(() => {
    window.history.replaceState({}, '', '/workspace/product-1?tab=releases')
  })

  afterEach(() => {
    window.history.replaceState({}, '', '/')
  })

  it('reads the code the issuer appended', () => {
    expect(handoffCode('?code=abc123')).toBe('abc123')
    expect(handoffCode('?redirectUri=x&code=abc123')).toBe('abc123')
  })

  it('reports no code for an empty or unrelated query', () => {
    expect(handoffCode('')).toBeNull()
    expect(handoffCode('?tab=releases')).toBeNull()
    // A blank code is a broken redirect, not a credential to send.
    expect(handoffCode('?code=')).toBeNull()
    expect(handoffCode('?code=%20')).toBeNull()
  })

  it('strips every hand-off parameter and keeps the rest of the address', () => {
    const stripped = stripHandoffParams(
      'https://dx.solsynth.dev/workspace/p1?tab=releases&code=abc&state=s1&error=access_denied',
    )
    const url = new URL(stripped)
    expect(url.origin).toBe('https://dx.solsynth.dev')
    expect(url.pathname).toBe('/workspace/p1')
    expect(url.searchParams.get('tab')).toBe('releases')
    expect(url.searchParams.has('code')).toBe(false)
    expect(url.searchParams.has('state')).toBe(false)
    expect(url.searchParams.has('error')).toBe(false)
  })

  it('removes the code from the address bar without navigating', () => {
    window.history.replaceState({}, '', '/marketplace?code=abc123&tab=apps')
    expect(handoffCode(window.location.search)).toBe('abc123')

    clearHandoffParams()

    expect(window.location.search).toBe('?tab=apps')
    expect(handoffCode(window.location.search)).toBeNull()
    // The exchange is single-use, so a reload must not find a code to replay.
    expect(handoffCode(window.location.search)).toBeNull()
  })

  it('leaves an address without a code untouched', () => {
    const before = window.location.href
    clearHandoffParams()
    expect(window.location.href).toBe(before)
  })

  it('returns the current page without a stale code', () => {
    window.history.replaceState({}, '', '/workspace/p1?code=old')
    expect(currentPageUrl()).toBe(`${window.location.origin}/workspace/p1`)
  })
})

describe('signInUrl', () => {
  beforeEach(() => {
    window.history.replaceState({}, '', '/workspace/p1?tab=releases')
  })

  afterEach(() => {
    window.history.replaceState({}, '', '/')
  })

  it('sends the current page as the return address on the console origin', () => {
    const url = new URL(signInUrl())
    expect(url.origin).toBe('https://id.solian.app')
    expect(url.pathname).toBe('/sign-in')
    expect(url.searchParams.get('redirectUri')).toBe(
      `${window.location.origin}/workspace/p1?tab=releases`,
    )
  })

  it('never puts a token or secret in the sign-in address', () => {
    const url = new URL(signInUrl())
    for (const name of ['token', 'access_token', 'refresh_token', 'client_secret']) {
      expect(url.searchParams.has(name)).toBe(false)
    }
    expect([...url.searchParams.keys()]).toEqual(['redirectUri'])
  })

  it('drops a hand-off code from the return address', () => {
    window.history.replaceState({}, '', '/marketplace?code=spent')
    const url = new URL(signInUrl())
    expect(url.searchParams.get('redirectUri')).toBe(`${window.location.origin}/marketplace`)
  })
})
