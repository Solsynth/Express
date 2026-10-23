<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Add16, Renew16 } from '@carbon/icons-vue'

import { syncApi } from '@/lib/api'
import type { SyncConnectionWrite, SyncLinkWrite } from '@/lib/api'
import { ExpressError } from '@/lib/api/client'
import type {
  Publisher,
  SyncConnection,
  SyncJob,
  SyncLink,
} from '@/lib/api/types'
import { relative, tokens, utcStamp } from '@/lib/format'
import { useNoticeStore } from '@/stores/notice'
import ConsolePanel from '@/components/ConsolePanel.vue'
import LoadingSkeleton from '@/components/LoadingSkeleton.vue'
import IconButton from '@/components/IconButton.vue'
import ConfirmAction from '@/components/ConfirmAction.vue'
import TabBox from '@/components/TabBox.vue'
import type { Tab } from '@/components/tab'

const props = defineProps<{
  productId: string
  publisher: Publisher | null
  canManage: boolean
}>()

const { t } = useI18n()
const notice = useNoticeStore()

const tab = ref('links')
const connections = ref<SyncConnection[]>([])
const providers = ref<string[]>([])
const links = ref<SyncLink[]>([])
const jobs = ref<SyncJob[]>([])
const loading = ref(true)
const error = ref('')
const busy = ref(false)

const connectionForm = ref({ provider: '', name: '', baseURL: '', token: '' })
const creatingConnection = ref(false)
const pendingConnectionDelete = ref<SyncConnection | null>(null)

const linkForm = ref({
  connectionId: '',
  repository: '',
  include: '',
  exclude: '',
  draft: false,
  prerelease: false,
  enabled: true,
})
const creatingLink = ref(false)
const pendingLinkDelete = ref<SyncLink | null>(null)

const tabs = computed<Tab[]>(() => [
  { key: 'links', label: t('sync.links') },
  { key: 'connections', label: t('sync.connections') },
  { key: 'jobs', label: t('sync.jobs') },
])

const publisherName = computed(() => props.publisher?.name ?? '')

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [linkResult, jobResult, connectionResult] = await Promise.all([
      syncApi.links(props.productId),
      syncApi.jobs(props.productId, { limit: 25 }).catch(() => ({ data: [], total: 0, limit: 0, offset: 0 })),
      publisherName.value
        ? syncApi.connections(publisherName.value)
        : Promise.resolve({ data: [], providers: [] }),
    ])
    links.value = linkResult.data ?? []
    jobs.value = jobResult.data ?? []
    connections.value = connectionResult.data ?? []
    providers.value = connectionResult.providers ?? []
  } catch (caught) {
    error.value = caught instanceof ExpressError ? caught.message : t('console.error')
  } finally {
    loading.value = false
  }
}

function openConnection() {
  connectionForm.value = {
    provider: providers.value[0] ?? '',
    name: '',
    baseURL: '',
    token: '',
  }
  creatingConnection.value = true
}

async function createConnection() {
  const name = connectionForm.value.name.trim()
  const provider = connectionForm.value.provider
  if (!name || !provider) return
  busy.value = true
  error.value = ''
  const body: SyncConnectionWrite = {
    provider,
    name,
    base_url: connectionForm.value.baseURL.trim() || undefined,
    // The provider token travels inside the sealed secret map; the console
    // never sees it again after this request.
    secret: connectionForm.value.token.trim()
      ? { token: connectionForm.value.token.trim() }
      : undefined,
  }
  try {
    await syncApi.createConnection(publisherName.value, body)
    notice.ok(t('sync.connectionCreated', { name }))
    creatingConnection.value = false
    await load()
  } catch (caught) {
    error.value = caught instanceof ExpressError ? caught.message : t('console.error')
  } finally {
    busy.value = false
  }
}

async function removeConnection() {
  const connection = pendingConnectionDelete.value
  if (!connection) return
  busy.value = true
  try {
    await syncApi.removeConnection(publisherName.value, connection.id)
    notice.ok(t('sync.connectionDeleted', { name: connection.name }))
    pendingConnectionDelete.value = null
    await load()
  } catch (caught) {
    notice.fail(caught instanceof ExpressError ? caught.message : t('console.error'))
  } finally {
    busy.value = false
  }
}

