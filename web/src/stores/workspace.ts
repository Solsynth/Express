import { defineStore } from 'pinia'
import { computed, ref } from 'vue'

import { catalogApi } from '@/lib/api'
import type { ConsolePublisher, Product } from '@/lib/api/types'
import { localized } from '@/lib/format'
import { useSessionStore } from '@/stores/session'

/** A product plus the publisher it belongs to, for the sidebar switcher. */
export interface WorkspaceProduct {
  product: Product
  publisher: ConsolePublisher
}

/**
 * The account's products, loaded once and shared by the sidebar switcher and
 * the workspace page: both answer "what can I open", and asking twice would
 * show a stale list in one of them.
 */
export const useWorkspaceStore = defineStore('workspace', () => {
  const session = useSessionStore()

  const products = ref<WorkspaceProduct[]>([])
  const loading = ref(false)
  const loaded = ref(false)
  const error = ref('')

  const grouped = computed(() => {
    const groups = new Map<string, { publisher: ConsolePublisher; products: Product[] }>()
    for (const entry of products.value) {
      const group = groups.get(entry.publisher.id)
      if (group) group.products.push(entry.product)
      else groups.set(entry.publisher.id, { publisher: entry.publisher, products: [entry.product] })
    }
    return [...groups.values()].map((group) => ({
      ...group,
      products: group.products.sort((left, right) =>
        localized(left.names, left.name).localeCompare(localized(right.names, right.name)),
      ),
    }))
  })

  async function load(force = false) {
    if (!session.signedIn) {
      products.value = []
      loaded.value = false
      return
    }
    if (loaded.value && !force) return
    loading.value = true
    error.value = ''
    try {
      const lists = await Promise.all(
        session.publishers.map(async (publisher) => {
          const result = await catalogApi.publisherApps(publisher.name)
          return (result.data ?? []).map((product) => ({ product, publisher }))
        }),
      )
      products.value = lists.flat()
      loaded.value = true
    } catch (caught) {
      // A sidebar that cannot list products is not a reason to blank the page;
      // the workspace view reports the failure where the user asked for it.
      products.value = []
      error.value = caught instanceof Error ? caught.message : ''
    } finally {
      loading.value = false
    }
  }

  return { products, grouped, loading, loaded, error, load }
})
