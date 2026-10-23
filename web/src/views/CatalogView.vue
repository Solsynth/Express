<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronLeft16, ChevronRight16, Renew16 } from '@carbon/icons-vue'
import { RouterLink } from 'vue-router'

import { catalogApi } from '@/lib/api'
import { ExpressError } from '@/lib/api/client'
import type { MarketplaceApp } from '@/lib/api/types'
import { localized, relative } from '@/lib/format'
import PageHeader from '@/components/PageHeader.vue'
import ConsolePanel from '@/components/ConsolePanel.vue'
import LoadingSkeleton from '@/components/LoadingSkeleton.vue'
import IconButton from '@/components/IconButton.vue'
import AppMark from '@/components/AppMark.vue'

const { t, locale } = useI18n()

const apps = ref<MarketplaceApp[]>([])
const total = ref(0)
const loading = ref(true)
const error = ref('')
const page = ref(0)
const pageSize = 20

// The catalog is public, so its sort is a URL-free local choice: a reader who
// reloads gets the default order rather than a shared link that disagrees with
// the one they copied.
const sort = ref<'updated_at' | 'name' | 'created_at'>('updated_at')
const order = ref<'desc' | 'asc'>('desc')

const offset = computed(() => page.value * pageSize)
const lastPage = computed(() => Math.max(0, Math.ceil(total.value / pageSize) - 1))

async function load() {
  loading.value = true
  error.value = ''
  try {
    const result = await catalogApi.apps({
      sort: sort.value,
      order: order.value,
      limit: pageSize,
      offset: offset.value,
    })
    apps.value = result.data ?? []
    total.value = result.total ?? 0
  } catch (caught) {
    error.value = caught instanceof ExpressError ? caught.message : t('catalog.error')
  } finally {
    loading.value = false
  }
}

function go(next: number) {
  page.value = Math.min(Math.max(0, next), lastPage.value)
  void load()
}

// Changing the ordering asks a different question, so it starts at the first
// page instead of keeping a deep offset into a reordered list.
watch([sort, order], () => {
  page.value = 0
  void load()
})

function productName(app: MarketplaceApp): string {
  return localized(app.product.names, app.product.name, locale.value)
}

function publisherName(app: MarketplaceApp): string {
  return app.publisher?.nick || app.publisher?.name || '—'
}

onMounted(load)
</script>

<template>
  <div>
    <PageHeader :title="t('catalog.title')" :description="t('catalog.description')">
      <template #actions>
        <label class="flex items-center gap-2 text-2xs text-faint">
          {{ t('catalog.sort') }}
          <select v-model="sort" class="select select-xs w-auto">
            <option value="updated_at">{{ t('catalog.sortUpdated') }}</option>
            <option value="name">{{ t('catalog.sortName') }}</option>
            <option value="created_at">{{ t('catalog.sortCreated') }}</option>
          </select>
        </label>
        <label class="flex items-center gap-2 text-2xs text-faint">
          {{ t('catalog.order') }}
          <select v-model="order" class="select select-xs w-auto">
            <option value="desc">{{ t('catalog.descending') }}</option>
            <option value="asc">{{ t('catalog.ascending') }}</option>
          </select>
        </label>
        <IconButton :label="t('common.refresh')" :disabled="loading" @click="load">
          <Renew16 aria-hidden="true" />
        </IconButton>
      </template>
    </PageHeader>

    <p v-if="error" class="mb-4 border-l border-error pl-3 text-xs text-error">{{ error }}</p>

    <ConsolePanel :title="t('catalog.latest')" :note="loading ? undefined : t('catalog.total', { n: total })">
      <LoadingSkeleton v-if="loading" :rows="6" variant="rows" />

      <div v-else-if="!apps.length" class="py-6">
        <p class="voice text-[13px]">{{ t('catalog.empty') }}</p>
        <p class="voice mt-1 text-[12px] text-faint">{{ t('catalog.emptyBody') }}</p>
      </div>

      <ul v-else class="-my-1">
        <li v-for="app in apps" :key="app.product.id" class="border-b border-line-soft last:border-0">
          <!-- Two columns while there is room, one stacked block on a phone:
               a catalogue row is a name, a publisher, a version, and a date,
               which cannot share a 300 px line without clipping the middle. -->
          <RouterLink
            :to="`/marketplace/${app.product.id}`"
            view-transition
            class="flex items-start gap-3 py-3 transition-colors hover:bg-base-200/50"
          >
            <AppMark :picture="app.product.icon" :name="productName(app)" flat />
            <div class="min-w-0 flex-1">
              <div class="flex items-baseline justify-between gap-3">
                <p class="min-w-0 truncate text-[13px] text-base-content">{{ productName(app) }}</p>
                <p v-if="app.latest" class="shrink-0 text-xs tabular-nums text-dim">
                  {{ app.latest.version }}
                </p>
              </div>
              <!-- Both meta values wrap rather than truncate: a publisher name
                   is short enough to survive on a phone, and clipping it hides
                   the one word that says whose product this is. -->
              <div class="flex flex-wrap items-baseline justify-between gap-x-3">
                <p class="text-2xs text-faint">
                  {{ t('product.by', { publisher: publisherName(app) }) }}
                </p>
                <p v-if="app.latest" class="shrink-0 text-2xs text-faint">
                  {{ t('catalog.published', { when: relative(app.latest.published_at) }) }}
                </p>
                <p v-else class="shrink-0 text-2xs text-faint">{{ t('catalog.unreleased') }}</p>
              </div>
            </div>
          </RouterLink>
        </li>
      </ul>

      <template #footer>
        <div class="flex items-center justify-between text-2xs text-faint">
          <span />
          <div class="flex items-center gap-3">
            <span v-if="!loading" class="tabular-nums">
              {{ Math.min(offset + 1, total) }}–{{ Math.min(offset + pageSize, total) }} / {{ total }}
            </span>
            <button
              type="button"
              class="btn btn-ghost btn-xs px-1 text-dim disabled:text-faint"
              :aria-label="t('common.previousPage')"
              :title="t('common.previousPage')"
              :disabled="page === 0 || loading"
              @click="go(page - 1)"
            >
              <ChevronLeft16 aria-hidden="true" />
            </button>
            <button
              type="button"
              class="btn btn-ghost btn-xs px-1 text-dim disabled:text-faint"
              :aria-label="t('common.nextPage')"
              :title="t('common.nextPage')"
              :disabled="page >= lastPage || loading"
              @click="go(page + 1)"
            >
              <ChevronRight16 aria-hidden="true" />
            </button>
          </div>
        </div>
      </template>
    </ConsolePanel>
  </div>
</template>
