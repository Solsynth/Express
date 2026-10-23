import { defineStore } from 'pinia'
import { computed, ref } from 'vue'

import { ExpressError, onSessionRefresh, onUnauthorized } from '@/lib/api/client'
import { sessionApi } from '@/lib/api'
import type { ConsoleSession } from '@/lib/api/types'
import { clearHandoffParams, handoffCode } from '@/lib/handoff'
import { translate } from '@/lib/i18n'
import { useNoticeStore } from '@/stores/notice'

type SessionState = 'loading' | 'ready' | 'signed-out' | 'offline'

export const useSessionStore = defineStore('session', () => {
  const state = ref<SessionState>('loading')
  const session = ref<ConsoleSession | null>(null)
  const error = ref('')

  /** The in-flight boot, shared so the shell and router do not both redeem. */
  let booting: Promise<void> | null = null
  let booted = false

  const signedIn = computed(() => state.value === 'ready' && session.value !== null)
  const account = computed(() => session.value?.account ?? null)
  const publishers = computed(() => session.value?.publishers ?? [])
  const permissions = computed(() => new Set(session.value?.permissions ?? []))
  const isSuperuser = computed(() => session.value?.account.is_superuser === true)

  /**
   * A 401 on an ordinary request is handled by the API client: it refreshes
   * once and repeats the request, and only calls here when the refresh itself
   * was refused. The session is then genuinely over, and the shell must say so
   * once rather than let each panel invent its own wording.
   */
  onUnauthorized(() => {
    session.value = null
    state.value = 'signed-out'
    error.value = ''
  })

  /** Trades the refresh cookie for a fresh access token. */
  onSessionRefresh(async () => {
    try {
      await sessionApi.refresh()
      return true
    } catch {
      return false
    }
  })

  /**
   * The console reads the account and its capabilities from one route. An
   * unreachable API is distinct from a missing session: the first is a
   * deployment problem, the second is the normal signed-out state.
   */
  async function load() {
    state.value = 'loading'
    error.value = ''
    try {
      session.value = await sessionApi.me()
      state.value = 'ready'
    } catch (caught) {
      session.value = null
      if (caught instanceof ExpressError && caught.isUnauthenticated) {
        state.value = 'signed-out'
        return
      }
      error.value = caught instanceof Error ? caught.message : ''
      state.value = 'offline'
    }
  }

  /**
   * Exchanges a hand-off code from the address bar for this console's own
   * session cookie, then loads the account. The code is removed before the
   * request is made: it is single-use, so leaving it in the history only
   * invites a replay that fails. A failure is reported, not thrown — the
   * console still has to boot.
   */
  async function completeHandoff(): Promise<boolean> {
    const code = handoffCode(window.location.search)
    if (!code) return false
    clearHandoffParams()
    state.value = 'loading'
    error.value = ''
    try {
      session.value = await sessionApi.redeem(code)
      state.value = 'ready'
      return true
    } catch {
      session.value = null
      state.value = 'signed-out'
      useNoticeStore().fail(translate('app.handoffFailed'))
      return false
    }
  }

  /**
   * Boots the console: exchange a hand-off code when there is one, then load.
   * The router and the shell both ask on start, so the work is shared rather
   * than performed twice — a second redeem would spend a code already used.
   */
  function boot(): Promise<void> {
    if (booted) return Promise.resolve()
    if (!booting) {
      booting = runBoot().finally(() => {
        booting = null
        booted = true
      })
    }
    return booting
  }

  async function runBoot() {
    if (await completeHandoff()) return
    await load()
  }

  async function retry() {
    await load()
  }

  /** Ends this console's session and reports it in the shell. */
  async function signOut() {
    try {
      await sessionApi.logout()
    } catch {
      // The cookies are HttpOnly, so a failed call is the only case where the
      // browser keeps them; the console still shows the signed-out state and
      // the next request decides whether the server agrees.
    }
    clear()
    useNoticeStore().ok(translate('app.signedOutNotice'))
  }

  /** True when the account may perform the named console action. */
  function can(permission: string): boolean {
    if (isSuperuser.value) return true
    return permissions.value.has(permission)
  }

  function clear() {
    session.value = null
    state.value = 'signed-out'
    error.value = ''
  }

  return {
    state,
    session,
    error,
    signedIn,
    account,
    publishers,
    permissions,
    isSuperuser,
    load,
    boot,
    completeHandoff,
    retry,
    signOut,
    can,
    clear,
  }
})
