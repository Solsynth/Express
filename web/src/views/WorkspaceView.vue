<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Add16, ChevronRight16, Launch16, Renew16 } from '@carbon/icons-vue'
import { RouterLink, useRouter } from 'vue-router'

import { productApi } from '@/lib/api'
import type { ProductWrite } from '@/lib/api'
import { ExpressError } from '@/lib/api/client'
import type { ConsolePublisher } from '@/lib/api/types'
import { localized, relative } from '@/lib/format'
import { useNoticeStore } from '@/stores/notice'
import { useSessionStore } from '@/stores/session'
import { useWorkspaceStore } from '@/stores/workspace'
import { navigate } from '@/lib/viewTransition'
import PageHeader from '@/components/PageHeader.vue'
import ConsolePanel from '@/components/ConsolePanel.vue'
import LoadingSkeleton from '@/components/LoadingSkeleton.vue'
import IconButton from '@/components/IconButton.vue'
import AppMark from '@/components/AppMark.vue'
import SessionGate from '@/components/SessionGate.vue'

const { t, locale } = useI18n()
const session = useSessionStore()
const workspace = useWorkspaceStore()
const notice = useNoticeStore()
const router = useRouter()

const creating = ref(false)
const creatingFor = ref<ConsolePublisher | null>(null)
const busy = ref(false)
const error = ref('')
const draft = ref({ slug: '', name: '', description: '' })

const loading = computed(() => workspace.loading && !workspace.loaded)
const groups = computed(() => workspace.grouped)
const canCreate = computed(() => session.can('app.products.create'))

function productName(product: { names?: Record<string, string>; name: string }): string {
  return localized(product.names, product.name, locale.value)
}

function openCreate(publisher: ConsolePublisher) {
  creatingFor.value = publisher
  draft.value = { slug: '', name: '', description: '' }
  creating.value = true
}

function closeCreate() {
  creating.value = false
  creatingFor.value = null
}

async function create() {
  const publisher = creatingFor.value
  const slug = draft.value.slug.trim()
  if (!publisher || !slug) return
  busy.value = true
  error.value = ''
  const body: ProductWrite = {
    slug,
    name: draft.value.name.trim() || slug,
    description: draft.value.description.trim(),
  }
  try {
    const created = await productApi.create(publisher.name, body)
    notice.ok(t('workspace.created', { name: created.name }))
    closeCreate()
    await workspace.load(true)
    await navigate(router, `/workspace/${created.id}`)
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : t('workspace.error')
  } finally {
    busy.value = false
  }
}

onMounted(() => void workspace.load())
</script>

