<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Add16, Renew16 } from '@carbon/icons-vue'

import { channelApi } from '@/lib/api'
import type { ChannelWrite } from '@/lib/api'
import { ExpressError } from '@/lib/api/client'
import type { Channel } from '@/lib/api/types'
import { localized, relative } from '@/lib/format'
import { useNoticeStore } from '@/stores/notice'
import ConsolePanel from '@/components/ConsolePanel.vue'
import LoadingSkeleton from '@/components/LoadingSkeleton.vue'
import IconButton from '@/components/IconButton.vue'
import ConfirmAction from '@/components/ConfirmAction.vue'

const props = defineProps<{
  productId: string
  canManage: boolean
}>()

const { t, locale } = useI18n()
const notice = useNoticeStore()

/** The channels every deployment ships; they cannot be renamed or deleted. */
const BUILT_IN = new Set(['stable', 'beta', 'nightly', 'rolling'])

const channels = ref<Channel[]>([])
const loading = ref(true)
const error = ref('')
const busy = ref(false)

const creating = ref(false)
const editing = ref<Channel | null>(null)
const pendingDelete = ref<Channel | null>(null)
const draft = ref({ name: '', displayName: '', retention: '' })

const sorted = computed(() =>
  [...channels.value].sort((left, right) => {
    const leftBuiltIn = BUILT_IN.has(left.name) ? 0 : 1
    const rightBuiltIn = BUILT_IN.has(right.name) ? 0 : 1
    if (leftBuiltIn !== rightBuiltIn) return leftBuiltIn - rightBuiltIn
    return left.name.localeCompare(right.name)
  }),
)

function isBuiltIn(channel: Channel): boolean {
  return BUILT_IN.has(channel.name)
}

function label(channel: Channel): string {
  return localized(channel.display_names, channel.display_name || channel.name, locale.value)
}

function retentionText(channel: Channel): string {
  if (channel.artifact_retention === undefined || channel.artifact_retention === null) {
    return t('channels.retentionDefault')
  }
  if (channel.artifact_retention === 0) return t('channels.retentionOff')
  return t('channels.retentionCount', { n: channel.artifact_retention })
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const result = await channelApi.list(props.productId)
    channels.value = result.data ?? []
  } catch (caught) {
    error.value = caught instanceof ExpressError ? caught.message : t('console.error')
  } finally {
    loading.value = false
  }
}

function openCreate() {
  draft.value = { name: '', displayName: '', retention: '' }
  creating.value = true
}

function closeForm() {
  creating.value = false
  editing.value = null
}

function openEdit(channel: Channel) {
  editing.value = channel
  draft.value = {
    name: channel.name,
    displayName: channel.display_name ?? '',
    retention:
      channel.artifact_retention === undefined || channel.artifact_retention === null
        ? ''
        : String(channel.artifact_retention),
  }
}

/** An empty retention field means "use the platform default", not zero. */
function retentionBody(): number | null {
  const value = draft.value.retention.trim()
  if (!value) return null
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : null
}

async function create() {
  const name = draft.value.name.trim()
  if (!name) return
  busy.value = true
  error.value = ''
  const body: ChannelWrite = {
    name,
    display_name: draft.value.displayName.trim() || name,
    artifact_retention: retentionBody(),
  }
  try {
    await channelApi.create(props.productId, body)
    notice.ok(t('channels.created', { name }))
    creating.value = false
    await load()
  } catch (caught) {
    error.value = caught instanceof ExpressError ? caught.message : t('console.error')
  } finally {
    busy.value = false
  }
}

async function save() {
  const channel = editing.value
  if (!channel) return
  busy.value = true
  error.value = ''
  const body: ChannelWrite = {
    display_name: draft.value.displayName.trim() || channel.name,
    artifact_retention: retentionBody(),
  }
  try {
    await channelApi.update(props.productId, channel.id, body)
    notice.ok(t('channels.updated', { name: channel.name }))
    editing.value = null
    await load()
  } catch (caught) {
    error.value = caught instanceof ExpressError ? caught.message : t('console.error')
  } finally {
    busy.value = false
  }
}

async function remove() {
  const channel = pendingDelete.value
  if (!channel) return
  busy.value = true
  try {
    await channelApi.remove(props.productId, channel.id)
    notice.ok(t('channels.deleted', { name: channel.name }))
    pendingDelete.value = null
    await load()
  } catch (caught) {
    notice.fail(caught instanceof ExpressError ? caught.message : t('console.error'))
  } finally {
    busy.value = false
  }
}

onMounted(load)
</script>

