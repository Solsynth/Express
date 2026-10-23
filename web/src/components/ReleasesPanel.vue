<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Add16, ChevronLeft16, ChevronRight16, Renew16 } from '@carbon/icons-vue'
import { RouterLink } from 'vue-router'

import { channelApi, releaseApi } from '@/lib/api'
import type { ReleaseWrite } from '@/lib/api'
import { ExpressError } from '@/lib/api/client'
import type { ManagedRelease } from '@/lib/api/types'
import { count, localized, relative, tokens } from '@/lib/format'
import { useNoticeStore } from '@/stores/notice'
import ConsolePanel from '@/components/ConsolePanel.vue'
import LoadingSkeleton from '@/components/LoadingSkeleton.vue'
import IconButton from '@/components/IconButton.vue'
import ConfirmAction from '@/components/ConfirmAction.vue'

const props = defineProps<{
  productId: string
  canManage: boolean
  canPublish: boolean
}>()

const { t, locale } = useI18n()
const notice = useNoticeStore()

const releases = ref<ManagedRelease[]>([])
const total = ref(0)
const loading = ref(true)
const error = ref('')
const page = ref(0)
const pageSize = 20
const channelFilter = ref('')
const channels = ref<string[]>([])

const creating = ref(false)
const busy = ref(false)
const draft = ref({
  version: '',
  channels: 'stable',
  title: '',
  notes: '',
  forceUpdate: false,
})

const pendingPublish = ref<ManagedRelease | null>(null)
const pendingYank = ref<ManagedRelease | null>(null)
const pendingDelete = ref<ManagedRelease | null>(null)

const offset = computed(() => page.value * pageSize)
const lastPage = computed(() => Math.max(0, Math.ceil(total.value / pageSize) - 1))

/**
 * The listing is channel-scoped: a release is published to one or more
 * channels, and the API answers per channel. The selector therefore always
 * names one, defaulting to the first channel the product has.
 */
async function load() {
  loading.value = true
  error.value = ''
  try {
    if (!channels.value.length) {
      const listed = await channelApi.list(props.productId)
      channels.value = (listed.data ?? []).map((channel) => channel.name)
      if (!channelFilter.value) {
        channelFilter.value =
          channels.value.find((name) => name === 'stable') ?? channels.value[0] ?? ''
      }
    }
    if (!channelFilter.value) {
      releases.value = []
      total.value = 0
      return
    }
    const result = await releaseApi.manage(props.productId, {
      channel: channelFilter.value,
      limit: pageSize,
      offset: offset.value,
    })
    releases.value = result.data ?? []
    total.value = result.total ?? 0
  } catch (caught) {
    error.value = caught instanceof ExpressError ? caught.message : t('console.error')
  } finally {
    loading.value = false
  }
}

function go(next: number) {
  page.value = Math.min(Math.max(0, next), lastPage.value)
  void load()
}

function openCreate() {
  draft.value = { version: '', channels: 'stable', title: '', notes: '', forceUpdate: false }
  creating.value = true
}

async function create() {
  const version = draft.value.version.trim()
  if (!version) return
  busy.value = true
  error.value = ''
  const body: ReleaseWrite = {
    version,
    channels: tokens(draft.value.channels),
    title: draft.value.title.trim(),
    release_notes: draft.value.notes,
    force_update: draft.value.forceUpdate,
  }
  try {
    await releaseApi.create(props.productId, body)
    notice.ok(t('releases.created', { version }))
    creating.value = false
    page.value = 0
    await load()
  } catch (caught) {
    error.value = caught instanceof ExpressError ? caught.message : t('console.error')
  } finally {
    busy.value = false
  }
}

async function publish() {
  const release = pendingPublish.value
  if (!release) return
  busy.value = true
  try {
    await releaseApi.publish(props.productId, release.id)
    notice.ok(t('releases.published', { version: release.version }))
    pendingPublish.value = null
    await load()
  } catch (caught) {
    notice.fail(caught instanceof ExpressError ? caught.message : t('console.error'))
  } finally {
    busy.value = false
  }
}

async function yank() {
  const release = pendingYank.value
  if (!release) return
  busy.value = true
  try {
    await releaseApi.yank(props.productId, release.id)
    notice.ok(t('releases.yanked', { version: release.version }))
    pendingYank.value = null
    await load()
  } catch (caught) {
    notice.fail(caught instanceof ExpressError ? caught.message : t('console.error'))
  } finally {
    busy.value = false
  }
}

async function remove() {
  const release = pendingDelete.value
  if (!release) return
  busy.value = true
  try {
    await releaseApi.remove(props.productId, release.id)
    notice.ok(t('releases.deleted', { version: release.version }))
    pendingDelete.value = null
    await load()
  } catch (caught) {
    notice.fail(caught instanceof ExpressError ? caught.message : t('console.error'))
  } finally {
    busy.value = false
  }
}

