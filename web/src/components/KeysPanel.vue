<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Add16, Renew16 } from '@carbon/icons-vue'

import { uploadKeyApi } from '@/lib/api'
import { ExpressError } from '@/lib/api/client'
import type { CreatedUploadKey, UploadKey } from '@/lib/api/types'
import { relative } from '@/lib/format'
import { useNoticeStore } from '@/stores/notice'
import ConsolePanel from '@/components/ConsolePanel.vue'
import LoadingSkeleton from '@/components/LoadingSkeleton.vue'
import IconButton from '@/components/IconButton.vue'
import ConfirmAction from '@/components/ConfirmAction.vue'
import CopyButton from '@/components/CopyButton.vue'

const props = defineProps<{
  productId: string
  canManage: boolean
}>()

const { t } = useI18n()
const notice = useNoticeStore()

const keys = ref<UploadKey[]>([])
const loading = ref(true)
const error = ref('')
const busy = ref(false)

const creating = ref(false)
const name = ref('')
const issued = ref<CreatedUploadKey | null>(null)
const pendingRevoke = ref<UploadKey | null>(null)

async function load() {
  loading.value = true
  error.value = ''
  try {
    const result = await uploadKeyApi.list(props.productId)
    keys.value = result.data ?? []
  } catch (caught) {
    error.value = caught instanceof ExpressError ? caught.message : t('console.error')
  } finally {
    loading.value = false
  }
}

function openCreate() {
  name.value = ''
  creating.value = true
}

async function create() {
  const label = name.value.trim()
  if (!label) return
  busy.value = true
  error.value = ''
  try {
    // The plaintext key arrives exactly once, so it is held in the page state
    // until the reader copies it; the list below is metadata only.
    issued.value = await uploadKeyApi.create(props.productId, label)
    notice.ok(t('keys.created', { name: label }))
    creating.value = false
    await load()
  } catch (caught) {
    error.value = caught instanceof ExpressError ? caught.message : t('console.error')
  } finally {
    busy.value = false
  }
}

async function revoke() {
  const key = pendingRevoke.value
  if (!key) return
  busy.value = true
  try {
    await uploadKeyApi.revoke(props.productId, key.id)
    notice.ok(t('keys.revoked', { name: key.name }))
    pendingRevoke.value = null
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
  <div class="space-y-6">
    <!-- The plaintext key is the only thing on this page that cannot be
         recovered, so it sits above the list in a warning-toned block. -->
    <section
      v-if="issued"
      class="border border-warning/50 bg-warning/5 px-4 py-3"
      role="status"
    >
      <p class="text-2xs uppercase tracking-[0.14em] text-warning">{{ t('keys.issued') }}</p>
      <div class="mt-2 flex flex-wrap items-center gap-2">
        <code class="min-w-0 flex-1 truncate font-mono text-xs text-base-content">
          {{ issued.key }}
        </code>
        <CopyButton :value="issued.key" :label="t('keys.title')" />
        <button type="button" class="btn btn-ghost btn-xs" @click="issued = null">
          {{ t('common.dismiss') }}
        </button>
      </div>
    </section>

    <ConsolePanel :title="t('keys.title')" :note="loading ? undefined : String(keys.length)">
      <template #header>
        <div class="flex flex-wrap items-center gap-1 sm:gap-2">
          <IconButton :label="t('common.refresh')" :disabled="loading" @click="load">
            <Renew16 aria-hidden="true" />
          </IconButton>
          <button v-if="canManage" type="button" class="btn btn-ghost btn-xs" @click="openCreate">
            <Add16 class="mr-1" aria-hidden="true" />
            {{ t('keys.create') }}
          </button>
        </div>
      </template>

      <p class="voice mb-3 text-[12px]">{{ t('keys.description') }}</p>
      <p v-if="error" class="mb-3 border-l border-error pl-3 text-xs text-error">{{ error }}</p>

      <LoadingSkeleton v-if="loading" :rows="3" :columns="[3, 2, 2, 2]" header />

      <div v-else-if="!keys.length" class="py-6">
        <p class="voice text-[13px]">{{ t('keys.empty') }}</p>
      </div>

      <div v-else class="table-bleed overflow-x-auto">
        <table class="ledger ledger-stack">
          <thead>
            <tr>
              <th scope="col">{{ t('keys.columns.name') }}</th>
              <th scope="col">{{ t('keys.columns.prefix') }}</th>
              <th scope="col">{{ t('keys.columns.created') }}</th>
              <th scope="col">{{ t('keys.columns.lastUsed') }}</th>
              <th scope="col"><span class="sr-only">{{ t('common.actions') }}</span></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="key in keys" :key="key.id">
              <td class="text-xs text-base-content" :data-label="t('keys.columns.name')">
                {{ key.name }}
                <span v-if="key.revoked_at" class="chip ml-2 text-faint">
                  {{ t('keys.revokedState') }}
                </span>
              </td>
              <td class="whitespace-nowrap font-mono text-2xs text-dim" :data-label="t('keys.columns.prefix')">{{ key.key_prefix }}…</td>
              <td class="whitespace-nowrap text-2xs text-faint" :data-label="t('keys.columns.created')">{{ relative(key.created_at) }}</td>
              <td class="whitespace-nowrap text-2xs text-faint" :data-label="t('keys.columns.lastUsed')">
                {{ key.last_used_at ? relative(key.last_used_at) : t('common.never') }}
              </td>
              <td class="whitespace-nowrap">
                <div class="flex justify-end">
                  <button
                    v-if="canManage && !key.revoked_at"
                    type="button"
                    class="btn btn-ghost btn-xs text-dim hover:text-error"
                    @click="pendingRevoke = key"
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

    <dialog v-if="creating" class="modal modal-open" aria-modal="true" :aria-label="t('keys.createTitle')">
      <div class="modal-box max-w-md rounded-[2px] border border-line bg-base-100 p-0 shadow-none">
        <form @submit.prevent="create">
          <div class="px-5 py-4">
            <h2 class="title text-base text-base-content">{{ t('keys.createTitle') }}</h2>
            <p class="voice mt-2 text-[12px]">{{ t('keys.createBody') }}</p>
            <div class="mt-4">
              <label class="field-label" for="key-name">{{ t('keys.fieldName') }}</label>
              <input
                id="key-name"
                v-model="name"
                class="input input-sm"
                required
                autocomplete="off"
                placeholder="GitHub Actions"
              />
              <p class="mt-1 text-2xs text-faint">{{ t('keys.fieldNameHint') }}</p>
            </div>
          </div>
          <footer class="flex items-center justify-end gap-2 border-t border-line px-5 py-3">
            <button type="button" class="btn btn-ghost btn-xs" @click="creating = false">
              {{ t('common.cancel') }}
            </button>
            <button type="submit" class="btn btn-primary btn-xs" :disabled="busy || !name.trim()">
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
      v-if="pendingRevoke"
      :title="t('keys.revokeTitle', { name: pendingRevoke.name })"
      :body="t('keys.revokeBody')"
      :action="t('keys.revokeAction')"
      tone="error"
      :busy="busy"
      @confirm="revoke"
      @cancel="pendingRevoke = null"
    />
  </div>
</template>
