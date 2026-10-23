<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ArrowLeft16, CloudUpload16, Launch16, Renew16, TrashCan16 } from '@carbon/icons-vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'

import { artifactApi, channelApi, releaseApi } from '@/lib/api'
import { ExpressError } from '@/lib/api/client'
import type { ManagedRelease } from '@/lib/api/types'
import { bytes, count, localized, relative, target, utcStamp } from '@/lib/format'
import { useNoticeStore } from '@/stores/notice'
import { useSessionStore } from '@/stores/session'
import PageHeader from '@/components/PageHeader.vue'
import ConsolePanel from '@/components/ConsolePanel.vue'
import LoadingSkeleton from '@/components/LoadingSkeleton.vue'
import IconButton from '@/components/IconButton.vue'
import ConfirmAction from '@/components/ConfirmAction.vue'
import SessionGate from '@/components/SessionGate.vue'

const { t, locale } = useI18n()
const route = useRoute()
const router = useRouter()
const session = useSessionStore()
const notice = useNoticeStore()

const productId = computed(() => String(route.params.productId ?? ''))
const releaseId = computed(() => String(route.params.releaseId ?? ''))

const release = ref<ManagedRelease | null>(null)
const loading = ref(true)
const error = ref('')
const busy = ref(false)

const uploading = ref(false)
const upload = ref({ file: null as File | null, version: '', channel: 'stable', platform: '', architecture: '' })
const progress = ref(0)

const attachingExternal = ref(false)
const external = ref({ url: '', fileName: '', mimeType: '', hash: '', platform: '', architecture: '' })

const pendingDelete = ref<ManagedRelease | null>(null)

const canManage = computed(() => session.can('distribution.releases.manage'))
const canPublish = computed(() => session.can('distribution.releases.publish'))
const canUpload = computed(() => session.can('distribution.artifacts.upload'))

const title = computed(() =>
  release.value ? localized(release.value.titles, release.value.title, locale.value) : '',
)
const notes = computed(() =>
  release.value ? localized(release.value.descriptions, release.value.release_notes, locale.value) : '',
)

/**
 * The management listing is channel-scoped, so the detail view asks each
 * channel for its newest page and stops at the one holding this release. A
 * release belongs to one or more channels, and any one of them answers.
 */
async function load() {
  loading.value = true
  error.value = ''
  try {
    const listed = await channelApi.list(productId.value)
    for (const channel of listed.data ?? []) {
      const result = await releaseApi.manage(productId.value, {
        channel: channel.name,
        limit: 100,
        offset: 0,
      })
      const found = (result.data ?? []).find((item) => item.id === releaseId.value)
      if (found) {
        release.value = found
        upload.value.version = found.version
        upload.value.channel = found.channels[0] ?? channel.name
        return
      }
    }
    release.value = null
  } catch (caught) {
    error.value = caught instanceof ExpressError ? caught.message : t('console.error')
  } finally {
    loading.value = false
  }
}

async function sha256(file: File): Promise<string> {
  const digest = await crypto.subtle.digest('SHA-256', await file.arrayBuffer())
  return [...new Uint8Array(digest)].map((byte) => byte.toString(16).padStart(2, '0')).join('')
}

function onFile(event: Event) {
  const input = event.target as HTMLInputElement
  upload.value.file = input.files?.[0] ?? null
}

async function submitUpload() {
  const file = upload.value.file
  const version = upload.value.version.trim()
  if (!file || !version) return
  uploading.value = true
  progress.value = 0
  error.value = ''
  try {
    const digest = await sha256(file)
    const prepared = await artifactApi.prepare(productId.value, {
      version,
      channel: upload.value.channel.trim() || undefined,
      file_name: file.name,
      mime_type: file.type || 'application/octet-stream',
      sha256: digest,
    })
    await artifactApi.put(prepared.upload_url, file, file.type || 'application/octet-stream', digest)
    progress.value = 100
    await artifactApi.attach(productId.value, prepared.release_id ?? version, {
      object_key: prepared.object_key,
      platform: upload.value.platform.trim() || 'any',
      architecture: upload.value.architecture.trim() || 'any',
    })
    notice.ok(t('artifacts.attached', { name: file.name }))
    uploading.value = false
    upload.value.file = null
    await load()
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : t('console.error')
  } finally {
    uploading.value = false
  }
}

