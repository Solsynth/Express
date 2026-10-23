<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Launch16, Renew16, TrashCan16 } from '@carbon/icons-vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'

import { catalogApi, productApi } from '@/lib/api'
import type { ProductWrite } from '@/lib/api'
import { ExpressError } from '@/lib/api/client'
import type { Product, ProductDetail } from '@/lib/api/types'
import { count, localized, relative, target } from '@/lib/format'
import { withViewTransition } from '@/lib/viewTransition'
import { useModulesStore, MODULES } from '@/stores/modules'
import { useNoticeStore } from '@/stores/notice'
import { navigate } from '@/lib/viewTransition'
import { useSessionStore } from '@/stores/session'
import PageHeader from '@/components/PageHeader.vue'
import ConsolePanel from '@/components/ConsolePanel.vue'
import LoadingSkeleton from '@/components/LoadingSkeleton.vue'
import IconButton from '@/components/IconButton.vue'
import ConfirmAction from '@/components/ConfirmAction.vue'
import AppMark from '@/components/AppMark.vue'
import ArtworkPanel from '@/components/ArtworkPanel.vue'
import SessionGate from '@/components/SessionGate.vue'
import TabBox from '@/components/TabBox.vue'
import type { Tab } from '@/components/tab'
import ReleasesPanel from '@/components/ReleasesPanel.vue'
import ChannelsPanel from '@/components/ChannelsPanel.vue'
import KeysPanel from '@/components/KeysPanel.vue'
import MetricsPanel from '@/components/MetricsPanel.vue'
import SyncPanel from '@/components/SyncPanel.vue'

const { t, locale } = useI18n()
const route = useRoute()
const router = useRouter()
const session = useSessionStore()
const modules = useModulesStore()
const notice = useNoticeStore()

const detail = ref<ProductDetail | null>(null)
const loading = ref(true)
const error = ref('')
const forbidden = ref(false)
const missing = ref(false)
const tab = ref('overview')
/** True only while a tab switch is animating; names the panel region. */
const panelTransition = ref(false)

const editing = ref(false)
const saving = ref(false)
const removing = ref(false)
const busy = ref(false)
const draft = ref({ slug: '', name: '', description: '' })

const productId = computed(() => String(route.params.productId ?? ''))

const tabs = computed<Tab[]>(() => {
  const list: Tab[] = [
    { key: 'overview', label: t('console.overview') },
    { key: 'releases', label: t('console.releases') },
    { key: 'channels', label: t('console.channels') },
    { key: 'keys', label: t('console.keys') },
    { key: 'metrics', label: t('console.metrics') },
  ]
  if (modules.has(MODULES.sync)) list.push({ key: 'sync', label: t('console.sync') })
  return list
})

const productName = computed(() =>
  detail.value ? localized(detail.value.product.names, detail.value.product.name, locale.value) : '',
)
const description = computed(() =>
  detail.value
    ? localized(detail.value.product.descriptions, detail.value.product.description, locale.value)
    : '',
)
const publisher = computed(() => detail.value?.publisher ?? null)
const publisherName = computed(() => publisher.value?.nick || publisher.value?.name || '—')

/** The membership that authorizes this product's console, when there is one. */
const membership = computed(() =>
  publisher.value ? session.publishers.find((item) => item.id === publisher.value?.id) : undefined,
)

const latest = computed(() => detail.value?.latest ?? null)

async function load() {
  loading.value = true
  error.value = ''
  forbidden.value = false
  missing.value = false
  try {
    detail.value = await catalogApi.product(productId.value)
  } catch (caught) {
    detail.value = null
    if (caught instanceof ExpressError && caught.isNotFound) missing.value = true
    else if (caught instanceof ExpressError && caught.isForbidden) forbidden.value = true
    else error.value = caught instanceof ExpressError ? caught.message : t('console.error')
  } finally {
    loading.value = false
  }
}

