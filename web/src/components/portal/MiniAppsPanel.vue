<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Add16, CloudUpload16, Download16, Renew16 } from '@carbon/icons-vue'

import { miniAppApi } from '@/lib/api/portal'
import type { MiniAppWrite } from '@/lib/api/portal'
import { ExpressError } from '@/lib/api/client'
import type { DevProject, MiniApp, PackageUpload, PortalScope } from '@/lib/api/types'
import { bytes, count } from '@/lib/format'
import { MINI_APP_STAGES, miniAppStage } from '@/lib/portal'
import type { MiniAppStage } from '@/lib/portal'
import { useNoticeStore } from '@/stores/notice'
import { useSessionStore } from '@/stores/session'
import ConsolePanel from '@/components/ConsolePanel.vue'
import LoadingSkeleton from '@/components/LoadingSkeleton.vue'
import IconButton from '@/components/IconButton.vue'
import ConfirmAction from '@/components/ConfirmAction.vue'
import TabBox from '@/components/TabBox.vue'
import type { Tab } from '@/components/tab'

const props = defineProps<{
  /** The publisher and project every private portal call is scoped to. */
  scope: PortalScope | null
  /** The project whose mini apps this panel lists. */
  project: DevProject | null
}>()

const { t } = useI18n()
const notice = useNoticeStore()
const session = useSessionStore()

// ── The mini app ledger ──

const apps = ref<MiniApp[]>([])
const loading = ref(true)
const error = ref('')

// ── The selected mini app, reloaded whenever the selection moves ──

const selectedId = ref('')
const detail = ref<MiniApp | null>(null)
const detailLoading = ref(false)
const detailError = ref('')
/**
 * One detail fetch at a time. A slower answer for an app the reader has already
 * moved away from must not become the panel's answer for the current one.
 */
let detailToken = 0

// ── Dialogs ──

const creating = ref(false)
const editing = ref<MiniApp | null>(null)
const pendingDelete = ref<MiniApp | null>(null)
const uploadOpen = ref(false)
const uploadResult = ref<PackageUpload | null>(null)
const uploadError = ref('')
const uploadFile = ref<File | null>(null)

const busy = ref(false)
const formError = ref('')
const draft = ref({ slug: '', stage: 'development' as MiniAppStage })
const editDraft = ref({ stage: 'development' as MiniAppStage })

const canView = computed(() => session.can('mini.apps.view'))
const canCreate = computed(() => session.can('mini.apps.create'))
const canUpdate = computed(() => session.can('mini.apps.update'))
const canDelete = computed(() => session.can('mini.apps.delete'))
const canUpload = computed(() => session.can('mini.apps.package.upload'))

const tabs = computed<Tab[]>(() => apps.value.map((app) => ({ key: app.id, label: app.slug })))
const selectedSlug = computed(
  () => apps.value.find((app) => app.id === selectedId.value)?.slug ?? '',
)
const selected = computed<MiniApp | null>(() => {
  const app = detail.value
  return app && app.id === selectedId.value ? app : null
})
const manifest = computed(() => selected.value?.manifest ?? null)
const hasPackage = computed(() => {
  const app = selected.value
  if (!app) return false
  return app.package_sha256 !== undefined || app.package_size !== undefined
})

/** The identity of the scope, not the object: the store replaces it wholesale. */
const scopeKey = computed(() => (props.scope ? `${props.scope.dev}/${props.scope.proj}` : ''))

async function load() {
  const scope = props.scope
  loading.value = true
  error.value = ''
  if (!scope || !canView.value) {
    apps.value = []
    loading.value = false
    return
  }
  try {
    const list = await miniAppApi.list(scope)
    apps.value = list
    // A mini app deleted in another tab would leave a selection pointing at a
    // record this project no longer serves.
    if (selectedId.value && !list.some((app) => app.id === selectedId.value)) {
      selectedId.value = ''
    }
  } catch (caught) {
    error.value = caught instanceof ExpressError ? caught.message : t('portal.error')
  } finally {
    loading.value = false
  }
}