<template>
  <ConsolePanel
    :title="t('channels.title')"
    :note="loading ? undefined : String(sorted.length)"
  >
    <template #header>
      <div class="ml-auto flex flex-wrap items-center gap-1 sm:gap-2">
        <IconButton :label="t('common.refresh')" :disabled="loading" @click="load">
          <Renew16 aria-hidden="true" />
        </IconButton>
        <button v-if="canManage" type="button" class="btn btn-ghost btn-xs" @click="openCreate">
          <Add16 class="mr-1" aria-hidden="true" />
          {{ t('channels.create') }}
        </button>
      </div>
    </template>

    <p class="voice mb-3 text-[12px]">{{ t('channels.description') }}</p>
    <p v-if="error" class="mb-3 border-l border-error pl-3 text-xs text-error">{{ error }}</p>

    <LoadingSkeleton v-if="loading" :rows="4" :columns="[2, 3, 2, 2]" header />

    <div v-else-if="!sorted.length" class="py-6">
      <p class="voice text-[13px]">{{ t('channels.empty') }}</p>
    </div>

    <div v-else class="table-bleed overflow-x-auto">
      <table class="ledger ledger-stack">
        <thead>
          <tr>
            <th scope="col">{{ t('channels.columns.name') }}</th>
            <th scope="col">{{ t('channels.columns.display') }}</th>
            <th scope="col">{{ t('channels.columns.latest') }}</th>
            <th scope="col">{{ t('channels.columns.retention') }}</th>
            <th scope="col"><span class="sr-only">{{ t('common.actions') }}</span></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="channel in sorted" :key="channel.id">
            <td class="whitespace-nowrap" :data-label="t('channels.columns.name')">
              <span class="font-mono text-xs text-base-content">{{ channel.name }}</span>
              <span class="chip ml-2" :class="isBuiltIn(channel) ? 'text-faint' : 'text-info'">
                {{ isBuiltIn(channel) ? t('channels.builtIn') : t('channels.custom') }}
              </span>
            </td>
            <td class="text-xs text-dim" :data-label="t('channels.columns.display')">{{ label(channel) }}</td>
            <td class="whitespace-nowrap text-2xs text-dim" :data-label="t('channels.columns.latest')">
              <template v-if="channel.latest">
                <span class="font-mono">{{ channel.latest.version }}</span>
                <span class="ml-1.5 text-faint">{{ relative(channel.latest.published_at) }}</span>
              </template>
              <span v-else class="text-faint">{{ t('channels.noLatest') }}</span>
            </td>
            <td class="whitespace-nowrap text-2xs text-dim" :data-label="t('channels.columns.retention')">{{ retentionText(channel) }}</td>
            <td class="whitespace-nowrap">
              <div class="flex items-center justify-end gap-1">
                <template v-if="canManage">
                  <button type="button" class="btn btn-ghost btn-xs text-dim" @click="openEdit(channel)">
                    {{ t('common.edit') }}
                  </button>
                  <button
                    v-if="!isBuiltIn(channel)"
                    type="button"
                    class="btn btn-ghost btn-xs text-dim hover:text-error"
                    @click="pendingDelete = channel"
                  >
                    {{ t('common.delete') }}
                  </button>
                </template>
                <span v-else-if="isBuiltIn(channel)" class="text-2xs text-faint">—</span>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <template #footer>
      <p v-if="canManage" class="text-2xs text-faint">{{ t('channels.immutable') }}</p>
    </template>
  </ConsolePanel>

  <dialog
    v-if="creating || editing"
    class="modal modal-open"
    aria-modal="true"
    :aria-label="creating ? t('channels.createTitle') : t('channels.updated', { name: editing?.name ?? '' })"
  >
    <div class="modal-box max-w-md rounded-[2px] border border-line bg-base-100 p-0 shadow-none">
      <form @submit.prevent="creating ? create() : save()">
        <div class="px-5 py-4">
          <h2 class="title text-base text-base-content">
            {{ creating ? t('channels.createTitle') : t('common.edit') }}
          </h2>
          <p v-if="creating" class="voice mt-2 text-[12px]">{{ t('channels.createBody') }}</p>
          <div class="mt-4 space-y-3">
            <div v-if="creating">
              <label class="field-label" for="channel-name">{{ t('channels.fieldName') }}</label>
              <input
                id="channel-name"
                v-model="draft.name"
                class="input input-sm"
                required
                autocomplete="off"
                spellcheck="false"
                placeholder="preview"
              />
              <p class="mt-1 text-2xs text-faint">{{ t('channels.fieldNameHint') }}</p>
            </div>
            <div>
              <label class="field-label" for="channel-display">{{ t('channels.fieldDisplayName') }}</label>
              <input id="channel-display" v-model="draft.displayName" class="input input-sm" />
            </div>
            <div>
              <label class="field-label" for="channel-retention">
                {{ t('channels.fieldRetention') }}
              </label>
              <input
                id="channel-retention"
                v-model="draft.retention"
                class="input input-sm"
                inputmode="numeric"
                autocomplete="off"
              />
              <p class="mt-1 text-2xs text-faint">{{ t('channels.fieldRetentionHint') }}</p>
            </div>
          </div>
        </div>
        <footer class="flex items-center justify-end gap-2 border-t border-line px-5 py-3">
          <button type="button" class="btn btn-ghost btn-xs" @click="closeForm">
            {{ t('common.cancel') }}
          </button>
          <button type="submit" class="btn btn-primary btn-xs" :disabled="busy">
            {{ busy ? t('common.saving') : t('common.save') }}
          </button>
        </footer>
      </form>
    </div>
    <button
      type="button"
      class="modal-backdrop bg-black/60"
      :aria-label="t('common.cancel')"
      @click="closeForm"
    />
  </dialog>

  <ConfirmAction
    v-if="pendingDelete"
    :title="t('channels.deleteTitle', { name: pendingDelete.name })"
    :body="t('channels.deleteBody')"
    :action="t('channels.deleteAction')"
    tone="error"
    :busy="busy"
    @confirm="remove"
    @cancel="pendingDelete = null"
  />
</template>
