/**
 * The shared sign-in page.
 *
 * Solar Network apps do not ship their own login form: they open the identity
 * console's page on the identity host and hand it a callback. The session then
 * lives in an HttpOnly cookie scoped to `[auth] cookieDomain` on Stargate, so
 * it reaches this console as soon as the identity host sets it — Express only
 * has to send the browser there and receive it back.
 *
 * `VITE_AUTH_URL` overrides the page for a deployment whose identity host is
 * not id.solian.app (and for local development, where it is the Stargate
 * process on :8080).
 */
const DEFAULT_AUTH_PAGE = 'https://id.solian.app/sign-in'

/** The address of the sign-in page, returning to the page the person is on. */
export function signInUrl(): string {
  const base = import.meta.env.VITE_AUTH_URL || DEFAULT_AUTH_PAGE
  try {
    const url = new URL(base, window.location.origin)
    url.searchParams.set('redirectUri', window.location.href)
    return url.toString()
  } catch {
    return base
  }
}
