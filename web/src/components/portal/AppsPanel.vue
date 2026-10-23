<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Add16, Renew16 } from '@carbon/icons-vue'

import { appProductApi, boardApi, customAppApi, secretApi } from '@/lib/api/portal'
import type { AppProductWrite, BoardWidgetWrite, CustomAppWrite, SecretWrite } from '@/lib/api/portal'
import { ExpressError } from '@/lib/api/client'
import type {
  AppProduct,
  AppStatusWord,
  BoardWidget,
  CustomApp,
  CustomAppSecret,
  DevProject,
  PortalScope,
} from '@/lib/api/types'
import { appStatus, expiryDuration, recurrence, stockMode } from '@/lib/portal'
import type { Recurrence, StockMode } from '@/lib/portal'
import { relative } from '@/lib/format'
import { useNoticeStore } from '@/stores/notice'
import { useSessionStore } from '@/stores/session'
import ConsolePanel from '@/components/ConsolePanel.vue'
import LoadingSkeleton from '@/components/LoadingSkeleton.vue'
import IconButton from '@/components/IconButton.vue'
import ConfirmAction from '@/components/ConfirmAction.vue'
import CopyButton from '@/components/CopyButton.vue'
import TabBox from '@/components/TabBox.vue'
import type { Tab } from '@/components/tab'

const props = defineProps<{
  scope: PortalScope | null
  project: DevProject | null
}>()

const { t } = useI18n()
const notice = useNoticeStore()
const session = useSessionStore()

// ── Permission nodes the server checks ──
const canCreateApp = computed(() => session.can('custom.apps.create'))
const canUpdateApp = computed(() => session.can('custom.apps.update'))
const canDeleteApp = computed(() => session.can('custom.apps.delete'))
const canManageSecrets = computed(() => session.can('custom.apps.secrets.manage'))
const canCreateProduct = computed(() => session.can('app.products.create'))
const canUpdateProduct = computed(() => session.can('app.products.update'))
const canDeleteProduct = computed(() => session.can('app.products.delete'))

// ── Custom apps ──
const apps = ref<CustomApp[]>([])
const loading = ref(true)
const error = ref('')
const busy = ref(false)

const creating = ref(false)
const editing = ref<CustomApp | null>(null)
const pendingDelete = ref<CustomApp | null>(null)
const appDraft = ref({
  slug: '',
  name: '',
  description: '',
  status: 'developing' as AppStatusWord,
})

// ── The app every dependent section acts on ──
const selectedAppId = ref('')
const selectedApp = computed(
  () => apps.value.find((app) => app.id === selectedAppId.value) ?? null,
)
const appTabs = computed<Tab[]>(() => apps.value.map((app) => ({ key: app.id, label: app.name })))

// ── Secrets ──
const secrets = ref<CustomAppSecret[]>([])
const secretsLoading = ref(false)
const secretsError = ref('')
const secretsBusy = ref(false)

const creatingSecret = ref(false)
const pendingRotate = ref<CustomAppSecret | null>(null)
const pendingRevoke = ref<CustomAppSecret | null>(null)
const secretDraft = ref({ description: '', expiresIn: '', type: 'api_key' })
/**
 * The plaintext exists only between the create or rotate response and the
 * moment the reader dismisses it; the ledger below is metadata alone.
 */
const issued = ref('')

// ── Board widgets ──
const widgets = ref<BoardWidget[]>([])
const widgetsLoading = ref(false)
const widgetsError = ref('')
const widgetsBusy = ref(false)

const creatingWidget = ref(false)
const editingWidget = ref<BoardWidget | null>(null)
const pendingDeleteWidget = ref<BoardWidget | null>(null)
const widgetDraft = ref({
  key: '',
  name: '',
  description: '',
  required: '',
  maxBytes: '',
  allowMultiple: true,
  enabled: true,
})

// ── App products ──
const products = ref<AppProduct[]>([])
const productsLoading = ref(false)
const productsError = ref('')
const productsBusy = ref(false)

const creatingProduct = ref(false)
const editingProduct = ref<AppProduct | null>(null)
const pendingDeleteProduct = ref<AppProduct | null>(null)
const productDraft = ref({
  identifier: '',
  displayName: '',
  description: '',
  price: '',
  currency: 'CNY',
  recurrence: 'none' as Recurrence,
  stockMode: 'unlimited' as StockMode,
  stockQuantity: '',
  group: '',
})

function message(caught: unknown): string {
  return caught instanceof ExpressError ? caught.message : t('portal.error')
}

/** Splits a comma separated field list into the names the API stores. */
function names(value: string): string[] {
  return value
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
}

function listOf<T>(value: T[] | null | undefined): T[] {
  return value ?? []
}

// ── Loading ──

async function load() {
  loading.value = true
  error.value = ''
  try {
    const scope = props.scope
    apps.value = scope ? await customAppApi.list(scope) : []
    if (!apps.value.some((app) => app.id === selectedAppId.value)) {
      selectedAppId.value = apps.value[0]?.id ?? ''
    }
  } catch (caught) {
    error.value = message(caught)
  } finally {
    loading.value = false
  }
}

async function loadSecrets() {
  secretsLoading.value = true
  secretsError.value = ''
  try {
    const scope = props.scope
    const appId = selectedAppId.value
    secrets.value =
      scope && appId && canUpdateApp.value ? await secretApi.list(scope, appId) : []
  } catch (caught) {
    secretsError.value = message(caught)
  } finally {
    secretsLoading.value = false
  }
}

async function loadWidgets() {
  widgetsLoading.value = true
  widgetsError.value = ''
  try {
    const scope = props.scope
    const appId = selectedAppId.value
    widgets.value =
      scope && appId && canUpdateApp.value ? await boardApi.list(scope, appId) : []
  } catch (caught) {
    widgetsError.value = message(caught)
  } finally {
    widgetsLoading.value = false
  }
}