const statusClass: Record<string, string> = {
  draft: 'text-warning border-warning/50',
  published: 'text-success border-success/50',
  yanked: 'text-faint border-line',
}

watch(channelFilter, () => {
  page.value = 0
  void load()
})
watch(
  () => props.productId,
  () => {
    channels.value = []
    channelFilter.value = ''
    page.value = 0
    void load()
  },
)
onMounted(load)
</script>

<template>
  <ConsolePanel
    :title="t('releases.title')"
    :note="loading ? undefined : t('releases.total', { n: total })"
  >
    <template #header>
      <div class="flex flex-wrap items-center gap-1 sm:gap-2">
        <select
          v-model="channelFilter"
          class="select select-xs control-select"
          :aria-label="t('releases.filterChannel')"
        >
          <option v-for="channel in channels" :key="channel" :value="channel">{{ channel }}</option>
        </select>
        <IconButton :label="t('common.refresh')" :disabled="loading" @click="load">
          <Renew16 aria-hidden="true" />
        </IconButton>
        <button v-if="canManage" type="button" class="btn btn-ghost btn-xs" @click="openCreate">
          <Add16 class="mr-1" aria-hidden="true" />
          {{ t('releases.create') }}
        </button>
      </div>
    </template>

    <p v-if="error" class="mb-3 border-l border-error pl-3 text-xs text-error">{{ error }}</p>

    <LoadingSkeleton v-if="loading" :rows="5" :columns="[2, 3, 2, 2, 2]" header />

    <div v-else-if="!releases.length" class="py-6">
      <p class="voice text-[13px]">{{ t('releases.empty') }}</p>
      <p class="voice mt-1 text-[12px] text-faint">{{ t('releases.emptyBody') }}</p>
    </div>

    <div v-else class="table-bleed overflow-x-auto">
      <table class="ledger ledger-stack">
        <thead>
          <tr>
            <th scope="col">{{ t('releases.columns.version') }}</th>
            <th scope="col">{{ t('releases.columns.channels') }}</th>
            <th scope="col">{{ t('releases.columns.status') }}</th>
            <th scope="col">{{ t('releases.columns.artifacts') }}</th>
            <th scope="col">{{ t('releases.columns.downloads') }}</th>
            <th scope="col">{{ t('releases.columns.published') }}</th>
            <th scope="col"><span class="sr-only">{{ t('common.actions') }}</span></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="release in releases" :key="release.id">
            <td class="whitespace-nowrap" :data-label="t('releases.columns.version')">
              <RouterLink
                :to="`/workspace/${productId}/releases/${release.id}`"
                view-transition
                class="font-mono text-xs text-base-content hover:underline"
              >
                {{ release.version }}
              </RouterLink>
              <p v-if="release.title" class="max-w-[220px] truncate text-2xs text-faint">
                {{ localized(release.titles, release.title, locale) }}
              </p>
            </td>
            <td class="whitespace-nowrap text-2xs text-dim" :data-label="t('releases.columns.channels')">
              {{ (release.channels ?? []).join(', ') || '—' }}
            </td>
            <td class="whitespace-nowrap" :data-label="t('releases.columns.status')">
              <span class="chip" :class="statusClass[release.status] ?? ''">
                {{ t(`releases.status.${release.status}`) }}
              </span>
            </td>
            <td class="whitespace-nowrap text-2xs tabular-nums text-dim" :data-label="t('releases.columns.artifacts')">
              {{ release.artifacts.length }}
            </td>
            <td class="whitespace-nowrap text-2xs tabular-nums text-dim" :data-label="t('releases.columns.downloads')">
              {{ count(release.download_count) }}
            </td>
            <td class="whitespace-nowrap text-2xs text-faint" :data-label="t('releases.columns.published')">
              {{ release.published_at ? relative(release.published_at) : '—' }}
            </td>
            <td class="whitespace-nowrap">
              <div class="flex items-center justify-end gap-1">
                <button
                  v-if="canPublish && release.status === 'draft'"
                  type="button"
                  class="btn btn-ghost btn-xs text-dim"
                  @click="pendingPublish = release"
                >
                  {{ t('common.publish') }}
                </button>
                <button
                  v-if="canPublish && release.status === 'published'"
                  type="button"
                  class="btn btn-ghost btn-xs text-dim hover:text-error"
                  @click="pendingYank = release"
                >
                  {{ t('common.yank') }}
                </button>
                <button
                  v-if="canManage && release.status === 'draft'"
                  type="button"
                  class="btn btn-ghost btn-xs text-dim hover:text-error"
                  @click="pendingDelete = release"
                >
                  {{ t('common.delete') }}
                </button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <template #footer>
      <div class="flex items-center justify-between text-2xs text-faint">
        <span />
        <div class="flex items-center gap-3">
          <span v-if="!loading && total" class="tabular-nums">
            {{ offset + 1 }}–{{ Math.min(offset + pageSize, total) }} / {{ total }}
          </span>
          <button
            type="button"
            class="btn btn-ghost btn-xs px-1 text-dim disabled:text-faint"
            :aria-label="t('common.previousPage')"
            :disabled="page === 0 || loading"
            @click="go(page - 1)"
          >
            <ChevronLeft16 aria-hidden="true" />
          </button>
          <button
            type="button"
            class="btn btn-ghost btn-xs px-1 text-dim disabled:text-faint"
            :aria-label="t('common.nextPage')"
            :disabled="page >= lastPage || loading"
            @click="go(page + 1)"
          >
            <ChevronRight16 aria-hidden="true" />
          </button>
        </div>
      </div>
    </template>
  </ConsolePanel>

  <dialog v-if="creating" class="modal modal-open" aria-modal="true" :aria-label="t('releases.createTitle')">
    <div class="modal-box max-w-lg rounded-[2px] border border-line bg-base-100 p-0 shadow-none">
      <form @submit.prevent="create">
        <div class="px-5 py-4">
          <h2 class="title text-base text-base-content">{{ t('releases.createTitle') }}</h2>
          <p class="voice mt-2 text-[12px]">{{ t('releases.createBody') }}</p>
          <div class="mt-4 space-y-3">
            <div>
              <label class="field-label" for="release-version">{{ t('releases.fieldVersion') }}</label>
              <input
                id="release-version"
                v-model="draft.version"
                class="input input-sm"
                required
                autocomplete="off"
                spellcheck="false"
                placeholder="1.4.0"
              />
              <p class="mt-1 text-2xs text-faint">{{ t('releases.fieldVersionHint') }}</p>
            </div>
            <div>
              <label class="field-label" for="release-channels">{{ t('releases.fieldChannels') }}</label>
              <input
                id="release-channels"
                v-model="draft.channels"
                class="input input-sm"
                autocomplete="off"
                spellcheck="false"
              />
              <p class="mt-1 text-2xs text-faint">{{ t('releases.fieldChannelsHint') }}</p>
            </div>
            <div>
              <label class="field-label" for="release-title">{{ t('releases.fieldTitle') }}</label>
              <input id="release-title" v-model="draft.title" class="input input-sm" />
            </div>
            <div>
              <label class="field-label" for="release-notes">{{ t('releases.fieldNotes') }}</label>
              <textarea id="release-notes" v-model="draft.notes" class="textarea textarea-sm" rows="5" />
            </div>
            <label class="flex items-center gap-2 text-2xs text-dim">
              <input v-model="draft.forceUpdate" type="checkbox" class="checkbox checkbox-xs" />
              {{ t('releases.fieldForceUpdate') }}
            </label>
          </div>
        </div>
        <footer class="flex items-center justify-end gap-2 border-t border-line px-5 py-3">
          <button type="button" class="btn btn-ghost btn-xs" @click="creating = false">
            {{ t('common.cancel') }}
          </button>
          <button type="submit" class="btn btn-primary btn-xs" :disabled="busy || !draft.version.trim()">
            {{ busy ? t('common.saving') : t('common.create') }}
          </button>
        </footer>
      </form>
    </div>
    <button
      type="button"
      class="modal-backdrop bg-black/60"
      :aria-label="t('common.cancel')"
      @click="creating = false"
    />
  </dialog>

  <ConfirmAction
    v-if="pendingPublish"
    :title="t('releases.publishTitle', { version: pendingPublish.version })"
    :body="t('releases.publishBody')"
    :action="t('releases.publishAction')"
    :busy="busy"
    @confirm="publish"
    @cancel="pendingPublish = null"
  />
  <ConfirmAction
    v-if="pendingYank"
    :title="t('releases.yankTitle', { version: pendingYank.version })"
    :body="t('releases.yankBody')"
    :action="t('releases.yankAction')"
    tone="error"
    :busy="busy"
    @confirm="yank"
    @cancel="pendingYank = null"
  />
  <ConfirmAction
    v-if="pendingDelete"
    :title="t('releases.deleteTitle', { version: pendingDelete.version })"
    :body="t('releases.deleteBody')"
    :action="t('releases.deleteAction')"
    tone="error"
    :busy="busy"
    @confirm="remove"
    @cancel="pendingDelete = null"
  />
</template>