/**
 * A tab switch swaps one panel for another inside the same page. The panel is
 * named for the duration of the switch only, so the browser cross-fades that
 * region instead of the whole route — and an ordinary navigation never has to
 * share a unique name with it.
 */
function selectTab(next: string) {
  if (next === tab.value) return
  panelTransition.value = true
  void withViewTransition(() => {
    tab.value = next
  }).finally(() => {
    panelTransition.value = false
  })
}

function openEdit() {
  const product = detail.value?.product
  if (!product) return
  draft.value = {
    slug: product.slug,
    name: product.name,
    description: product.description ?? '',
  }
  editing.value = true
}

async function save() {
  const product = detail.value?.product
  if (!product) return
  saving.value = true
  error.value = ''
  const body: ProductWrite = {
    slug: draft.value.slug.trim(),
    name: draft.value.name.trim() || draft.value.slug.trim(),
    description: draft.value.description.trim(),
    // Art is managed outside the console today; a full-body update must send
    // the stored references back so an edit does not clear them.
    names: product.names,
    descriptions: product.descriptions,
    icon: product.icon,
    background: product.background,
    previews: product.previews,
  }
  try {
    await productApi.update(product.id, body)
    editing.value = false
    notice.ok(t('releases.updated', { version: product.slug }))
    await load()
  } catch (caught) {
    error.value = caught instanceof ExpressError ? caught.message : t('console.error')
  } finally {
    saving.value = false
  }
}

/**
 * The artwork panel saves one slot at a time and hands back the product the
 * write endpoint stored. Replacing the product in place keeps the panel's own
 * previews in sync without re-fetching the release list around it.
 */
function applyProduct(product: Product) {
  if (!detail.value) return
  detail.value = { ...detail.value, product }
}

async function remove() {
  const product = detail.value?.product
  if (!product) return
  busy.value = true
  try {
    await productApi.remove(product.id)
    notice.ok(t('console.deleted'))
    removing.value = false
    await navigate(router, '/workspace')
  } catch (caught) {
    notice.fail(caught instanceof ExpressError ? caught.message : t('console.error'))
  } finally {
    busy.value = false
  }
}

watch(productId, () => {
  tab.value = 'overview'
  void load()
})

onMounted(() => {
  void load()
  void modules.load()
})
</script>

