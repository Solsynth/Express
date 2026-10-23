import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { modulesApi } from '@/lib/api'
import type { ModuleState } from '@/lib/api/types'

/** Module IDs the console gates surfaces on. */
export const MODULES = {
  sync: 'app.solian.distribution.sync',
} as const

/**
 * The resolved module set. The console uses it to hide the surfaces a
 * deployment does not serve, so a disabled module never renders a nav entry or
 * a route that would 404. A deployment whose module lookup fails keeps every
 * surface: the routes themselves still refuse what they cannot serve.
 */
export const useModulesStore = defineStore('modules', () => {
  const modules = ref<ModuleState[]>([])
  const loaded = ref(false)

  const ids = computed(() => new Set(modules.value.map((module) => module.id)))

  function has(id: string): boolean {
    if (!loaded.value) return true
    return ids.value.has(id)
  }

  function hasAll(list: string[]): boolean {
    return list.every(has)
  }

  async function load() {
    try {
      const state = await modulesApi.state()
      modules.value = state.modules ?? []
      loaded.value = true
    } catch {
      modules.value = []
      loaded.value = false
    }
  }

  return { modules, loaded, ids, has, hasAll, load }
})
