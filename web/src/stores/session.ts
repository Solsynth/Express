import { defineStore } from 'pinia'
import { computed, ref } from 'vue'

import { ExpressError, onUnauthorized } from '@/lib/api/client'
import { sessionApi } from '@/lib/api'
import type { ConsoleSession } from '@/lib/api/types'

type SessionState = 'loading' | 'ready' | 'signed-out' | 'offline'

export const useSessionStore = defineStore('session', () => {
  const state = ref<SessionState>('loading')
  const session = ref<ConsoleSession | null>(null)
  const error = ref('')

  const signedIn = computed(() => state.value === 'ready' && session.value !== null)
  const account = computed(() => session.value?.account ?? null)
  const publishers = computed(() => session.value?.publishers ?? [])
  const permissions = computed(() => new Set(session.value?.permissions ?? []))
  const isSuperuser = computed(() => session.value?.account.is_superuser === true)

  // Every 401 lands here: the session is gone and the shell must say so once
  // rather than let each panel invent its own wording.
  onUnauthorized(() => {
    session.value = null
    state.value = 'signed-out'
    error.value = ''
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

  async function retry() {
    await load()
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
    retry,
    can,
    clear,
  }
})