<template>
  <div>
    <PageHeader :title="t('workspace.title')" :description="t('workspace.description')">
      <template #actions>
        <IconButton
          :label="t('common.refresh')"
          :disabled="loading || !session.signedIn"
          @click="workspace.load(true)"
        >
          <Renew16 aria-hidden="true" />
        </IconButton>
      </template>
    </PageHeader>

    <SessionGate :empty="session.signedIn && !session.publishers.length ? 'publishers' : null">
      <p v-if="workspace.error" class="mb-4 border-l border-error pl-3 text-xs text-error">
        {{ workspace.error }}
      </p>
      <p v-if="error" class="mb-4 border-l border-error pl-3 text-xs text-error">{{ error }}</p>

      <LoadingSkeleton v-if="loading" :rows="4" variant="rows" />

      <div v-else class="grid grid-cols-[repeat(auto-fit,minmax(min(100%,440px),1fr))] gap-4">
        <ConsolePanel
          v-for="group in groups"
          :key="group.publisher.id"
          :title="group.publisher.nick || group.publisher.name"
          :note="t('workspace.productCount', { n: group.products.length })"
        >
          <template #header>
            <span class="chip ml-auto">{{ group.publisher.role }}</span>
          </template>

          <p v-if="!group.products.length" class="voice py-4 text-[12px]">
            {{ t('workspace.empty') }}
          </p>

          <ul v-else class="-my-1">
            <li
              v-for="product in group.products"
              :key="product.id"
              class="flex flex-wrap items-center gap-x-3 gap-y-2 border-b border-line-soft py-3 last:border-0"
            >
              <AppMark :picture="product.icon" :name="productName(product)" flat />
              <RouterLink :to="`/workspace/${product.id}`" view-transition class="min-w-0 flex-1">
                <p class="truncate text-[13px] text-base-content">{{ productName(product) }}</p>
                <p class="truncate text-2xs text-faint">
                  <span class="font-mono">{{ product.slug }}</span>
                  <span class="mx-1 text-line">·</span>
                  {{ relative(product.updated_at) }}
                </p>
              </RouterLink>
              <RouterLink :to="`/marketplace/${product.id}`" class="link-quiet text-2xs">
                {{ t('workspace.publicPage') }}
                <Launch16 aria-hidden="true" />
              </RouterLink>
              <RouterLink
                :to="`/workspace/${product.id}`"
                view-transition
                class="btn btn-ghost btn-xs"
                :aria-label="t('workspace.openConsole')"
              >
                {{ t('workspace.openConsole') }}
                <ChevronRight16 class="ml-1" aria-hidden="true" />
              </RouterLink>
            </li>
          </ul>

          <template #footer>
            <div class="flex items-center justify-between">
              <span class="text-2xs text-faint">{{ group.publisher.name }}</span>
              <button
                v-if="canCreate"
                type="button"
                class="btn btn-ghost btn-xs"
                @click="openCreate(group.publisher)"
              >
                <Add16 class="mr-1" aria-hidden="true" />
                {{ t('workspace.create') }}
              </button>
              <span v-else class="text-2xs text-faint">{{ t('workspace.noPermission') }}</span>
            </div>
          </template>
        </ConsolePanel>

        <p v-if="!groups.length" class="voice py-6">{{ t('workspace.empty') }}</p>
      </div>
    </SessionGate>

    <dialog v-if="creating" class="modal modal-open" aria-modal="true" :aria-label="t('workspace.createTitle')">
      <div class="modal-box max-w-md rounded-[2px] border border-line bg-base-100 p-0 shadow-none">
        <form @submit.prevent="create">
          <div class="px-5 py-4">
            <h2 class="title text-base text-base-content">{{ t('workspace.createTitle') }}</h2>
            <p class="voice mt-2 text-[12px]">{{ t('workspace.createBody') }}</p>

            <div class="mt-4 space-y-3">
              <div>
                <label class="field-label" for="product-slug">{{ t('workspace.fieldSlug') }}</label>
                <input
                  id="product-slug"
                  v-model="draft.slug"
                  class="input input-sm"
                  required
                  autocomplete="off"
                  spellcheck="false"
                  placeholder="desktop-client"
                />
                <p class="mt-1 text-2xs text-faint">{{ t('workspace.fieldSlugHint') }}</p>
              </div>
              <div>
                <label class="field-label" for="product-name">{{ t('workspace.fieldName') }}</label>
                <input
                  id="product-name"
                  v-model="draft.name"
                  class="input input-sm"
                  autocomplete="off"
                  placeholder="Desktop Client"
                />
                <p class="mt-1 text-2xs text-faint">{{ t('workspace.fieldNameHint') }}</p>
              </div>
              <div>
                <label class="field-label" for="product-description">
                  {{ t('workspace.fieldDescription') }}
                </label>
                <textarea
                  id="product-description"
                  v-model="draft.description"
                  class="textarea textarea-sm"
                  rows="3"
                />
              </div>
            </div>
          </div>
          <footer class="flex items-center justify-end gap-2 border-t border-line px-5 py-3">
            <button type="button" class="btn btn-ghost btn-xs" :disabled="busy" @click="closeCreate">
              {{ t('common.cancel') }}
            </button>
            <button type="submit" class="btn btn-primary btn-xs" :disabled="busy || !draft.slug.trim()">
              {{ busy ? t('common.saving') : t('common.create') }}
            </button>
          </footer>
        </form>
      </div>
      <button
        type="button"
        class="modal-backdrop bg-black/60"
        :aria-label="t('common.cancel')"
        @click="closeCreate"
      />
    </dialog>
  </div>
</template>