async function loadProducts() {
  productsLoading.value = true
  productsError.value = ''
  try {
    const scope = props.scope
    const appId = selectedAppId.value
    products.value =
      scope && appId && canCreateProduct.value ? await appProductApi.list(scope, appId) : []
  } catch (caught) {
    productsError.value = message(caught)
  } finally {
    productsLoading.value = false
  }
}

function loadDependents() {
  void loadSecrets()
  void loadWidgets()
  void loadProducts()
}

/** The project the panel is loaded for; a change starts the panel over. */
const scopeKey = computed(() =>
  props.scope && props.project ? `${props.scope.dev}/${props.project.id}` : '',
)

watch(scopeKey, () => {
  selectedAppId.value = ''
  issued.value = ''
  void load()
})

watch(selectedAppId, () => {
  issued.value = ''
  loadDependents()
})

// ── Custom app actions ──

function openCreateApp() {
  appDraft.value = { slug: '', name: '', description: '', status: 'developing' }
  creating.value = true
}

function openEditApp(app: CustomApp) {
  editing.value = app
  appDraft.value = {
    slug: app.slug,
    name: app.name,
    description: app.description ?? '',
    status: appStatus(app.status),
  }
}

function closeAppForm() {
  creating.value = false
  editing.value = null
}

async function createApp() {
  const slug = appDraft.value.slug.trim()
  if (!slug || !props.scope) return
  busy.value = true
  error.value = ''
  const body: CustomAppWrite = {
    slug,
    name: appDraft.value.name.trim() || slug,
    description: appDraft.value.description.trim(),
    status: appDraft.value.status,
  }
  try {
    const created = await customAppApi.create(props.scope, body)
    notice.ok(t('portalApps.created', { name: created.name }))
    creating.value = false
    selectedAppId.value = created.id
    await load()
  } catch (caught) {
    error.value = message(caught)
  } finally {
    busy.value = false
  }
}

async function saveApp() {
  const app = editing.value
  const scope = props.scope
  if (!app || !scope) return
  busy.value = true
  error.value = ''
  const body: CustomAppWrite = {
    name: appDraft.value.name.trim(),
    description: appDraft.value.description.trim(),
    status: appDraft.value.status,
    // The API clears any art reference it is not given, so the existing
    // references travel back with the values the form does not change.
    picture_id: app.picture?.id,
    background_id: app.background?.id,
  }
  try {
    await customAppApi.update(scope, app.id, body)
    notice.ok(t('portalApps.updated', { name: body.name ?? app.name }))
    editing.value = null
    await load()
  } catch (caught) {
    error.value = message(caught)
  } finally {
    busy.value = false
  }
}

async function removeApp() {
  const app = pendingDelete.value
  const scope = props.scope
  if (!app || !scope) return
  busy.value = true
  try {
    await customAppApi.remove(scope, app.id)
    notice.ok(t('portalApps.deleted'))
    pendingDelete.value = null
    if (selectedAppId.value === app.id) selectedAppId.value = ''
    await load()
  } catch (caught) {
    notice.fail(message(caught))
  } finally {
    busy.value = false
  }
}

// ── Secret actions ──

function openCreateSecret() {
  secretDraft.value = { description: '', expiresIn: '', type: 'api_key' }
  creatingSecret.value = true
}

/** An empty hours field means the secret never expires. */
function expiresValue(): string {
  const raw = secretDraft.value.expiresIn.trim()
  if (!raw) return ''
  const hours = Number(raw)
  return Number.isFinite(hours) ? expiryDuration(hours) : `${raw}h`
}

async function createSecret() {
  const scope = props.scope
  const appId = selectedAppId.value
  if (!scope || !appId) return
  secretsBusy.value = true
  secretsError.value = ''
  const body: SecretWrite = {
    description: secretDraft.value.description.trim(),
    expires_in: expiresValue(),
    type: secretDraft.value.type,
  }
  try {
    const created = await secretApi.create(scope, appId, body)
    issued.value = created.secret ?? ''
    notice.ok(t('portalSecrets.issued'))
    creatingSecret.value = false
    await loadSecrets()
  } catch (caught) {
    secretsError.value = message(caught)
  } finally {
    secretsBusy.value = false
  }
}

async function rotateSecret() {
  const target = pendingRotate.value
  const scope = props.scope
  const appId = selectedAppId.value
  if (!target || !scope || !appId) return
  secretsBusy.value = true
  try {
    const rotated = await secretApi.rotate(scope, appId, target.id)
    issued.value = rotated.secret ?? ''
    notice.ok(t('portalSecrets.rotated'))
    pendingRotate.value = null
    await loadSecrets()
  } catch (caught) {
    notice.fail(message(caught))
  } finally {
    secretsBusy.value = false
  }
}

async function revokeSecret() {
  const target = pendingRevoke.value
  const scope = props.scope
  const appId = selectedAppId.value
  if (!target || !scope || !appId) return
  secretsBusy.value = true
  try {
    await secretApi.remove(scope, appId, target.id)
    notice.ok(t('portalSecrets.revoked'))
    pendingRevoke.value = null
    await loadSecrets()
  } catch (caught) {
    notice.fail(message(caught))
  } finally {
    secretsBusy.value = false
  }
}

// ── Board widget actions ──

function openCreateWidget() {
  widgetDraft.value = {
    key: '',
    name: '',
    description: '',
    required: '',
    maxBytes: '',
    allowMultiple: true,
    enabled: true,
  }
  creatingWidget.value = true
}

function openEditWidget(widget: BoardWidget) {
  editingWidget.value = widget
  widgetDraft.value = {
    key: widget.key,
    name: widget.name,
    description: widget.description ?? '',
    required: listOf(widget.required_fields).join(', '),
    maxBytes:
      widget.max_payload_bytes === undefined || widget.max_payload_bytes === null
        ? ''
        : String(widget.max_payload_bytes),
    allowMultiple: widget.allow_multiple,
    enabled: widget.is_enabled,
  }
}

function closeWidgetForm() {
  creatingWidget.value = false
  editingWidget.value = null
}

/** An empty limit means the API stores no cap on the payload. */
function maxBytesValue(): number | null {
  const raw = widgetDraft.value.maxBytes.trim()
  if (!raw) return null
  const parsed = Number(raw)
  return Number.isFinite(parsed) ? parsed : null
}