async function loadDetail() {
  const scope = props.scope
  const id = selectedId.value
  const token = ++detailToken
  detailLoading.value = true
  detailError.value = ''
  if (!scope || !id || !canView.value) {
    detail.value = null
    detailLoading.value = false
    return
  }
  // The previous record leaves only when it belongs to another mini app: a
  // refresh of the current one keeps the panel painted while the answer travels.
  if (detail.value?.id !== id) detail.value = null
  try {
    const loaded = await miniAppApi.get(scope, id)
    if (token === detailToken) detail.value = loaded
  } catch (caught) {
    if (token === detailToken) {
      detailError.value = caught instanceof ExpressError ? caught.message : t('portal.error')
    }
  } finally {
    if (token === detailToken) detailLoading.value = false
  }
}

function select(id: string) {
  if (selectedId.value === id) return
  selectedId.value = id
}

watch(
  scopeKey,
  () => {
    selectedId.value = ''
    detail.value = null
    creating.value = false
    editing.value = null
    pendingDelete.value = null
    uploadOpen.value = false
    void load()
  },
  { immediate: true },
)

watch(selectedId, () => void loadDetail())

// ── Create and edit ──

function openCreate() {
  draft.value = { slug: '', stage: 'development' }
  formError.value = ''
  creating.value = true
}

function openEdit(app: MiniApp) {
  editing.value = app
  editDraft.value = { stage: miniAppStage(app.stage) }
  formError.value = ''
}

function closeForm() {
  creating.value = false
  editing.value = null
  formError.value = ''
}

async function create() {
  const scope = props.scope
  const slug = draft.value.slug.trim()
  if (!scope || !slug) return
  busy.value = true
  formError.value = ''
  const body: MiniAppWrite = { slug, stage: draft.value.stage }
  try {
    const created = await miniAppApi.create(scope, body)
    notice.ok(t('portalMiniApps.created', { name: created.slug }))
    creating.value = false
    await load()
    select(created.id)
  } catch (caught) {
    formError.value = caught instanceof ExpressError ? caught.message : t('portal.error')
  } finally {
    busy.value = false
  }
}

async function save() {
  const scope = props.scope
  const app = editing.value
  if (!scope || !app) return
  busy.value = true
  formError.value = ''
  const body: MiniAppWrite = { stage: editDraft.value.stage }
  // The PATCH clears whichever of the two it is not given, so the current ones
  // travel back with the record even though this form never edits them.
  if (app.icon) body.icon_id = app.icon.id
  if (app.background) body.background_id = app.background.id
  try {
    await miniAppApi.update(scope, app.id, body)
    notice.ok(t('portalMiniApps.updated', { name: app.slug }))
    editing.value = null
    await load()
    if (selectedId.value === app.id) await loadDetail()
  } catch (caught) {
    formError.value = caught instanceof ExpressError ? caught.message : t('portal.error')
  } finally {
    busy.value = false
  }
}

async function remove() {
  const scope = props.scope
  const app = pendingDelete.value
  if (!scope || !app) return
  busy.value = true
  try {
    await miniAppApi.remove(scope, app.id)
    notice.ok(t('portalMiniApps.deleted'))
    pendingDelete.value = null
    if (selectedId.value === app.id) selectedId.value = ''
    await load()
  } catch (caught) {
    notice.fail(caught instanceof ExpressError ? caught.message : t('portal.error'))
  } finally {
    busy.value = false
  }
}

// ── Package upload ──

function openUpload() {
  uploadFile.value = null
  uploadError.value = ''
  uploadResult.value = null
  uploadOpen.value = true
}

function closeUpload() {
  uploadOpen.value = false
  uploadFile.value = null
  uploadError.value = ''
  uploadResult.value = null
}

