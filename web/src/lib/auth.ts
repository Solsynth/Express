import { currentPageUrl } from '@/lib/handoff'

/**
 * The shared sign-in page.
 *
 * Solar Network apps do not ship their own login form: they open the identity
 * console's page on the identity host and hand it a callback. The identity
 * host cannot set a cookie for this domain, so it sends the browser back here
 * with a single-use `code`, which this console exchanges for its own session
 * cookie (`src/lib/handoff.ts`, `POST /api/auth/session`).
 *
 * `VITE_AUTH_URL` overrides the page for a deployment whose identity host is
 * not id.solian.app (and for local development, where it is the Stargate
 * process on :8080).
 */
const DEFAULT_AUTH_PAGE = 'https://id.solian.app/sign-in'

/**
 * The address of the sign-in page, returning to the page the person is on.
 *
 * The return address is the current page on the origin the console is served
 * from, with any hand-off parameters removed: sending back an expired `code`
 * would only fail the exchange again. No token or secret is ever placed here —
 * the issuer validates the host before it issues a code, and the console only
 * ever sees the code.
 */
export function signInUrl(): string {
  const base = import.meta.env.VITE_AUTH_URL || DEFAULT_AUTH_PAGE
  try {
    const url = new URL(base, window.location.origin)
    url.searchParams.set('redirectUri', currentPageUrl())
    return url.toString()
  } catch {
    return base
  }
}