function widgetBody(key: string, fieldTypes: BoardWidget['field_types']): BoardWidgetWrite {
  return {
    key,
    name: widgetDraft.value.name.trim() || key,
    description: widgetDraft.value.description.trim(),
    is_enabled: widgetDraft.value.enabled,
    renderer_type: 'data',
    field_types: fieldTypes,
    required_fields: names(widgetDraft.value.required),
    max_payload_bytes: maxBytesValue(),
    allow_multiple: widgetDraft.value.allowMultiple,
  }
}

async function createWidget() {
  const scope = props.scope
  const appId = selectedAppId.value
  const key = widgetDraft.value.key.trim()
  if (!scope || !appId || !key) return
  widgetsBusy.value = true
  widgetsError.value = ''
  try {
    const created = await boardApi.create(scope, appId, widgetBody(key, []))
    notice.ok(t('portalBoard.created', { name: created.name }))
    creatingWidget.value = false
    await loadWidgets()
  } catch (caught) {
    widgetsError.value = message(caught)
  } finally {
    widgetsBusy.value = false
  }
}

async function saveWidget() {
  const widget = editingWidget.value
  const scope = props.scope
  const appId = selectedAppId.value
  if (!widget || !scope || !appId) return
  widgetsBusy.value = true
  widgetsError.value = ''
  const body = widgetBody(widget.key, listOf(widget.field_types))
  body.renderer_type = widget.renderer_type || 'data'
  try {
    await boardApi.update(scope, appId, widget.key, body)
    notice.ok(t('portalBoard.updated', { name: body.name }))
    editingWidget.value = null
    await loadWidgets()
  } catch (caught) {
    widgetsError.value = message(caught)
  } finally {
    widgetsBusy.value = false
  }
}

async function removeWidget() {
  const widget = pendingDeleteWidget.value
  const scope = props.scope
  const appId = selectedAppId.value
  if (!widget || !scope || !appId) return
  widgetsBusy.value = true
  try {
    await boardApi.remove(scope, appId, widget.key)
    notice.ok(t('portalBoard.deleted'))
    pendingDeleteWidget.value = null
    await loadWidgets()
  } catch (caught) {
    notice.fail(message(caught))
  } finally {
    widgetsBusy.value = false
  }
}

// ── App product actions ──

function openCreateProduct() {
  productDraft.value = {
    identifier: '',
    displayName: '',
    description: '',
    price: '',
    currency: 'CNY',
    recurrence: 'none',
    stockMode: 'unlimited',
    stockQuantity: '',
    group: '',
  }
  creatingProduct.value = true
}

function openEditProduct(product: AppProduct) {
  editingProduct.value = product
  productDraft.value = {
    identifier: product.identifier,
    displayName: product.display_name,
    description: product.description ?? '',
    price: String(product.price),
    currency: product.currency,
    recurrence: recurrence(product.recurrence),
    stockMode: stockMode(product.state?.stock_mode ?? 0),
    stockQuantity:
      product.state?.stock_quantity === undefined || product.state.stock_quantity === null
        ? ''
        : String(product.state.stock_quantity),
    group: product.group_identifier ?? '',
  }
}

function closeProductForm() {
  creatingProduct.value = false
  editingProduct.value = null
}

function priceValue(): number {
  const parsed = Number(productDraft.value.price.trim())
  return Number.isFinite(parsed) ? parsed : 0
}

/** An empty quantity leaves the stored stock untouched. */
function stockQuantityValue(): number | undefined {
  const raw = productDraft.value.stockQuantity.trim()
  if (!raw) return undefined
  const parsed = Number(raw)
  return Number.isFinite(parsed) ? parsed : undefined
}

function productState(): NonNullable<AppProductWrite['state']> {
  const state: NonNullable<AppProductWrite['state']> = {
    stock_mode: productDraft.value.stockMode,
  }
  const quantity = stockQuantityValue()
  if (quantity !== undefined) state.stock_quantity = quantity
  return state
}

async function createProduct() {
  const scope = props.scope
  const appId = selectedAppId.value
  const identifier = productDraft.value.identifier.trim()
  if (!scope || !appId || !identifier) return
  productsBusy.value = true
  productsError.value = ''
  const body: AppProductWrite = {
    identifier,
    display_name: productDraft.value.displayName.trim() || identifier,
    currency: productDraft.value.currency.trim() || 'CNY',
    price: priceValue(),
    recurrence: productDraft.value.recurrence,
    state: productState(),
  }
  try {
    const created = await appProductApi.create(scope, appId, body)
    notice.ok(t('portalProducts.created', { name: created.display_name }))
    creatingProduct.value = false
    await loadProducts()
  } catch (caught) {
    productsError.value = message(caught)
  } finally {
    productsBusy.value = false
  }
}

async function saveProduct() {
  const product = editingProduct.value
  const scope = props.scope
  const appId = selectedAppId.value
  if (!product || !scope || !appId) return
  productsBusy.value = true
  productsError.value = ''
  const body: AppProductWrite = {
    display_name: productDraft.value.displayName.trim(),
    description: productDraft.value.description.trim(),
    price: priceValue(),
    recurrence: productDraft.value.recurrence,
    group_identifier: productDraft.value.group.trim(),
    // The API clears any art reference it is not given, so the existing
    // references travel back with the values the form does not change.
    picture_id: product.picture?.id,
    background_id: product.background?.id,
    state: productState(),
  }
  try {
    await appProductApi.update(scope, appId, product.id, body)
    notice.ok(t('portalProducts.updated', { name: body.display_name ?? product.display_name }))
    editingProduct.value = null
    await loadProducts()
  } catch (caught) {
    productsError.value = message(caught)
  } finally {
    productsBusy.value = false
  }
}