<template>
  <div>
    <SessionGate :empty="session.signedIn && !session.publishers.length ? 'publishers' : null">
      <p v-if="error" class="mb-4 border-l border-error pl-3 text-xs text-error">{{ error }}</p>

      <div v-if="missing" class="py-10">
        <p class="voice">{{ t('console.notFound') }}</p>
        <RouterLink to="/workspace" class="btn btn-ghost btn-sm mt-4">
          {{ t('app.nav.workspace') }}
        </RouterLink>
      </div>

      <div v-else-if="forbidden || (detail && !membership)" class="py-10">
        <p class="voice">{{ t('console.forbidden') }}</p>
        <RouterLink :to="`/marketplace/${productId}`" class="btn btn-ghost btn-sm mt-4">
          {{ t('workspace.publicPage') }}
        </RouterLink>
      </div>

      <LoadingSkeleton v-else-if="loading" :rows="5" variant="rows" />

      <template v-else-if="detail">
        <PageHeader :title="productName" :description="description || t('product.noDescription')">
          <template #actions>
            <RouterLink :to="`/marketplace/${detail.product.id}`" class="btn btn-ghost btn-xs">
              {{ t('workspace.publicPage') }}
              <Launch16 class="ml-1" aria-hidden="true" />
            </RouterLink>
            <button
              v-if="session.can('app.products.update')"
              type="button"
              class="btn btn-ghost btn-xs"
              @click="openEdit"
            >
              {{ t('common.edit') }}
            </button>
            <IconButton :label="t('common.refresh')" :disabled="loading" @click="load">
              <Renew16 aria-hidden="true" />
            </IconButton>
          </template>
        </PageHeader>

        <!-- Identity, then the destructive action on its own line: at 390 px
             the two would share a row and squeeze the publisher name into a
             three-word column. -->
        <div class="mb-6 flex flex-wrap items-start gap-3">
          <AppMark :picture="detail.product.icon" :name="productName" flat />
          <div class="min-w-0 flex-1">
            <p class="text-2xs text-faint">
              {{ t('console.publisher') }}: {{ publisherName }}
            </p>
            <p class="text-2xs text-faint">
              <span class="font-mono">{{ detail.product.slug }}</span>
              <span class="mx-1 text-line">·</span>
              {{ t('common.updated') }} {{ relative(detail.product.updated_at) }}
            </p>
          </div>
        </div>
        <div
          v-if="session.can('app.products.delete')"
          class="mb-2 flex justify-end"
        >
          <button
            type="button"
            class="btn btn-ghost btn-xs text-faint hover:text-error"
            @click="removing = true"
          >
            <TrashCan16 class="mr-1" aria-hidden="true" />
            {{ t('console.deleteAction') }}
          </button>
        </div>

        <TabBox
          :tabs="tabs"
          :active="tab"
          :label="t('app.console')"
          @select="selectTab"
        />

        <div
          v-if="tab === 'overview'"
          class="space-y-6"
          :class="panelTransition ? 'console-panel' : ''"
        >
          <div class="grid grid-cols-3 gap-2 sm:gap-4">
            <ConsolePanel :title="t('console.latestStable')">
              <p class="font-mono text-lg text-base-content">
                {{ latest?.version ?? '—' }}
              </p>
              <p class="mt-1 text-2xs text-faint">
                {{ latest ? relative(latest.published_at) : t('workspace.noRelease') }}
              </p>
            </ConsolePanel>
            <ConsolePanel :title="t('console.totalDownloads')">
              <p class="font-mono text-lg tabular-nums text-base-content">
                {{ count(latest?.download_count) }}
              </p>
              <p class="mt-1 text-2xs text-faint">{{ latest?.version ?? '—' }}</p>
            </ConsolePanel>
            <ConsolePanel :title="t('product.targets', { n: latest?.artifacts.length ?? 0 })">
              <p class="font-mono text-lg tabular-nums text-base-content">
                {{ latest?.artifacts.length ?? 0 }}
              </p>
              <p class="mt-1 text-2xs text-faint">
                {{ latest?.channels.join(', ') || '—' }}
              </p>
            </ConsolePanel>
          </div>

          <!-- The overview is two columns while there is width to fill: the
               latest release on the left, the guidance that explains what to
               do next on the right. Below `lg` the guidance leads, because a
               wide screen already shows the release in its own panel. -->
          <div class="grid gap-4 lg:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
            <ConsolePanel :title="t('console.latestRelease')" :note="latest?.version">
              <template v-if="latest">
                <p class="text-[13px] text-base-content">
                  {{ localized(latest.titles, latest.title || latest.version, $i18n.locale) }}
                </p>
                <p class="mt-1 text-2xs text-faint">
                  {{ latest.channels.join(', ') }}
                  <span class="mx-1 text-line">·</span>
                  {{ relative(latest.published_at) }}
                </p>
                <ul class="mt-3 flex flex-wrap gap-1.5">
                  <li
                    v-for="artifact in latest.artifacts"
                    :key="artifact.id"
                    class="chip"
                    :class="artifact.expired ? 'text-faint line-through' : ''"
                    :title="artifact.file_name"
                  >
                    {{ target(artifact.platform, artifact.architecture) }}
                  </li>
                </ul>
                <RouterLink
                  :to="`/workspace/${detail.product.id}/releases/${latest.id}`"
                  class="btn btn-ghost btn-xs mt-3"
                >
                  {{ t('releases.detail', { version: latest.version }) }}
                </RouterLink>
              </template>
              <p v-else class="voice text-[12px]">{{ t('workspace.noRelease') }}</p>
            </ConsolePanel>

            <ArtworkPanel
              :product="detail.product"
              :can-manage="session.can('app.products.update')"
              class="lg:col-span-2"
              @updated="applyProduct"
            />

            <ConsolePanel :title="t('console.quickStart')">
              <p class="voice">{{ t('console.quickStartBody') }}</p>
              <button type="button" class="btn btn-outline btn-xs mt-3" @click="tab = 'releases'">
                {{ t('console.releases') }}
              </button>
            </ConsolePanel>
          </div>
        </div>

        <ReleasesPanel
          v-else-if="tab === 'releases'"
          :class="panelTransition ? 'console-panel' : ''"
          :product-id="detail.product.id"
          :can-manage="session.can('distribution.releases.manage')"
          :can-publish="session.can('distribution.releases.publish')"
        />

        <ChannelsPanel
          :class="panelTransition ? 'console-panel' : ''"
          v-else-if="tab === 'channels'"
          :product-id="detail.product.id"
          :can-manage="session.can('distribution.channels.manage')"
        />

        <KeysPanel
          :class="panelTransition ? 'console-panel' : ''"
          v-else-if="tab === 'keys'"
          :product-id="detail.product.id"
          :can-manage="session.can('distribution.upload_keys.manage')"
        />

        <MetricsPanel
          :class="panelTransition ? 'console-panel' : ''"
          v-else-if="tab === 'metrics'"
          :product-id="detail.product.id"
          :can-read="session.can('distribution.metrics.read')"
        />

        <SyncPanel
          :class="panelTransition ? 'console-panel' : ''"
          v-else-if="tab === 'sync'"
          :product-id="detail.product.id"
          :publisher="publisher"
          :can-manage="session.can('distribution.sync.manage')"
        />
      </template>
    </SessionGate>

    <dialog v-if="editing" class="modal modal-open" aria-modal="true" :aria-label="t('console.editTitle')">
      <div class="modal-box max-w-md rounded-[2px] border border-line bg-base-100 p-0 shadow-none">
        <form @submit.prevent="save">
          <div class="px-5 py-4">
            <h2 class="title text-base text-base-content">{{ t('console.editTitle') }}</h2>
            <div class="mt-4 space-y-3">
              <div>
                <label class="field-label" for="edit-slug">{{ t('console.slug') }}</label>
                <input id="edit-slug" v-model="draft.slug" class="input input-sm" required />
              </div>
              <div>
                <label class="field-label" for="edit-name">{{ t('workspace.fieldName') }}</label>
                <input id="edit-name" v-model="draft.name" class="input input-sm" />
              </div>
              <div>
                <label class="field-label" for="edit-description">
                  {{ t('workspace.fieldDescription') }}
                </label>
                <textarea
                  id="edit-description"
                  v-model="draft.description"
                  class="textarea textarea-sm"
                  rows="3"
                />
              </div>
            </div>
          </div>
          <footer class="flex items-center justify-end gap-2 border-t border-line px-5 py-3">
            <button type="button" class="btn btn-ghost btn-xs" @click="editing = false">
              {{ t('common.cancel') }}
            </button>
            <button type="submit" class="btn btn-primary btn-xs" :disabled="saving">
              {{ saving ? t('common.saving') : t('common.saveChanges') }}
            </button>
          </footer>
        </form>
      </div>
      <button
        type="button"
        class="modal-backdrop bg-black/60"
        :aria-label="t('common.cancel')"
        @click="editing = false"
      />
    </dialog>

    <ConfirmAction
      v-if="removing"
      :title="t('console.deleteTitle')"
      :body="t('console.deleteBody')"
      :action="t('console.deleteAction')"
      tone="error"
      :busy="busy"
      @confirm="remove"
      @cancel="removing = false"
    />
  </div>
</template>