async function submitExternal() {
  const current = release.value
  if (!current) return
  const url = external.value.url.trim()
  if (!url) return
  busy.value = true
  error.value = ''
  try {
    await artifactApi.attach(productId.value, current.id, {
      download_url: url,
      file_name: external.value.fileName.trim() || undefined,
      mime_type: external.value.mimeType.trim() || undefined,
      hash: external.value.hash.trim() || undefined,
      platform: external.value.platform.trim() || 'any',
      architecture: external.value.architecture.trim() || 'any',
    })
    notice.ok(t('artifacts.attachedExternal'))
    attachingExternal.value = false
    external.value = { url: '', fileName: '', mimeType: '', hash: '', platform: '', architecture: '' }
    await load()
  } catch (caught) {
    error.value = caught instanceof ExpressError ? caught.message : t('console.error')
  } finally {
    busy.value = false
  }
}

async function publish() {
  const current = release.value
  if (!current) return
  busy.value = true
  try {
    await releaseApi.publish(productId.value, current.id)
    notice.ok(t('releases.published', { version: current.version }))
    await load()
  } catch (caught) {
    notice.fail(caught instanceof ExpressError ? caught.message : t('console.error'))
  } finally {
    busy.value = false
  }
}

async function remove() {
  const current = pendingDelete.value
  if (!current) return
  busy.value = true
  try {
    await releaseApi.remove(productId.value, current.id)
    notice.ok(t('releases.deleted', { version: current.version }))
    pendingDelete.value = null
    await router.push(`/workspace/${productId.value}`)
  } catch (caught) {
    notice.fail(caught instanceof ExpressError ? caught.message : t('console.error'))
  } finally {
    busy.value = false
  }
}

watch([productId, releaseId], () => void load())
onMounted(load)
</script>

