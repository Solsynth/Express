/**
 * The receiving half of the identity hand-off.
 *
 * The identity console cannot set a cookie on this domain, so it sends the
 * browser back here with a single-use `code`. This module trades that code for
 * the console's own session cookie and takes the code out of the address bar
 * as soon as it has been used: a code left in the history is a credential left
 * on screen.
 *
 * Everything here is pure URL work, so the exchange order — read the code,
 * strip it, then post it — can be tested without a browser. The exchange
 * itself lives in the session store.
 */

/** Parameters the issuer may append that must not survive the exchange. */
const HANDOFF_PARAMS = ['code', 'state', 'error', 'error_description'] as const

/** The hand-off code in a query string, or null when there is none. */
export function handoffCode(search: string): string | null {
  const code = new URLSearchParams(search).get('code')
  return code && code.trim() ? code : null
}

/** The same URL with every hand-off parameter removed. */
export function stripHandoffParams(href: string): string {
  const url = new URL(href)
  for (const name of HANDOFF_PARAMS) url.searchParams.delete(name)
  return url.toString()
}

/**
 * The current page without a hand-off code, used as the sign-in return
 * address: sending the issuer a redirect that still carries the old code
 * would put an expired credential back in the bar on return.
 */
export function currentPageUrl(): string {
  return stripHandoffParams(window.location.href)
}

/**
 * Removes the hand-off parameters from the address bar without a navigation,
 * so the code cannot be replayed from the history and a reload does not try
 * to redeem it twice.
 */
export function clearHandoffParams(): void {
  const stripped = stripHandoffParams(window.location.href)
  if (stripped === window.location.href) return
  window.history.replaceState(window.history.state, '', stripped)
}