async function removeProduct() {
  const product = pendingDeleteProduct.value
  const scope = props.scope
  const appId = selectedAppId.value
  if (!product || !scope || !appId) return
  productsBusy.value = true
  try {
    await appProductApi.remove(scope, appId, product.id)
    notice.ok(t('portalProducts.deleted'))
    pendingDeleteProduct.value = null
    await loadProducts()
  } catch (caught) {
    notice.fail(message(caught))
  } finally {
    productsBusy.value = false
  }
}

onMounted(() => void load())
</script>

<template>
  <div>
    <ConsolePanel v-if="!props.scope" :title="t('portalApps.title')">
      <p class="voice text-[13px]">{{ t('portal.scopeMissing') }}</p>
    </ConsolePanel>

    <template v-else>
      <ConsolePanel
        :title="t('portalApps.title')"
        :note="loading ? undefined : String(apps.length)"
      >
        <template #header>
          <div class="flex flex-wrap items-center gap-1 sm:gap-2">
            <IconButton :label="t('common.refresh')" :disabled="loading" @click="load">
              <Renew16 aria-hidden="true" />
            </IconButton>
            <button v-if="canCreateApp" type="button" class="btn btn-ghost btn-xs" @click="openCreateApp">
              <Add16 class="mr-1" aria-hidden="true" />
              {{ t('portalApps.create') }}
            </button>
          </div>
        </template>

        <p class="voice mb-3 text-[12px]">{{ t('portalApps.description') }}</p>
        <p v-if="error" class="mb-3 border-l border-error pl-3 text-xs text-error">{{ error }}</p>

        <LoadingSkeleton v-if="loading" :rows="3" :columns="[3, 2, 2, 2]" header />

        <div v-else-if="!apps.length" class="py-6">
          <p class="voice text-[13px]">{{ t('portalApps.empty') }}</p>
        </div>

        <div v-else class="table-bleed overflow-x-auto">
          <table class="ledger ledger-stack">
            <thead>
              <tr>
                <th scope="col">{{ t('portalApps.columns.name') }}</th>
                <th scope="col">{{ t('portalApps.columns.slug') }}</th>
                <th scope="col">{{ t('portalApps.columns.status') }}</th>
                <th scope="col">{{ t('portalApps.columns.updated') }}</th>
                <th scope="col"><span class="sr-only">{{ t('common.actions') }}</span></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="app in apps" :key="app.id">
                <td class="whitespace-nowrap text-xs text-base-content" :data-label="t('portalApps.columns.name')">
                  {{ app.name }}
                </td>
                <td class="whitespace-nowrap font-mono text-2xs text-dim" :data-label="t('portalApps.columns.slug')">
                  {{ app.slug }}
                </td>
                <td class="whitespace-nowrap text-2xs text-dim" :data-label="t('portalApps.columns.status')">
                  {{ t(`portalApps.status.${appStatus(app.status)}`) }}
                </td>
                <td class="whitespace-nowrap text-2xs text-faint" :data-label="t('portalApps.columns.updated')">
                  {{ relative(app.updated_at) }}
                </td>
                <td class="whitespace-nowrap">
                  <div class="flex items-center justify-end gap-1">
                    <button
                      v-if="canUpdateApp"
                      type="button"
                      class="btn btn-ghost btn-xs text-dim"
                      @click="openEditApp(app)"
                    >
                      {{ t('common.edit') }}
                    </button>
                    <button
                      v-if="canDeleteApp"
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

      <TabBox
        v-if="apps.length"
        class="mt-6"
        :tabs="appTabs"
        :active="selectedAppId"
        :label="t('portalApps.title')"
        @select="selectedAppId = $event"
      />

      <section
        v-if="issued"
        class="mb-6 border border-warning/50 bg-warning/5 px-4 py-3"
        role="status"
      >
        <p class="text-2xs uppercase tracking-[0.14em] text-warning">{{ t('portalSecrets.issued') }}</p>
        <div class="mt-2 flex flex-wrap items-center gap-2">
          <code class="min-w-0 flex-1 truncate font-mono text-xs text-base-content">
            {{ issued }}
          </code>
          <CopyButton :value="issued" :label="t('portalSecrets.title')" />
          <button type="button" class="btn btn-ghost btn-xs" @click="issued = ''">
            {{ t('common.dismiss') }}
          </button>
        </div>
      </section>

      <ConsolePanel :title="t('portalSecrets.title')" :note="selectedApp?.name">
        <template #header>
          <div class="flex flex-wrap items-center gap-1 sm:gap-2">
            <IconButton
              v-if="canUpdateApp"
              :label="t('common.refresh')"
              :disabled="secretsLoading || !selectedApp"
              @click="loadSecrets"
            >
              <Renew16 aria-hidden="true" />
            </IconButton>
            <button
              v-if="canManageSecrets && selectedApp"
              type="button"
              class="btn btn-ghost btn-xs"
              @click="openCreateSecret"
            >
              <Add16 class="mr-1" aria-hidden="true" />
              {{ t('portalSecrets.create') }}
            </button>
          </div>
        </template>

        <p class="voice mb-3 text-[12px]">{{ t('portalSecrets.description') }}</p>
        <p v-if="secretsError" class="mb-3 border-l border-error pl-3 text-xs text-error">{{ secretsError }}</p>

        <LoadingSkeleton v-if="secretsLoading" :rows="3" :columns="[3, 2, 2, 2]" header />

        <div v-else-if="!selectedApp" class="py-6">
          <p class="voice text-[13px]">{{ t('portalApps.empty') }}</p>
        </div>

        <div v-else-if="!secrets.length" class="py-6">
          <p class="voice text-[13px]">{{ t('portalSecrets.empty') }}</p>
        </div>

        <div v-else class="table-bleed overflow-x-auto">
          <table class="ledger ledger-stack">
            <thead>
              <tr>
                <th scope="col">{{ t('portalSecrets.columns.description') }}</th>
                <th scope="col">{{ t('portalSecrets.columns.type') }}</th>
                <th scope="col">{{ t('portalSecrets.columns.created') }}</th>
                <th scope="col">{{ t('portalSecrets.columns.expires') }}</th>
                <th scope="col"><span class="sr-only">{{ t('common.actions') }}</span></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="secret in secrets" :key="secret.id">
                <td class="text-xs text-base-content" :data-label="t('portalSecrets.columns.description')">
                  {{ secret.description || t('portalSecrets.fieldDescription') }}
                </td>
                <td class="whitespace-nowrap text-2xs text-dim" :data-label="t('portalSecrets.columns.type')">
                  {{ t(`portalSecrets.types.${secret.type}`) }}
                </td>
                <td class="whitespace-nowrap text-2xs text-faint" :data-label="t('portalSecrets.columns.created')">
                  {{ relative(secret.created_at) }}
                </td>
                <td class="whitespace-nowrap text-2xs text-faint" :data-label="t('portalSecrets.columns.expires')">
                  {{ secret.expired_at ? relative(secret.expired_at) : t('common.never') }}
                </td>
                <td class="whitespace-nowrap">
                  <div class="flex items-center justify-end gap-1">
                    <button
                      v-if="canManageSecrets"
                      type="button"
                      class="btn btn-ghost btn-xs text-dim"
                      @click="pendingRotate = secret"
                    >
                      {{ t('portalSecrets.rotate') }}
                    </button>
                    <button
                      v-if="canManageSecrets"
                      type="button"
                      class="btn btn-ghost btn-xs text-dim hover:text-error"
                      @click="pendingRevoke = secret"
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

      <ConsolePanel :title="t('portalBoard.title')" :note="selectedApp?.name">
        <template #header>
          <div class="flex flex-wrap items-center gap-1 sm:gap-2">
            <IconButton
              v-if="canUpdateApp"
              :label="t('common.refresh')"
              :disabled="widgetsLoading || !selectedApp"
              @click="loadWidgets"
            >
              <Renew16 aria-hidden="true" />
            </IconButton>
            <button
              v-if="canUpdateApp && selectedApp"
              type="button"
              class="btn btn-ghost btn-xs"
              @click="openCreateWidget"
            >
              <Add16 class="mr-1" aria-hidden="true" />
              {{ t('portalBoard.create') }}
            </button>
          </div>
        </template>

        <p class="voice mb-3 text-[12px]">{{ t('portalBoard.description') }}</p>
        <p v-if="widgetsError" class="mb-3 border-l border-error pl-3 text-xs text-error">{{ widgetsError }}</p>

        <LoadingSkeleton v-if="widgetsLoading" :rows="3" :columns="[3, 2, 2, 2]" header />

        <div v-else-if="!selectedApp" class="py-6">
          <p class="voice text-[13px]">{{ t('portalApps.empty') }}</p>
        </div>

        <div v-else-if="!widgets.length" class="py-6">
          <p class="voice text-[13px]">{{ t('portalBoard.empty') }}</p>
        </div>

        <div v-else class="table-bleed overflow-x-auto">
          <table class="ledger ledger-stack">
            <thead>
              <tr>
                <th scope="col">{{ t('portalBoard.columns.key') }}</th>
                <th scope="col">{{ t('portalBoard.columns.name') }}</th>
                <th scope="col">{{ t('portalBoard.columns.fields') }}</th>
                <th scope="col">{{ t('portalBoard.columns.enabled') }}</th>
                <th scope="col"><span class="sr-only">{{ t('common.actions') }}</span></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="widget in widgets" :key="widget.key">
                <td class="whitespace-nowrap font-mono text-2xs text-dim" :data-label="t('portalBoard.columns.key')">
                  {{ widget.key }}
                </td>
                <td class="text-xs text-base-content" :data-label="t('portalBoard.columns.name')">
                  {{ widget.name }}
                </td>
                <td class="whitespace-nowrap text-2xs tabular-nums text-dim" :data-label="t('portalBoard.columns.fields')">
                  {{ listOf(widget.field_types).length }}
                </td>
                <td class="whitespace-nowrap text-2xs text-dim" :data-label="t('portalBoard.columns.enabled')">
                  {{ widget.is_enabled ? t('common.enabled') : t('common.disabled') }}
                </td>
                <td class="whitespace-nowrap">
                  <div class="flex items-center justify-end gap-1">
                    <button
                      v-if="canUpdateApp"
                      type="button"
                      class="btn btn-ghost btn-xs text-dim"
                      @click="openEditWidget(widget)"
                    >
                      {{ t('common.edit') }}
                    </button>
                    <button
                      v-if="canUpdateApp"
                      type="button"
                      class="btn btn-ghost btn-xs text-dim hover:text-error"
                      @click="pendingDeleteWidget = widget"
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

      <ConsolePanel :title="t('portalProducts.title')" :note="selectedApp?.name">
        <template #header>
          <div class="flex flex-wrap items-center gap-1 sm:gap-2">
            <IconButton
              v-if="canCreateProduct"
              :label="t('common.refresh')"
              :disabled="productsLoading || !selectedApp"
              @click="loadProducts"
            >
              <Renew16 aria-hidden="true" />
            </IconButton>
            <button
              v-if="canCreateProduct && selectedApp"
              type="button"
              class="btn btn-ghost btn-xs"
              @click="openCreateProduct"
            >
              <Add16 class="mr-1" aria-hidden="true" />
              {{ t('portalProducts.create') }}
            </button>
          </div>
        </template>

        <p class="voice mb-3 text-[12px]">{{ t('portalProducts.description') }}</p>
        <p v-if="productsError" class="mb-3 border-l border-error pl-3 text-xs text-error">{{ productsError }}</p>

        <LoadingSkeleton v-if="productsLoading" :rows="3" :columns="[3, 2, 3, 2, 2]" header />

        <div v-else-if="!selectedApp" class="py-6">
          <p class="voice text-[13px]">{{ t('portalApps.empty') }}</p>
        </div>

        <div v-else-if="!products.length" class="py-6">
          <p class="voice text-[13px]">{{ t('portalProducts.empty') }}</p>
        </div>

        <div v-else class="table-bleed overflow-x-auto">
          <table class="ledger ledger-stack">
            <thead>
              <tr>
                <th scope="col">{{ t('portalProducts.columns.identifier') }}</th>
                <th scope="col">{{ t('portalProducts.columns.name') }}</th>
                <th scope="col">{{ t('portalProducts.columns.price') }}</th>
                <th scope="col">{{ t('portalProducts.columns.recurrence') }}</th>
                <th scope="col">{{ t('portalProducts.columns.stock') }}</th>
                <th scope="col"><span class="sr-only">{{ t('common.actions') }}</span></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="product in products" :key="product.id">
                <td class="whitespace-nowrap font-mono text-2xs text-dim" :data-label="t('portalProducts.columns.identifier')">
                  {{ product.identifier }}
                </td>
                <td class="text-xs text-base-content" :data-label="t('portalProducts.columns.name')">
                  {{ product.display_name }}
                </td>
                <td class="whitespace-nowrap text-2xs tabular-nums text-dim" :data-label="t('portalProducts.columns.price')">
                  {{ product.currency }} {{ product.price }}
                </td>
                <td class="whitespace-nowrap text-2xs text-dim" :data-label="t('portalProducts.columns.recurrence')">
                  {{ t(`portalProducts.recurrences.${recurrence(product.recurrence)}`) }}
                </td>
                <td class="whitespace-nowrap text-2xs text-dim" :data-label="t('portalProducts.columns.stock')">
                  {{ t(`portalProducts.stockModes.${stockMode(product.state?.stock_mode ?? 0)}`) }}
                </td>
                <td class="whitespace-nowrap">
                  <div class="flex items-center justify-end gap-1">
                    <button
                      v-if="canUpdateProduct"
                      type="button"
                      class="btn btn-ghost btn-xs text-dim"
                      @click="openEditProduct(product)"
                    >
                      {{ t('common.edit') }}
                    </button>
                    <button
                      v-if="canDeleteProduct"
                      type="button"
                      class="btn btn-ghost btn-xs text-dim hover:text-error"
                      @click="pendingDeleteProduct = product"
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

    <dialog
      v-if="creating || editing"
      class="modal modal-open"
      aria-modal="true"
      :aria-label="creating ? t('portalApps.createTitle') : t('portalApps.editTitle')"
    >
      <div class="modal-box max-w-md rounded-[2px] border border-line bg-base-100 p-0 shadow-none">
        <form @submit.prevent="creating ? createApp() : saveApp()">
          <div class="px-5 py-4">
            <h2 class="title text-base text-base-content">
              {{ creating ? t('portalApps.createTitle') : t('portalApps.editTitle') }}
            </h2>
            <p v-if="creating" class="voice mt-2 text-[12px]">{{ t('portalApps.createBody') }}</p>
            <div class="mt-4 space-y-3">
              <div v-if="creating">
                <label class="field-label" for="app-slug">{{ t('portalApps.columns.slug') }}</label>
                <input
                  id="app-slug"
                  v-model="appDraft.slug"
                  class="input input-sm"
                  required
                  autocomplete="off"
                  spellcheck="false"
                  placeholder="my-app"
                />
                <p class="mt-1 text-2xs text-faint">{{ t('portal.fieldSlugHint') }}</p>
              </div>
              <div>
                <label class="field-label" for="app-name">{{ t('portalApps.columns.name') }}</label>
                <input
                  id="app-name"
                  v-model="appDraft.name"
                  class="input input-sm"
                  required
                  autocomplete="off"
                  placeholder="My App"
                />
              </div>
              <div>
                <label class="field-label" for="app-description">{{ t('portal.fieldDescription') }}</label>
                <textarea
                  id="app-description"
                  v-model="appDraft.description"
                  class="textarea textarea-sm"
                  rows="3"
                />
              </div>
              <div>
                <label class="field-label" for="app-status">{{ t('portalApps.columns.status') }}</label>
                <select id="app-status" v-model="appDraft.status" class="select select-sm">
                  <option value="developing">{{ t('portalApps.status.developing') }}</option>
                  <option value="staging">{{ t('portalApps.status.staging') }}</option>
                  <option value="production">{{ t('portalApps.status.production') }}</option>
                  <option value="suspended">{{ t('portalApps.status.suspended') }}</option>
                </select>
              </div>
            </div>
          </div>
          <footer class="flex items-center justify-end gap-2 border-t border-line px-5 py-3">
            <button type="button" class="btn btn-ghost btn-xs" :disabled="busy" @click="closeAppForm">
              {{ t('common.cancel') }}
            </button>
            <button type="submit" class="btn btn-primary btn-xs" :disabled="busy">
              {{ busy ? t('common.saving') : creating ? t('common.create') : t('common.save') }}
            </button>
          </footer>
        </form>
      </div>
      <button
        type="button"
        class="modal-backdrop bg-black/60"
        :aria-label="t('common.cancel')"
        @click="closeAppForm"
      />
    </dialog>

    <dialog v-if="creatingSecret" class="modal modal-open" aria-modal="true" :aria-label="t('portalSecrets.createTitle')">
      <div class="modal-box max-w-md rounded-[2px] border border-line bg-base-100 p-0 shadow-none">
        <form @submit.prevent="createSecret">
          <div class="px-5 py-4">
            <h2 class="title text-base text-base-content">{{ t('portalSecrets.createTitle') }}</h2>
            <p class="voice mt-2 text-[12px]">{{ t('portalSecrets.createBody') }}</p>
            <div class="mt-4 space-y-3">
              <div>
                <label class="field-label" for="secret-description">{{ t('portalSecrets.fieldDescription') }}</label>
                <input
                  id="secret-description"
                  v-model="secretDraft.description"
                  class="input input-sm"
                  autocomplete="off"
                  placeholder="Board writer"
                />
                <p class="mt-1 text-2xs text-faint">{{ t('portalSecrets.fieldDescriptionHint') }}</p>
              </div>
              <div>
                <label class="field-label" for="secret-expires">{{ t('portalSecrets.fieldExpires') }}</label>
                <input
                  id="secret-expires"
                  v-model="secretDraft.expiresIn"
                  class="input input-sm"
                  inputmode="numeric"
                  autocomplete="off"
                  placeholder="24"
                />
                <p class="mt-1 text-2xs text-faint">{{ t('portalSecrets.fieldExpiresHint') }}</p>
              </div>
              <div>
                <label class="field-label" for="secret-type">{{ t('portalSecrets.fieldType') }}</label>
                <select id="secret-type" v-model="secretDraft.type" class="select select-sm">
                  <option value="api_key">{{ t('portalSecrets.types.api_key') }}</option>
                  <option value="oidc">{{ t('portalSecrets.types.oidc') }}</option>
                </select>
              </div>
            </div>
          </div>
          <footer class="flex items-center justify-end gap-2 border-t border-line px-5 py-3">
            <button type="button" class="btn btn-ghost btn-xs" :disabled="secretsBusy" @click="creatingSecret = false">
              {{ t('common.cancel') }}
            </button>
            <button type="submit" class="btn btn-primary btn-xs" :disabled="secretsBusy">
              {{ secretsBusy ? t('common.saving') : t('common.create') }}
            </button>
          </footer>
        </form>
      </div>
      <button
        type="button"
        class="modal-backdrop bg-black/60"
        :aria-label="t('common.cancel')"
        @click="creatingSecret = false"
      />
    </dialog>

    <dialog
      v-if="creatingWidget || editingWidget"
      class="modal modal-open"
      aria-modal="true"
      :aria-label="creatingWidget ? t('portalBoard.createTitle') : t('portalBoard.editTitle')"
    >
      <div class="modal-box max-w-md rounded-[2px] border border-line bg-base-100 p-0 shadow-none">
        <form @submit.prevent="creatingWidget ? createWidget() : saveWidget()">
          <div class="px-5 py-4">
            <h2 class="title text-base text-base-content">
              {{ creatingWidget ? t('portalBoard.createTitle') : t('portalBoard.editTitle') }}
            </h2>
            <p v-if="creatingWidget" class="voice mt-2 text-[12px]">{{ t('portalBoard.createBody') }}</p>
            <div class="mt-4 space-y-3">
              <div>
                <label class="field-label" for="widget-key">{{ t('portalBoard.fieldKey') }}</label>
                <input
                  id="widget-key"
                  v-model="widgetDraft.key"
                  class="input input-sm"
                  required
                  :disabled="!!editingWidget"
                  autocomplete="off"
                  spellcheck="false"
                  placeholder="steps"
                />
                <p class="mt-1 text-2xs text-faint">{{ t('portalBoard.fieldKeyHint') }}</p>
              </div>
              <div>
                <label class="field-label" for="widget-name">{{ t('portalBoard.fieldName') }}</label>
                <input
                  id="widget-name"
                  v-model="widgetDraft.name"
                  class="input input-sm"
                  required
                  autocomplete="off"
                />
              </div>
              <div>
                <label class="field-label" for="widget-description">{{ t('portalBoard.fieldDescription') }}</label>
                <textarea
                  id="widget-description"
                  v-model="widgetDraft.description"
                  class="textarea textarea-sm"
                  rows="3"
                />
              </div>
              <div>
                <label class="field-label" for="widget-required">{{ t('portalBoard.fieldRequired') }}</label>
                <input
                  id="widget-required"
                  v-model="widgetDraft.required"
                  class="input input-sm"
                  autocomplete="off"
                  placeholder="steps, goal"
                />
                <p class="mt-1 text-2xs text-faint">{{ t('portalBoard.fieldRequiredHint') }}</p>
              </div>
              <div>
                <label class="field-label" for="widget-bytes">{{ t('portalBoard.fieldMaxBytes') }}</label>
                <input
                  id="widget-bytes"
                  v-model="widgetDraft.maxBytes"
                  class="input input-sm"
                  inputmode="numeric"
                  autocomplete="off"
                />
                <p class="mt-1 text-2xs text-faint">{{ t('portalBoard.fieldMaxBytesHint') }}</p>
              </div>
              <label class="flex items-center gap-2 text-xs text-dim">
                <input v-model="widgetDraft.allowMultiple" type="checkbox" class="checkbox checkbox-xs" />
                {{ t('portalBoard.fieldAllowMultiple') }}
              </label>
              <label class="flex items-center gap-2 text-xs text-dim">
                <input v-model="widgetDraft.enabled" type="checkbox" class="checkbox checkbox-xs" />
                {{ t('common.enabled') }}
              </label>
            </div>
          </div>
          <footer class="flex items-center justify-end gap-2 border-t border-line px-5 py-3">
            <button type="button" class="btn btn-ghost btn-xs" :disabled="widgetsBusy" @click="closeWidgetForm">
              {{ t('common.cancel') }}
            </button>
            <button type="submit" class="btn btn-primary btn-xs" :disabled="widgetsBusy">
              {{ widgetsBusy ? t('common.saving') : creatingWidget ? t('common.create') : t('common.save') }}
            </button>
          </footer>
        </form>
      </div>
      <button
        type="button"
        class="modal-backdrop bg-black/60"
        :aria-label="t('common.cancel')"
        @click="closeWidgetForm"
      />
    </dialog>

    <dialog
      v-if="creatingProduct || editingProduct"
      class="modal modal-open"
      aria-modal="true"
      :aria-label="creatingProduct ? t('portalProducts.createTitle') : t('common.edit')"
    >
      <div class="modal-box max-w-md rounded-[2px] border border-line bg-base-100 p-0 shadow-none">
        <form @submit.prevent="creatingProduct ? createProduct() : saveProduct()">
          <div class="px-5 py-4">
            <h2 class="title text-base text-base-content">
              {{ creatingProduct ? t('portalProducts.createTitle') : t('common.edit') }}
            </h2>
            <p v-if="creatingProduct" class="voice mt-2 text-[12px]">{{ t('portalProducts.createBody') }}</p>
            <div class="mt-4 space-y-3">
              <div v-if="creatingProduct">
                <label class="field-label" for="product-identifier">{{ t('portalProducts.fieldIdentifier') }}</label>
                <input
                  id="product-identifier"
                  v-model="productDraft.identifier"
                  class="input input-sm"
                  required
                  autocomplete="off"
                  spellcheck="false"
                  placeholder="pro-monthly"
                />
                <p class="mt-1 text-2xs text-faint">{{ t('portalProducts.fieldIdentifierHint') }}</p>
              </div>
              <div>
                <label class="field-label" for="product-name">{{ t('portalProducts.fieldDisplayName') }}</label>
                <input
                  id="product-name"
                  v-model="productDraft.displayName"
                  class="input input-sm"
                  required
                  autocomplete="off"
                />
              </div>
              <div class="grid grid-cols-2 gap-3">
                <div>
                  <label class="field-label" for="product-price">{{ t('portalProducts.fieldPrice') }}</label>
                  <input
                    id="product-price"
                    v-model="productDraft.price"
                    class="input input-sm"
                    inputmode="decimal"
                    autocomplete="off"
                    placeholder="12.5"
                  />
                </div>
                <div v-if="creatingProduct">
                  <label class="field-label" for="product-currency">{{ t('portalProducts.fieldCurrency') }}</label>
                  <input
                    id="product-currency"
                    v-model="productDraft.currency"
                    class="input input-sm"
                    required
                    autocomplete="off"
                    spellcheck="false"
                    placeholder="CNY"
                  />
                </div>
              </div>
              <div v-if="editingProduct">
                <label class="field-label" for="product-description">{{ t('portal.fieldDescription') }}</label>
                <textarea
                  id="product-description"
                  v-model="productDraft.description"
                  class="textarea textarea-sm"
                  rows="3"
                />
              </div>
              <div>
                <label class="field-label" for="product-recurrence">{{ t('portalProducts.fieldRecurrence') }}</label>
                <select id="product-recurrence" v-model="productDraft.recurrence" class="select select-sm">
                  <option value="none">{{ t('portalProducts.recurrences.none') }}</option>
                  <option value="weekly">{{ t('portalProducts.recurrences.weekly') }}</option>
                  <option value="monthly">{{ t('portalProducts.recurrences.monthly') }}</option>
                  <option value="yearly">{{ t('portalProducts.recurrences.yearly') }}</option>
                </select>
              </div>
              <div>
                <label class="field-label" for="product-stock-mode">{{ t('portalProducts.fieldStockMode') }}</label>
                <select id="product-stock-mode" v-model="productDraft.stockMode" class="select select-sm">
                  <option value="unlimited">{{ t('portalProducts.stockModes.unlimited') }}</option>
                  <option value="daily">{{ t('portalProducts.stockModes.daily') }}</option>
                  <option value="weekly">{{ t('portalProducts.stockModes.weekly') }}</option>
                  <option value="monthly">{{ t('portalProducts.stockModes.monthly') }}</option>
                  <option value="yearly">{{ t('portalProducts.stockModes.yearly') }}</option>
                  <option value="manual">{{ t('portalProducts.stockModes.manual') }}</option>
                </select>
              </div>
              <div>
                <label class="field-label" for="product-stock-quantity">{{ t('portalProducts.fieldStockQuantity') }}</label>
                <input
                  id="product-stock-quantity"
                  v-model="productDraft.stockQuantity"
                  class="input input-sm"
                  inputmode="numeric"
                  autocomplete="off"
                />
              </div>
              <div v-if="editingProduct">
                <label class="field-label" for="product-group">{{ t('portalProducts.fieldGroup') }}</label>
                <input
                  id="product-group"
                  v-model="productDraft.group"
                  class="input input-sm"
                  autocomplete="off"
                />
              </div>
            </div>
          </div>
          <footer class="flex items-center justify-end gap-2 border-t border-line px-5 py-3">
            <button type="button" class="btn btn-ghost btn-xs" :disabled="productsBusy" @click="closeProductForm">
              {{ t('common.cancel') }}
            </button>
            <button type="submit" class="btn btn-primary btn-xs" :disabled="productsBusy">
              {{ productsBusy ? t('common.saving') : creatingProduct ? t('common.create') : t('common.save') }}
            </button>
          </footer>
        </form>
      </div>
      <button
        type="button"
        class="modal-backdrop bg-black/60"
        :aria-label="t('common.cancel')"
        @click="closeProductForm"
      />
    </dialog>

    <ConfirmAction
      v-if="pendingDelete"
      :title="t('portalApps.deleteTitle')"
      :body="t('portalApps.deleteBody')"
      :action="t('portalApps.deleteAction')"
      tone="error"
      :busy="busy"
      @confirm="removeApp"
      @cancel="pendingDelete = null"
    />

    <ConfirmAction
      v-if="pendingRotate"
      :title="t('portalSecrets.rotateTitle')"
      :body="t('portalSecrets.rotateBody')"
      :action="t('portalSecrets.rotateAction')"
      tone="error"
      :busy="secretsBusy"
      @confirm="rotateSecret"
      @cancel="pendingRotate = null"
    />

    <ConfirmAction
      v-if="pendingRevoke"
      :title="t('portalSecrets.deleteTitle')"
      :body="t('portalSecrets.deleteBody')"
      :action="t('portalSecrets.deleteAction')"
      tone="error"
      :busy="secretsBusy"
      @confirm="revokeSecret"
      @cancel="pendingRevoke = null"
    />

    <ConfirmAction
      v-if="pendingDeleteWidget"
      :title="t('portalBoard.deleteTitle')"
      :body="t('portalBoard.deleteBody')"
      :action="t('portalBoard.deleteAction')"
      tone="error"
      :busy="widgetsBusy"
      @confirm="removeWidget"
      @cancel="pendingDeleteWidget = null"
    />

    <ConfirmAction
      v-if="pendingDeleteProduct"
      :title="t('portalProducts.deleteTitle')"
      :body="t('portalProducts.deleteBody')"
      :action="t('portalProducts.deleteAction')"
      tone="error"
      :busy="productsBusy"
      @confirm="removeProduct"
      @cancel="pendingDeleteProduct = null"
    />
  </div>
</template>