<template>
  <div>
    <RouterLink
      :to="`/workspace/${productId}`"
      class="mb-5 inline-flex items-center gap-1.5 text-2xs text-faint transition-colors hover:text-dim"
    >
      <ArrowLeft16 aria-hidden="true" />
      {{ t('releases.backToReleases') }}
    </RouterLink>

    <SessionGate :empty="session.signedIn && !session.publishers.length ? 'publishers' : null">
      <p v-if="error" class="mb-4 border-l border-error pl-3 text-xs text-error">{{ error }}</p>

      <div v-if="!loading && !release" class="py-10">
        <p class="voice">{{ t('releases.empty') }}</p>
        <RouterLink :to="`/workspace/${productId}`" class="btn btn-ghost btn-sm mt-4">
          {{ t('releases.backToReleases') }}
        </RouterLink>
      </div>

      <LoadingSkeleton v-else-if="loading" :rows="4" variant="rows" />

      <template v-else-if="release">
        <PageHeader
          :title="release.version"
          :description="title || undefined"
        >
          <template #actions>
            <span class="chip">{{ t(`releases.status.${release.status}`) }}</span>
            <button
              v-if="canPublish && release.status === 'draft'"
              type="button"
              class="btn btn-primary btn-xs"
              :disabled="busy"
              @click="publish"
            >
              {{ t('releases.publishAction') }}
            </button>
            <button
              v-if="canManage && release.status === 'draft'"
              type="button"
              class="btn btn-ghost btn-xs text-dim hover:text-error"
              @click="pendingDelete = release"
            >
              <TrashCan16 class="mr-1" aria-hidden="true" />
              {{ t('common.delete') }}
            </button>
            <IconButton :label="t('common.refresh')" :disabled="loading" @click="load">
              <Renew16 aria-hidden="true" />
            </IconButton>
          </template>
        </PageHeader>

        <div class="mb-6 flex flex-wrap items-center gap-x-4 gap-y-1 text-2xs text-faint">
          <span>{{ t('releases.columns.channels') }}: {{ release.channels.join(', ') || '—' }}</span>
          <span>
            {{ t('releases.columns.published') }}:
            {{ release.published_at ? utcStamp(release.published_at) : '—' }}
          </span>
          <span>{{ t('common.downloads') }}: {{ count(release.download_count) }}</span>
          <span v-if="release.force_update" class="text-warning">{{ t('product.forceUpdate') }}</span>
        </div>

        <div class="space-y-6">
          <ConsolePanel :title="t('artifacts.title')" :note="String(release.artifacts.length)">
            <template #header>
              <div
                v-if="canUpload && release.status !== 'yanked'"
                class="ml-auto flex flex-wrap items-center gap-1 sm:gap-2"
              >
                <button type="button" class="btn btn-ghost btn-xs" @click="attachingExternal = true">
                  {{ t('artifacts.external') }}
                </button>
                <button type="button" class="btn btn-ghost btn-xs" @click="uploading = !uploading">
                  <CloudUpload16 class="mr-1" aria-hidden="true" />
                  {{ t('artifacts.upload') }}
                </button>
              </div>
            </template>

            <p class="voice mb-3 text-[12px]">{{ t('artifacts.description') }}</p>

            <div v-if="uploading" class="mb-4 border border-line bg-base-200/40 px-4 py-3">
              <h3 class="text-2xs uppercase tracking-[0.14em] text-dim">{{ t('artifacts.uploadTitle') }}</h3>
              <p class="voice mt-1 text-[12px]">{{ t('artifacts.uploadBody') }}</p>
              <div class="mt-3 grid gap-3 sm:grid-cols-2">
                <div class="sm:col-span-2">
                  <label class="field-label" for="artifact-file">{{ t('artifacts.fieldFile') }}</label>
                  <input
                    id="artifact-file"
                    type="file"
                    class="file-input file-input-sm w-full"
                    @change="onFile"
                  />
                  <p class="mt-1 text-2xs text-faint">{{ t('artifacts.fieldFileHint') }}</p>
                </div>
                <div>
                  <label class="field-label" for="artifact-version">{{ t('artifacts.fieldVersion') }}</label>
                  <input id="artifact-version" v-model="upload.version" class="input input-sm" />
                </div>
                <div>
                  <label class="field-label" for="artifact-channel">{{ t('artifacts.fieldChannel') }}</label>
                  <input id="artifact-channel" v-model="upload.channel" class="input input-sm" />
                </div>
                <div>
                  <label class="field-label" for="artifact-platform">{{ t('artifacts.fieldPlatform') }}</label>
                  <input
                    id="artifact-platform"
                    v-model="upload.platform"
                    class="input input-sm"
                    placeholder="macos"
                  />
                </div>
                <div>
                  <label class="field-label" for="artifact-arch">{{ t('artifacts.fieldArchitecture') }}</label>
                  <input
                    id="artifact-arch"
                    v-model="upload.architecture"
                    class="input input-sm"
                    placeholder="arm64"
                  />
                </div>
              </div>
              <div v-if="uploading && progress > 0" class="mt-3">
                <div class="h-[3px] w-full bg-base-300" aria-hidden="true">
                  <div class="h-full bg-primary" :style="{ width: `${progress}%` }" />
                </div>
              </div>
              <div class="mt-3 flex items-center justify-end gap-2">
                <button type="button" class="btn btn-ghost btn-xs" :disabled="uploading" @click="uploading = false">
                  {{ t('common.cancel') }}
                </button>
                <button
                  type="button"
                  class="btn btn-primary btn-xs"
                  :disabled="!upload.file || !upload.version.trim()"
                  @click="submitUpload"
                >
                  {{ t('artifacts.uploadAction') }}
                </button>
              </div>
            </div>

            <div v-if="!release.artifacts.length" class="py-4">
              <p class="voice text-[12px]">{{ t('artifacts.empty') }}</p>
            </div>

            <div v-else class="table-bleed overflow-x-auto">
              <table class="ledger ledger-stack">
                <thead>
                  <tr>
                    <th scope="col">{{ t('artifacts.columns.target') }}</th>
                    <th scope="col">{{ t('artifacts.columns.file') }}</th>
                    <th scope="col">{{ t('artifacts.columns.size') }}</th>
                    <th scope="col">{{ t('artifacts.columns.downloads') }}</th>
                    <th scope="col">{{ t('artifacts.columns.state') }}</th>
                    <th scope="col"><span class="sr-only">{{ t('common.actions') }}</span></th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="artifact in release.artifacts" :key="artifact.id">
                    <td class="whitespace-nowrap font-mono text-xs text-base-content" :data-label="t('artifacts.columns.target')">
                      {{ target(artifact.platform, artifact.architecture) }}
                    </td>
                    <td class="text-2xs text-dim sm:max-w-[240px]" :title="artifact.file_name" :data-label="t('artifacts.columns.file')">
                      <div class="cell-stack">
                        <span class="break-all sm:truncate">{{ artifact.file_name || '—' }}</span>
                        <span class="text-2xs text-faint">{{ artifact.mime_type }}</span>
                      </div>
                    </td>
                    <td class="whitespace-nowrap text-2xs tabular-nums text-dim" :data-label="t('artifacts.columns.size')">
                      {{ bytes(artifact.size) }}
                    </td>
                    <td class="whitespace-nowrap text-2xs tabular-nums text-dim" :data-label="t('artifacts.columns.downloads')">
                      {{ count(artifact.download_count) }}
                    </td>
                    <td class="whitespace-nowrap" :data-label="t('artifacts.columns.state')">
                      <span class="chip" :class="artifact.expired ? 'text-faint' : 'text-success border-success/50'">
                        {{ artifact.expired ? t('artifacts.expired') : t('artifacts.live') }}
                      </span>
                    </td>
                    <td class="whitespace-nowrap">
                      <div class="flex items-center justify-end gap-1">
                        <a
                          v-if="artifact.download_url"
                          :href="artifact.download_url"
                          class="btn btn-ghost btn-xs text-dim"
                          target="_blank"
                          rel="noreferrer"
                        >
                          <Launch16 aria-hidden="true" />
                        </a>
                      </div>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
          </ConsolePanel>

          <ConsolePanel :title="t('releases.notes')" :note="relative(release.created_at)">
            <p class="whitespace-pre-line font-sans text-[13px] leading-relaxed text-dim">
              {{ notes || t('releases.noNotes') }}
            </p>
            <p v-if="release.status === 'yanked'" class="mt-3 text-2xs text-warning">
              {{ t('releases.cannotEdit') }}
            </p>
          </ConsolePanel>

          <ConsolePanel v-if="release.metadata && Object.keys(release.metadata).length" :title="t('releases.metadata')">
            <dl class="grid gap-2 sm:grid-cols-2">
              <div v-for="(value, key) in release.metadata" :key="key" class="flex items-baseline gap-2">
                <dt class="font-mono text-2xs text-faint">{{ key }}</dt>
                <dd class="min-w-0 truncate font-mono text-2xs text-dim">{{ value }}</dd>
              </div>
            </dl>
          </ConsolePanel>
        </div>
      </template>
    </SessionGate>

    <dialog
      v-if="attachingExternal"
      class="modal modal-open"
      aria-modal="true"
      :aria-label="t('artifacts.externalTitle')"
    >
      <div class="modal-box max-w-md rounded-[2px] border border-line bg-base-100 p-0 shadow-none">
        <form @submit.prevent="submitExternal">
          <div class="px-5 py-4">
            <h2 class="title text-base text-base-content">{{ t('artifacts.externalTitle') }}</h2>
            <p class="voice mt-2 text-[12px]">{{ t('artifacts.externalBody') }}</p>
            <div class="mt-4 space-y-3">
              <div>
                <label class="field-label" for="external-url">{{ t('artifacts.fieldURL') }}</label>
                <input
                  id="external-url"
                  v-model="external.url"
                  class="input input-sm"
                  required
                  autocomplete="off"
                  spellcheck="false"
                  placeholder="https://downloads.example.com/client.tar.gz"
                />
              </div>
              <div class="grid gap-3 sm:grid-cols-2">
                <div>
                  <label class="field-label" for="external-name">{{ t('artifacts.fieldFileName') }}</label>
                  <input id="external-name" v-model="external.fileName" class="input input-sm" />
                </div>
                <div>
                  <label class="field-label" for="external-mime">{{ t('artifacts.fieldMimeType') }}</label>
                  <input id="external-mime" v-model="external.mimeType" class="input input-sm" />
                </div>
                <div>
                  <label class="field-label" for="external-platform">{{ t('artifacts.fieldPlatform') }}</label>
                  <input id="external-platform" v-model="external.platform" class="input input-sm" />
                </div>
                <div>
                  <label class="field-label" for="external-arch">{{ t('artifacts.fieldArchitecture') }}</label>
                  <input id="external-arch" v-model="external.architecture" class="input input-sm" />
                </div>
              </div>
              <div>
                <label class="field-label" for="external-hash">{{ t('artifacts.fieldHash') }}</label>
                <input id="external-hash" v-model="external.hash" class="input input-sm" autocomplete="off" />
              </div>
            </div>
          </div>
          <footer class="flex items-center justify-end gap-2 border-t border-line px-5 py-3">
            <button type="button" class="btn btn-ghost btn-xs" @click="attachingExternal = false">
              {{ t('common.cancel') }}
            </button>
            <button type="submit" class="btn btn-primary btn-xs" :disabled="busy || !external.url.trim()">
              {{ busy ? t('common.saving') : t('common.add') }}
            </button>
          </footer>
        </form>
      </div>
      <button
        type="button"
        class="modal-backdrop bg-black/60"
        :aria-label="t('common.cancel')"
        @click="attachingExternal = false"
      />
    </dialog>

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
  </div>
</template>
