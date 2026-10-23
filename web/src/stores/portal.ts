import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { developerApi, projectApi } from '@/lib/api/portal'
import type { DeveloperQuota, DevProject, Developer } from '@/lib/api/types'
import { useSessionStore } from '@/stores/session'

/**
 * The developer program's hierarchy: a publisher enrolled as a developer, the
 * projects under it, and the scope every private portal call carries.
 *
 * The marketplace's workspace store cannot hold this. Its shape is
 * publisher → product, and the portal's is publisher → project → app, so the
 * two live apart and the shell asks this store for the portal's tree.
 */
export const usePortalStore = defineStore('portal', () => {
  const session = useSessionStore()

  const developers = ref<Developer[]>([])
  const projects = ref<DevProject[]>([])
  const quota = ref<DeveloperQuota | null>(null)

  const loading = ref(false)
  const loaded = ref(false)
  const error = ref('')

  /** The developer the console is acting for. */
  const developer = ref<Developer | null>(null)
  /** The project the console is acting for. */
  const project = ref<DevProject | null>(null)

  const scoped = computed(() =>
    developer.value && project.value
      ? { dev: developer.value.publisher?.name ?? '', proj: project.value.id }
      : null,
  )

  async function load(force = false) {
    if (!session.signedIn) {
      developers.value = []
      projects.value = []
      loaded.value = false
      return
    }
    if (loaded.value && !force) return
    loading.value = true
    error.value = ''
    try {
      developers.value = await developerApi.list()
      loaded.value = true
    } catch (caught) {
      developers.value = []
      error.value = caught instanceof Error ? caught.message : ''
    } finally {
      loading.value = false
    }
  }

  /**
   * Loads the projects of one developer and selects it.
   *
   * The developer is named by its publisher: the API resolves `dev` through
   * the publisher directory, so the publisher name is the stable handle.
   */
  async function open(name: string) {
    loading.value = true
    error.value = ''
    try {
      const found = developers.value.find(
        (item) => item.publisher?.name === name || item.id === name,
      )
      developer.value =
        found ?? (await developerApi.get(name))
      projects.value = await projectApi.list({ dev: developer.value.publisher?.name ?? name, proj: '' })
      project.value = projects.value[0] ?? null
    } catch (caught) {
      projects.value = []
      project.value = null
      error.value = caught instanceof Error ? caught.message : ''
    } finally {
      loading.value = false
    }
  }

  /** Selects a project without reloading the list. */
  function select(projectId: string) {
    project.value = projects.value.find((item) => item.id === projectId) ?? null
  }

  async function loadQuota() {
    try {
      quota.value = await developerApi.quota()
    } catch {
      quota.value = null
    }
  }

  function clear() {
    developers.value = []
    projects.value = []
    developer.value = null
    project.value = null
    quota.value = null
    loaded.value = false
  }

  return {
    developers,
    projects,
    quota,
    developer,
    project,
    scoped,
    loading,
    loaded,
    error,
    load,
    open,
    select,
    loadQuota,
    clear,
  }
})