function openLink() {
  linkForm.value = {
    connectionId: connections.value[0]?.id ?? '',
    repository: '',
    include: '',
    exclude: '',
    draft: false,
    prerelease: false,
    enabled: true,
  }
  creatingLink.value = true
}

async function createLink() {
  const repository = linkForm.value.repository.trim()
  if (!repository || !linkForm.value.connectionId) return
  busy.value = true
  error.value = ''
  const body: SyncLinkWrite = {
    connection_id: linkForm.value.connectionId,
    repository,
    channels: {
      include: tokens(linkForm.value.include),
      exclude: tokens(linkForm.value.exclude),
    },
    draft: linkForm.value.draft,
    prerelease: linkForm.value.prerelease,
    enabled: linkForm.value.enabled,
  }
  try {
    await syncApi.createLink(props.productId, body)
    notice.ok(t('sync.linkCreated', { repository }))
    creatingLink.value = false
    await load()
  } catch (caught) {
    error.value = caught instanceof ExpressError ? caught.message : t('console.error')
  } finally {
    busy.value = false
  }
}

async function removeLink() {
  const link = pendingLinkDelete.value
  if (!link) return
  busy.value = true
  try {
    await syncApi.removeLink(props.productId, link.id)
    notice.ok(t('sync.linkDeleted', { repository: link.repository }))
    pendingLinkDelete.value = null
    await load()
  } catch (caught) {
    notice.fail(caught instanceof ExpressError ? caught.message : t('console.error'))
  } finally {
    busy.value = false
  }
}

async function retry(job: SyncJob) {
  try {
    await syncApi.retryJob(props.productId, job.id)
    notice.ok(t('sync.jobRetried'))
    await load()
  } catch (caught) {
    notice.fail(caught instanceof ExpressError ? caught.message : t('console.error'))
  }
}

const stateClass: Record<string, string> = {
  pending: 'text-warning border-warning/50',
  delivering: 'text-info border-info/50',
  delivered: 'text-success border-success/50',
  failed: 'text-error border-error/50',
}

onMounted(load)
</script>