function onFile(event: Event) {
  const input = event.target as HTMLInputElement
  uploadFile.value = input.files?.[0] ?? null
}

async function upload() {
  const scope = props.scope
  const app = selected.value
  const file = uploadFile.value
  if (!scope || !app || !file) return
  busy.value = true
  uploadError.value = ''
  try {
    uploadResult.value = await miniAppApi.uploadPackage(scope, app.id, file)
    notice.ok(t('portalMiniApps.package.uploaded'))
    await load()
    await loadDetail()
  } catch (caught) {
    uploadError.value = caught instanceof ExpressError ? caught.message : t('portal.error')
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <ConsolePanel v-if="!scope" :title="t('portalMiniApps.title')">
    <p class="voice text-[13px]">{{ t('portal.scopeMissing') }}</p>
  </ConsolePanel>

  <template v-else>
    <ConsolePanel
      :title="t('portalMiniApps.title')"
      :note="loading ? undefined : String(apps.length)"
    >
      <template #header>
        <div class="flex flex-wrap items-center gap-1 sm:gap-2">
          <IconButton :label="t('common.refresh')" :disabled="loading" @click="load">
            <Renew16 aria-hidden="true" />
          </IconButton>
          <button v-if="canCreate" type="button" class="btn btn-ghost btn-xs" @click="openCreate">
            <Add16 class="mr-1" aria-hidden="true" />
            {{ t('portalMiniApps.create') }}
          </button>
        </div>
      </template>

      <p class="voice mb-3 text-[12px]">{{ t('portalMiniApps.description') }}</p>
      <p v-if="error" class="mb-3 border-l border-error pl-3 text-xs text-error">{{ error }}</p>

      <TabBox
        v-if="tabs.length"
        :tabs="tabs"
        :active="selectedId"
        :label="t('portalMiniApps.title')"
        @select="select"
      />

      <LoadingSkeleton v-if="loading" :rows="4" :columns="[3, 2, 2, 1, 1]" header />

      <div v-else-if="!apps.length" class="py-6">
        <p class="voice text-[13px]">{{ t('portalMiniApps.empty') }}</p>
      </div>

      <div v-else class="table-bleed overflow-x-auto">
        <table class="ledger ledger-stack">
          <thead>
            <tr>
              <th scope="col">{{ t('portalMiniApps.columns.slug') }}</th>
              <th scope="col">{{ t('portalMiniApps.columns.name') }}</th>
              <th scope="col">{{ t('portalMiniApps.columns.version') }}</th>
              <th scope="col">{{ t('portalMiniApps.columns.stage') }}</th>
              <th scope="col">{{ t('portalMiniApps.columns.downloads') }}</th>
              <th scope="col"><span class="sr-only">{{ t('common.actions') }}</span></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="app in apps" :key="app.id">
              <td class="whitespace-nowrap" :data-label="t('portalMiniApps.columns.slug')">
                <span class="font-mono text-xs text-base-content">{{ app.slug }}</span>
              </td>
              <td class="text-xs text-dim" :data-label="t('portalMiniApps.columns.name')">
                {{ app.name || '—' }}
              </td>
              <td
                class="whitespace-nowrap font-mono text-2xs text-dim"
                :data-label="t('portalMiniApps.columns.version')"
              >
                {{ app.version }}
              </td>
              <td
                class="whitespace-nowrap text-2xs text-dim"
                :data-label="t('portalMiniApps.columns.stage')"
              >
                {{ t(`portalMiniApps.stages.${miniAppStage(app.stage)}`) }}
              </td>
              <td
                class="whitespace-nowrap text-2xs text-dim"
                :data-label="t('portalMiniApps.columns.downloads')"
              >
                {{ count(app.download_count) }}
              </td>
              <td class="whitespace-nowrap">
                <div class="flex items-center justify-end gap-1">
                  <button
                    v-if="canUpdate"
                    type="button"
                    class="btn btn-ghost btn-xs text-dim"
                    @click="openEdit(app)"
                  >
                    {{ t('common.edit') }}
                  </button>
                  <button
                    v-if="canDelete"
                    type="button"
                    class="btn btn-ghost btn-xs text-dim hover:text-error"
                    @click="pendingDelete = app"
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

    <ConsolePanel :title="t('portalMiniApps.package.title')" :note="selectedSlug || undefined">
      <template #header>
        <button
          v-if="canUpload && selectedId"
          type="button"
          class="btn btn-ghost btn-xs"
          @click="openUpload"
        >
          <CloudUpload16 class="mr-1" aria-hidden="true" />
          {{ t('portalMiniApps.package.upload') }}
        </button>
      </template>

      <p v-if="detailError" class="mb-3 border-l border-error pl-3 text-xs text-error">
        {{ detailError }}
      </p>

      <LoadingSkeleton v-if="selectedId && detailLoading" :rows="2" :columns="[3, 2]" header />

      <div v-else-if="!selected" class="py-4">
        <p class="voice text-[13px]">{{ t('portalMiniApps.empty') }}</p>
      </div>

      <div v-else-if="!hasPackage" class="py-4">
        <p class="voice text-[13px]">{{ t('portalMiniApps.package.empty') }}</p>
      </div>

      <div v-else class="table-bleed overflow-x-auto">
        <table class="ledger ledger-stack">
          <thead>
            <tr>
              <th scope="col">{{ t('portalMiniApps.package.size') }}</th>
              <th scope="col">{{ t('portalMiniApps.package.sha256') }}</th>
              <th scope="col"><span class="sr-only">{{ t('common.actions') }}</span></th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <td
                class="whitespace-nowrap text-2xs text-dim"
                :data-label="t('portalMiniApps.package.size')"
              >
                {{ bytes(selected.package_size) }}
              </td>
              <td
                class="break-all font-mono text-2xs text-dim"
                :data-label="t('portalMiniApps.package.sha256')"
              >
                {{ selected.package_sha256 }}
              </td>
              <td class="whitespace-nowrap">
                <div class="flex items-center justify-end gap-1">
                  <a
                    v-if="selected.package_url"
                    class="btn btn-ghost btn-xs text-dim"
                    :href="selected.package_url"
                    target="_blank"
                    rel="noopener noreferrer"
                    download
                  >
                    <Download16 class="mr-1" aria-hidden="true" />
                    {{ t('portalMiniApps.package.downloaded') }}
                  </a>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </ConsolePanel>

    <ConsolePanel :title="t('portalMiniApps.manifest.title')" :note="selectedSlug || undefined">
      <p v-if="detailError" class="mb-3 border-l border-error pl-3 text-xs text-error">
        {{ detailError }}
      </p>

      <LoadingSkeleton v-if="selectedId && detailLoading" :rows="2" :columns="[3, 2]" header />

      <div v-else-if="!selected" class="py-4">
        <p class="voice text-[13px]">{{ t('portalMiniApps.empty') }}</p>
      </div>

      <div v-else-if="!manifest" class="py-4">
        <p class="voice text-[13px]">{{ t('portalMiniApps.package.empty') }}</p>
      </div>

      <template v-else>
        <div class="table-bleed overflow-x-auto">
          <table class="ledger ledger-stack">
            <thead>
              <tr>
                <th scope="col">{{ t('portalMiniApps.manifest.id') }}</th>
                <th scope="col">{{ t('portalMiniApps.manifest.name') }}</th>
                <th scope="col">{{ t('portalMiniApps.manifest.version') }}</th>
                <th scope="col">{{ t('portalMiniApps.manifest.entry') }}</th>
                <th scope="col">{{ t('portalMiniApps.manifest.author') }}</th>
                <th scope="col">{{ t('portalMiniApps.manifest.description') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr>
                <td
                  class="whitespace-nowrap font-mono text-xs text-base-content"
                  :data-label="t('portalMiniApps.manifest.id')"
                >
                  {{ manifest.id }}
                </td>
                <td class="text-xs text-dim" :data-label="t('portalMiniApps.manifest.name')">
                  {{ manifest.name }}
                </td>
                <td
                  class="whitespace-nowrap font-mono text-2xs text-dim"
                  :data-label="t('portalMiniApps.manifest.version')"
                >
                  {{ manifest.version }}
                </td>
                <td
                  class="whitespace-nowrap font-mono text-2xs text-dim"
                  :data-label="t('portalMiniApps.manifest.entry')"
                >
                  {{ manifest.entry }}
                </td>
                <td class="text-2xs text-dim" :data-label="t('portalMiniApps.manifest.author')">
                  {{ manifest.author || '—' }}
                </td>
                <td
                  class="text-2xs text-dim"
                  :data-label="t('portalMiniApps.manifest.description')"
                >
                  {{ manifest.description || '—' }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <p class="mt-2 text-2xs text-faint">{{ t('portalMiniApps.manifest.idHint') }}</p>
      </template>
    </ConsolePanel>

    <dialog
      v-if="creating"
      class="modal modal-open"
      aria-modal="true"
      :aria-label="t('portalMiniApps.createTitle')"
    >
      <div class="modal-box max-w-md rounded-[2px] border border-line bg-base-100 p-0 shadow-none">
        <form @submit.prevent="create">
          <div class="px-5 py-4">
            <h2 class="title text-base text-base-content">{{ t('portalMiniApps.createTitle') }}</h2>
            <p class="voice mt-2 text-[12px]">{{ t('portalMiniApps.createBody') }}</p>
            <p v-if="formError" class="mb-3 border-l border-error pl-3 text-xs text-error">
              {{ formError }}
            </p>

            <div class="mt-4 space-y-3">
              <div>
                <label class="field-label" for="miniapp-slug">
                  {{ t('portalMiniApps.fieldSlug') }}
                </label>
                <input
                  id="miniapp-slug"
                  v-model="draft.slug"
                  class="input input-sm"
                  required
                  autocomplete="off"
                  spellcheck="false"
                  placeholder="counter-widget"
                />
                <p class="mt-1 text-2xs text-faint">{{ t('portalMiniApps.fieldSlugHint') }}</p>
              </div>
              <div>
                <label class="field-label" for="miniapp-create-stage">
                  {{ t('portalMiniApps.fieldStage') }}
                </label>
                <select id="miniapp-create-stage" v-model="draft.stage" class="select select-sm">
                  <option v-for="stage in MINI_APP_STAGES" :key="stage" :value="stage">
                    {{ t(`portalMiniApps.stages.${stage}`) }}
                  </option>
                </select>
              </div>
            </div>
          </div>
          <footer class="flex items-center justify-end gap-2 border-t border-line px-5 py-3">
            <button type="button" class="btn btn-ghost btn-xs" :disabled="busy" @click="closeForm">
              {{ t('common.cancel') }}
            </button>
            <button
              type="submit"
              class="btn btn-primary btn-xs"
              :disabled="busy || !draft.slug.trim()"
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
        @click="closeForm"
      />
    </dialog>

    <dialog
      v-if="editing"
      class="modal modal-open"
      aria-modal="true"
      :aria-label="t('portalMiniApps.editTitle')"
    >
      <div class="modal-box max-w-md rounded-[2px] border border-line bg-base-100 p-0 shadow-none">
        <form @submit.prevent="save">
          <div class="px-5 py-4">
            <h2 class="title text-base text-base-content">{{ t('portalMiniApps.editTitle') }}</h2>
            <p class="mt-2 font-mono text-xs text-dim">{{ editing.slug }}</p>
            <p v-if="formError" class="mb-3 border-l border-error pl-3 text-xs text-error">
              {{ formError }}
            </p>

            <div class="mt-4 space-y-3">
              <div>
                <label class="field-label" for="miniapp-edit-stage">
                  {{ t('portalMiniApps.fieldStage') }}
                </label>
                <select id="miniapp-edit-stage" v-model="editDraft.stage" class="select select-sm">
                  <option v-for="stage in MINI_APP_STAGES" :key="stage" :value="stage">
                    {{ t(`portalMiniApps.stages.${stage}`) }}
                  </option>
                </select>
              </div>
            </div>
          </div>
          <footer class="flex items-center justify-end gap-2 border-t border-line px-5 py-3">
            <button type="button" class="btn btn-ghost btn-xs" :disabled="busy" @click="closeForm">
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

    <dialog
      v-if="uploadOpen"
      class="modal modal-open"
      aria-modal="true"
      :aria-label="t('portalMiniApps.package.uploadTitle')"
    >
      <div class="modal-box max-w-md rounded-[2px] border border-line bg-base-100 p-0 shadow-none">
        <form @submit.prevent="upload">
          <div class="px-5 py-4">
            <h2 class="title text-base text-base-content">
              {{ t('portalMiniApps.package.uploadTitle') }}
            </h2>

            <template v-if="!uploadResult">
              <p class="voice mt-2 text-[12px]">{{ t('portalMiniApps.package.uploadBody') }}</p>
              <p v-if="uploadError" class="mb-3 border-l border-error pl-3 text-xs text-error">
                {{ uploadError }}
              </p>

              <div class="mt-4">
                <label class="field-label" for="miniapp-package">
                  {{ t('portalMiniApps.package.file') }}
                </label>
                <input
                  id="miniapp-package"
                  type="file"
                  class="file-input file-input-sm w-full"
                  accept=".zip"
                  @change="onFile"
                />
                <p class="mt-1 text-2xs text-faint">{{ t('portalMiniApps.package.fileHint') }}</p>
              </div>
            </template>

            <template v-else>
              <p class="voice mt-2 text-[12px]">{{ t('portalMiniApps.package.uploaded') }}</p>
              <dl class="mt-3 space-y-2">
                <div class="flex items-baseline justify-between gap-3 border-b border-line-soft pb-2">
                  <dt class="text-2xs uppercase tracking-[0.16em] text-faint">
                    {{ t('portalMiniApps.package.sha256') }}
                  </dt>
                  <dd class="break-all font-mono text-2xs text-dim">{{ uploadResult.sha256 }}</dd>
                </div>
                <div class="flex items-baseline justify-between gap-3">
                  <dt class="text-2xs uppercase tracking-[0.16em] text-faint">
                    {{ t('portalMiniApps.package.size') }}
                  </dt>
                  <dd class="text-2xs text-dim">{{ bytes(uploadResult.size) }}</dd>
                </div>
              </dl>
            </template>
          </div>
          <footer class="flex items-center justify-end gap-2 border-t border-line px-5 py-3">
            <button type="button" class="btn btn-ghost btn-xs" :disabled="busy" @click="closeUpload">
              {{ uploadResult ? t('common.close') : t('common.cancel') }}
            </button>
            <button
              v-if="!uploadResult"
              type="submit"
              class="btn btn-primary btn-xs"
              :disabled="busy || !uploadFile"
            >
              {{ busy ? t('common.saving') : t('portalMiniApps.package.upload') }}
            </button>
          </footer>
        </form>
      </div>
      <button
        type="button"
        class="modal-backdrop bg-black/60"
        :aria-label="t('common.cancel')"
        @click="closeUpload"
      />
    </dialog>

    <ConfirmAction
      v-if="pendingDelete"
      :title="t('portalMiniApps.deleteTitle')"
      :body="t('portalMiniApps.deleteBody')"
      :action="t('portalMiniApps.deleteAction')"
      tone="error"
      :busy="busy"
      @confirm="remove"
      @cancel="pendingDelete = null"
    />
  </template>
</template>