<template>
  <div class="space-y-6">
    <TabBox :tabs="tabs" :active="tab" :label="t('sync.title')" @select="tab = $event" />

    <p v-if="error" class="border-l border-error pl-3 text-xs text-error">{{ error }}</p>

    <template v-if="tab === 'links'">
      <ConsolePanel :title="t('sync.links')" :note="loading ? undefined : String(links.length)">
        <template #header>
          <div class="flex flex-wrap items-center gap-1 sm:gap-2">
            <IconButton :label="t('common.refresh')" :disabled="loading" @click="load">
              <Renew16 aria-hidden="true" />
            </IconButton>
            <button
              v-if="canManage && connections.length"
              type="button"
              class="btn btn-ghost btn-xs"
              @click="openLink"
            >
              <Add16 class="mr-1" aria-hidden="true" />
              {{ t('sync.createLink') }}
            </button>
          </div>
        </template>

        <p class="voice mb-3 text-[12px]">{{ t('sync.description') }}</p>

        <LoadingSkeleton v-if="loading" :rows="3" :columns="[3, 2, 2]" header />

        <div v-else-if="!links.length" class="py-6">
          <p class="voice text-[13px]">{{ t('sync.linksEmpty') }}</p>
          <p v-if="!connections.length" class="voice mt-1 text-[12px] text-faint">
            {{ t('sync.connectionsEmpty') }}
          </p>
        </div>

        <div v-else class="table-bleed overflow-x-auto">
          <table class="ledger ledger-stack">
            <thead>
              <tr>
                <th scope="col">{{ t('sync.fieldRepository') }}</th>
                <th scope="col">{{ t('sync.fieldConnection') }}</th>
                <th scope="col">{{ t('sync.fieldInclude') }}</th>
                <th scope="col">{{ t('common.status') }}</th>
                <th scope="col">{{ t('common.updated') }}</th>
                <th scope="col"><span class="sr-only">{{ t('common.actions') }}</span></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="link in links" :key="link.id">
                <td class="whitespace-nowrap font-mono text-xs text-base-content" :data-label="t('sync.fieldRepository')">
                  {{ link.repository }}
                </td>
                <td class="whitespace-nowrap text-2xs text-dim" :data-label="t('sync.fieldConnection')">
                  {{ link.connection }}
                  <span class="ml-1 text-faint">{{ link.provider }}</span>
                </td>
                <td class="text-2xs text-dim" :data-label="t('sync.fieldInclude')">
                  {{ (link.channels?.include ?? []).join(', ') || t('sync.fieldIncludeHint') }}
                </td>
                <td class="whitespace-nowrap" :data-label="t('common.status')">
                  <span class="chip" :class="link.enabled ? 'text-success border-success/50' : 'text-faint'">
                    {{ link.enabled ? t('common.enabled') : t('common.disabled') }}
                  </span>
                </td>
                <td class="whitespace-nowrap text-2xs text-faint" :data-label="t('common.updated')">
                  {{ link.last_synced_at ? relative(link.last_synced_at) : t('common.never') }}
                </td>
                <td class="whitespace-nowrap">
                  <div class="flex justify-end">
                    <button
                      v-if="canManage"
                      type="button"
                      class="btn btn-ghost btn-xs text-dim hover:text-error"
                      @click="pendingLinkDelete = link"
                    >
                      {{ t('common.delete') }}
                    </button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </ConsolePanel>
    </template>

    <template v-else-if="tab === 'connections'">
      <ConsolePanel
        :title="t('sync.connections')"
        :note="loading ? undefined : String(connections.length)"
      >
        <template #header>
          <div class="flex flex-wrap items-center gap-1 sm:gap-2">
            <button v-if="canManage" type="button" class="btn btn-ghost btn-xs" @click="openConnection">
              <Add16 class="mr-1" aria-hidden="true" />
              {{ t('sync.createConnection') }}
            </button>
          </div>
        </template>

        <LoadingSkeleton v-if="loading" :rows="3" :columns="[3, 2, 2]" header />

        <div v-else-if="!connections.length" class="py-6">
          <p class="voice text-[13px]">{{ t('sync.connectionsEmpty') }}</p>
        </div>

        <div v-else class="table-bleed overflow-x-auto">
          <table class="ledger ledger-stack">
            <thead>
              <tr>
                <th scope="col">{{ t('sync.fieldName') }}</th>
                <th scope="col">{{ t('sync.fieldProvider') }}</th>
                <th scope="col">{{ t('sync.fieldBaseURL') }}</th>
                <th scope="col">{{ t('sync.fieldToken') }}</th>
                <th scope="col"><span class="sr-only">{{ t('common.actions') }}</span></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="connection in connections" :key="connection.id">
                <td class="text-xs text-base-content" :data-label="t('sync.fieldName')">{{ connection.name }}</td>
                <td class="whitespace-nowrap font-mono text-2xs text-dim" :data-label="t('sync.fieldProvider')">{{ connection.provider }}</td>
                <td class="truncate text-2xs text-faint" :data-label="t('sync.fieldBaseURL')">{{ connection.base_url || '—' }}</td>
                <td class="whitespace-nowrap" :data-label="t('sync.fieldToken')">
                  <span class="chip" :class="connection.has_secret ? 'text-success border-success/50' : 'text-faint'">
                    {{ connection.has_secret ? t('sync.hasSecret') : t('sync.noSecret') }}
                  </span>
                </td>
                <td class="whitespace-nowrap">
                  <div class="flex justify-end">
                    <button
                      v-if="canManage"
                      type="button"
                      class="btn btn-ghost btn-xs text-dim hover:text-error"
                      @click="pendingConnectionDelete = connection"
                    >
                      {{ t('common.delete') }}
                    </button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </ConsolePanel>
    </template>

    <template v-else>
      <ConsolePanel :title="t('sync.jobs')" :note="loading ? undefined : String(jobs.length)">
        <LoadingSkeleton v-if="loading" :rows="4" :columns="[2, 2, 2, 2]" header />

        <div v-else-if="!jobs.length" class="py-6">
          <p class="voice text-[13px]">{{ t('sync.jobsEmpty') }}</p>
        </div>

        <div v-else class="table-bleed overflow-x-auto">
          <table class="ledger ledger-stack">
            <thead>
              <tr>
                <th scope="col">{{ t('sync.columns.version') }}</th>
                <th scope="col">{{ t('sync.columns.action') }}</th>
                <th scope="col">{{ t('sync.columns.state') }}</th>
                <th scope="col">{{ t('sync.columns.attempts') }}</th>
                <th scope="col">{{ t('sync.columns.updated') }}</th>
                <th scope="col"><span class="sr-only">{{ t('common.actions') }}</span></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="job in jobs" :key="job.id">
                <td class="whitespace-nowrap" :data-label="t('sync.columns.version')">
                  <a
                    v-if="job.external_url"
                    :href="job.external_url"
                    class="font-mono text-xs text-base-content hover:underline"
                    target="_blank"
                    rel="noreferrer"
                  >
                    {{ job.version }}
                  </a>
                  <span v-else class="font-mono text-xs text-base-content">{{ job.version }}</span>
                  <p v-if="job.last_error" class="max-w-[280px] truncate text-2xs text-error" :title="job.last_error">
                    {{ job.last_error }}
                  </p>
                </td>
                <td class="whitespace-nowrap text-2xs text-dim" :data-label="t('sync.columns.action')">{{ job.action }}</td>
                <td class="whitespace-nowrap" :data-label="t('sync.columns.state')">
                  <span class="chip" :class="stateClass[job.state] ?? ''">{{ job.state }}</span>
                </td>
                <td class="whitespace-nowrap text-2xs tabular-nums text-dim" :data-label="t('sync.columns.attempts')">{{ job.attempts }}</td>
                <td class="whitespace-nowrap text-2xs text-faint" :title="utcStamp(job.updated_at)" :data-label="t('sync.columns.updated')">
                  {{ relative(job.updated_at) }}
                </td>
                <td class="whitespace-nowrap">
                  <div class="flex justify-end">
                    <button
                      v-if="canManage && job.state === 'failed'"
                      type="button"
                      class="btn btn-ghost btn-xs text-dim"
                      @click="retry(job)"
                    >
                      {{ t('sync.retry') }}
                    </button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </ConsolePanel>
    </template>

    <dialog
      v-if="creatingConnection"
      class="modal modal-open"
      aria-modal="true"
      :aria-label="t('sync.createConnectionTitle')"
    >
      <div class="modal-box max-w-md rounded-[2px] border border-line bg-base-100 p-0 shadow-none">
        <form @submit.prevent="createConnection">
          <div class="px-5 py-4">
            <h2 class="title text-base text-base-content">{{ t('sync.createConnectionTitle') }}</h2>
            <p class="voice mt-2 text-[12px]">{{ t('sync.createConnectionBody') }}</p>
            <div class="mt-4 space-y-3">
              <div>
                <label class="field-label" for="sync-provider">{{ t('sync.fieldProvider') }}</label>
                <select id="sync-provider" v-model="connectionForm.provider" class="select select-sm">
                  <option v-for="provider in providers" :key="provider" :value="provider">
                    {{ provider }}
                  </option>
                </select>
              </div>
              <div>
                <label class="field-label" for="sync-name">{{ t('sync.fieldName') }}</label>
                <input id="sync-name" v-model="connectionForm.name" class="input input-sm" required />
              </div>
              <div>
                <label class="field-label" for="sync-base">{{ t('sync.fieldBaseURL') }}</label>
                <input
                  id="sync-base"
                  v-model="connectionForm.baseURL"
                  class="input input-sm"
                  autocomplete="off"
                  spellcheck="false"
                  placeholder="https://github.example.com"
                />
                <p class="mt-1 text-2xs text-faint">{{ t('sync.fieldBaseURLHint') }}</p>
              </div>
              <div>
                <label class="field-label" for="sync-token">{{ t('sync.fieldToken') }}</label>
                <input
                  id="sync-token"
                  v-model="connectionForm.token"
                  type="password"
                  class="input input-sm"
                  autocomplete="new-password"
                />
                <p class="mt-1 text-2xs text-faint">{{ t('sync.fieldTokenHint') }}</p>
              </div>
            </div>
          </div>
          <footer class="flex items-center justify-end gap-2 border-t border-line px-5 py-3">
            <button type="button" class="btn btn-ghost btn-xs" @click="creatingConnection = false">
              {{ t('common.cancel') }}
            </button>
            <button
              type="submit"
              class="btn btn-primary btn-xs"
              :disabled="busy || !connectionForm.name.trim() || !connectionForm.provider"
            >
              {{ busy ? t('common.saving') : t('common.create') }}
            </button>
          </footer>
        </form>
      </div>
      <button
        type="button"
        class="modal-backdrop bg-black/60"
        :aria-label="t('common.cancel')"
        @click="creatingConnection = false"
      />
    </dialog>

    <dialog v-if="creatingLink" class="modal modal-open" aria-modal="true" :aria-label="t('sync.createLinkTitle')">
      <div class="modal-box max-w-md rounded-[2px] border border-line bg-base-100 p-0 shadow-none">
        <form @submit.prevent="createLink">
          <div class="px-5 py-4">
            <h2 class="title text-base text-base-content">{{ t('sync.createLinkTitle') }}</h2>
            <p class="voice mt-2 text-[12px]">{{ t('sync.createLinkBody') }}</p>
            <div class="mt-4 space-y-3">
              <div>
                <label class="field-label" for="link-connection">{{ t('sync.fieldConnection') }}</label>
                <select id="link-connection" v-model="linkForm.connectionId" class="select select-sm">
                  <option v-for="connection in connections" :key="connection.id" :value="connection.id">
                    {{ connection.name }} · {{ connection.provider }}
                  </option>
                </select>
              </div>
              <div>
                <label class="field-label" for="link-repository">{{ t('sync.fieldRepository') }}</label>
                <input
                  id="link-repository"
                  v-model="linkForm.repository"
                  class="input input-sm"
                  required
                  autocomplete="off"
                  spellcheck="false"
                  placeholder="solsynth/client"
                />
                <p class="mt-1 text-2xs text-faint">{{ t('sync.fieldRepositoryHint') }}</p>
              </div>
              <div>
                <label class="field-label" for="link-include">{{ t('sync.fieldInclude') }}</label>
                <input
                  id="link-include"
                  v-model="linkForm.include"
                  class="input input-sm"
                  autocomplete="off"
                  spellcheck="false"
                  placeholder="stable, beta"
                />
                <p class="mt-1 text-2xs text-faint">{{ t('sync.fieldIncludeHint') }}</p>
              </div>
              <div>
                <label class="field-label" for="link-exclude">{{ t('sync.fieldExclude') }}</label>
                <input id="link-exclude" v-model="linkForm.exclude" class="input input-sm" autocomplete="off" />
              </div>
              <label class="flex items-center gap-2 text-2xs text-dim">
                <input v-model="linkForm.draft" type="checkbox" class="checkbox checkbox-xs" />
                {{ t('sync.fieldDraft') }}
              </label>
              <label class="flex items-center gap-2 text-2xs text-dim">
                <input v-model="linkForm.prerelease" type="checkbox" class="checkbox checkbox-xs" />
                {{ t('sync.fieldPrerelease') }}
              </label>
            </div>
          </div>
          <footer class="flex items-center justify-end gap-2 border-t border-line px-5 py-3">
            <button type="button" class="btn btn-ghost btn-xs" @click="creatingLink = false">
              {{ t('common.cancel') }}
            </button>
            <button
              type="submit"
              class="btn btn-primary btn-xs"
              :disabled="busy || !linkForm.repository.trim() || !linkForm.connectionId"
            >
              {{ busy ? t('common.saving') : t('common.create') }}
            </button>
          </footer>
        </form>
      </div>
      <button
        type="button"
        class="modal-backdrop bg-black/60"
        :aria-label="t('common.cancel')"
        @click="creatingLink = false"
      />
    </dialog>

    <ConfirmAction
      v-if="pendingConnectionDelete"
      :title="t('sync.deleteConnectionTitle', { name: pendingConnectionDelete.name })"
      :body="t('sync.deleteConnectionBody')"
      :action="t('common.delete')"
      tone="error"
      :busy="busy"
      @confirm="removeConnection"
      @cancel="pendingConnectionDelete = null"
    />
    <ConfirmAction
      v-if="pendingLinkDelete"
      :title="t('sync.deleteLinkTitle', { repository: pendingLinkDelete.repository })"
      :body="t('sync.deleteLinkBody')"
      :action="t('common.delete')"
      tone="error"
      :busy="busy"
      @confirm="removeLink"
      @cancel="pendingLinkDelete = null"
    />
  </div>
</template>
