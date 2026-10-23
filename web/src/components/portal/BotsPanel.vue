<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Add16, Renew16 } from '@carbon/icons-vue'

import { botApi, developerApi } from '@/lib/api/portal'
import type { BotChatConfigWrite } from '@/lib/api/portal'
import { ExpressError } from '@/lib/api/client'
import type {
  BotApiKey,
  BotChatConfig,
  BotView,
  DeveloperQuota,
  DevProject,
  PortalScope,
} from '@/lib/api/types'
import { relative } from '@/lib/format'
import { useNoticeStore } from '@/stores/notice'
import { useSessionStore } from '@/stores/session'
import ConsolePanel from '@/components/ConsolePanel.vue'
import LoadingSkeleton from '@/components/LoadingSkeleton.vue'
import IconButton from '@/components/IconButton.vue'
import ConfirmAction from '@/components/ConfirmAction.vue'
import CopyButton from '@/components/CopyButton.vue'

const props = defineProps<{
  scope: PortalScope | null
  project: DevProject | null
}>()

const { t } = useI18n()
const notice = useNoticeStore()
const session = useSessionStore()

// ── Permissions ──

const canCreate = computed(() => session.can('bot.accounts.create'))
const canUpdate = computed(() => session.can('bot.accounts.update'))
const canDelete = computed(() => session.can('bot.accounts.delete'))
const canManageKeys = computed(() => session.can('bot.accounts.keys.manage'))
const canManageChat = computed(() => session.can('bot.accounts.chat.manage'))

// ── Bots ──

const bots = ref<BotView[]>([])
const botsLoading = ref(true)
const botsError = ref('')
const busy = ref(false)

const selectedBotId = ref('')
const selectedBot = computed<BotView | null>(
  () => bots.value.find((bot) => bot.id === selectedBotId.value) ?? null,
)

const creating = ref(false)
const editing = ref<BotView | null>(null)
const pendingDelete = ref<BotView | null>(null)
const draft = ref({ slug: '', name: '', nick: '', bio: '', active: true })

/**
 * The bot row carries its identity account beside the registry entry, and the
 * account is an opaque object on the wire. Only the three readable fields are
 * read from it.
 */
interface BotIdentity {
  name?: string
  nick?: string
  bio?: string
}

function identity(bot: BotView): BotIdentity {
  const account = bot.account
  if (typeof account !== 'object' || account === null) return {}
  return account as BotIdentity
}

function botName(bot: BotView): string {
  return identity(bot).name ?? bot.slug
}

async function loadBots() {
  const scope = props.scope
  if (!scope) {
    bots.value = []
    botsLoading.value = false
    return
  }
  botsLoading.value = true
  botsError.value = ''
  try {
    bots.value = await botApi.list(scope)
  } catch (caught) {
    bots.value = []
    botsError.value = caught instanceof ExpressError ? caught.message : t('portal.error')
  } finally {
    botsLoading.value = false
  }
}

// ── Quota ──

const quota = ref<DeveloperQuota | null>(null)
const quotaLoading = ref(true)
const quotaError = ref('')

async function loadQuota() {
  // The quota belongs to the account, but the panel only means something once
  // a project is in scope; without one there is nothing to load at all.
  if (!props.scope) {
    quota.value = null
    quotaLoading.value = false
    return
  }
  quotaLoading.value = true
  quotaError.value = ''
  try {
    quota.value = await developerApi.quota()
  } catch (caught) {
    quota.value = null
    quotaError.value = caught instanceof ExpressError ? caught.message : t('portal.error')
  } finally {
    quotaLoading.value = false
  }
}

// ── API keys ──

const keys = ref<BotApiKey[]>([])
const keysLoading = ref(false)
const keysError = ref('')

/**
 * The plaintext of a bot API key is returned exactly once, by the create and
 * the rotate response. It is held here until the reader dismisses it, and
 * never comes from the listing.
 */
const issued = ref<{ value: string } | null>(null)

const creatingKey = ref(false)
const keyLabel = ref('')
const pendingRotate = ref<BotApiKey | null>(null)
const pendingRevoke = ref<BotApiKey | null>(null)

async function loadKeys() {
  const scope = props.scope
  const botId = selectedBotId.value
  if (!scope || !botId) {
    keys.value = []
    keysLoading.value = false
    return
  }
  keysLoading.value = true
  keysError.value = ''
  try {
    keys.value = await botApi.keys(scope, botId)
  } catch (caught) {
    keys.value = []
    keysError.value = caught instanceof ExpressError ? caught.message : t('portal.error')
  } finally {
    keysLoading.value = false
  }
}

function openCreateKey() {
  keyLabel.value = ''
  creatingKey.value = true
}

async function createKey() {
  const scope = props.scope
  const botId = selectedBotId.value
  const label = keyLabel.value.trim()
  if (!scope || !botId || !label) return
  busy.value = true
  keysError.value = ''
  try {
    const created = await botApi.createKey(scope, botId, label)
    const value = created.key?.value
    if (value) issued.value = { value }
    notice.ok(t('portalBots.keys.issued'))
    creatingKey.value = false
    await loadKeys()
  } catch (caught) {
    keysError.value = caught instanceof ExpressError ? caught.message : t('portal.error')
  } finally {
    busy.value = false
  }
}

async function rotateKey() {
  const scope = props.scope
  const botId = selectedBotId.value
  const key = pendingRotate.value
  if (!scope || !botId || !key) return
  busy.value = true
  try {
    const rotated = await botApi.rotateKey(scope, botId, key.id)
    const value = rotated.key?.value
    if (value) issued.value = { value }
    notice.ok(t('portalBots.keys.rotated'))
    pendingRotate.value = null
    await loadKeys()
  } catch (caught) {
    notice.fail(caught instanceof ExpressError ? caught.message : t('portal.error'))
  } finally {
    busy.value = false
  }
}

async function revokeKey() {
  const scope = props.scope
  const botId = selectedBotId.value
  const key = pendingRevoke.value
  if (!scope || !botId || !key) return
  busy.value = true
  try {
    await botApi.removeKey(scope, botId, key.id)
    notice.ok(t('portalBots.keys.revoked'))
    pendingRevoke.value = null
    await loadKeys()
  } catch (caught) {
    notice.fail(caught instanceof ExpressError ? caught.message : t('portal.error'))
  } finally {
    busy.value = false
  }
}

/** Protobuf timestamps arrive as seconds; the console reads ISO strings. */
function keyTime(value: { seconds: number; nanos?: number } | undefined): string {
  if (!value) return ''
  const ms = Number(value.seconds) * 1000
  if (!Number.isFinite(ms)) return ''
  return new Date(ms).toISOString()
}

// ── Chat behaviour ──

const chat = ref<BotChatConfig | null>(null)
const chatLoading = ref(false)
const chatError = ref('')
const chatDraft = ref({
  autoApproveDm: false,
  autoApproveGroupChat: false,
  supportChat: false,
  events: '',
})

async function loadChat() {
  const scope = props.scope
  const botId = selectedBotId.value
  if (!scope || !botId) {
    chat.value = null
    chatLoading.value = false
    return
  }
  chatLoading.value = true
  chatError.value = ''
  try {
    const config = await botApi.chat(scope, botId)
    chat.value = config
    chatDraft.value = {
      autoApproveDm: config.auto_approve_dm,
      autoApproveGroupChat: config.auto_approve_group_chat,
      supportChat: config.support_chat,
      events: config.subscribed_events.join(', '),
    }
  } catch (caught) {
    chat.value = null
    chatError.value = caught instanceof ExpressError ? caught.message : t('portal.error')
  } finally {
    chatLoading.value = false
  }
}

async function saveChat() {
  const scope = props.scope
  const botId = selectedBotId.value
  if (!scope || !botId) return
  busy.value = true
  chatError.value = ''
  const body: BotChatConfigWrite = {
    commands: chat.value?.commands ?? [],
    auto_approve_dm: chatDraft.value.autoApproveDm,
    auto_approve_group_chat: chatDraft.value.autoApproveGroupChat,
    support_chat: chatDraft.value.supportChat,
    subscribed_events: chatDraft.value.events
      .split(',')
      .map((event) => event.trim())
      .filter((event) => event.length > 0),
  }
  try {
    chat.value = await botApi.saveChat(scope, botId, body)
    notice.ok(t('portalBots.chat.saved'))
  } catch (caught) {
    chatError.value = caught instanceof ExpressError ? caught.message : t('portal.error')
  } finally {
    busy.value = false
  }
}

// ── Bot forms ──

function openCreate() {
  draft.value = { slug: '', name: '', nick: '', bio: '', active: true }
  creating.value = true
}

function openEdit(bot: BotView) {
  editing.value = bot
  const account = identity(bot)
  draft.value = {
    slug: bot.slug,
    name: account.name ?? '',
    nick: account.nick ?? '',
    bio: account.bio ?? '',
    active: bot.is_active,
  }
}

function closeForm() {
  creating.value = false
  editing.value = null
}

async function createBot() {
  const scope = props.scope
  const slug = draft.value.slug.trim()
  if (!scope || !slug) return
  busy.value = true
  botsError.value = ''
  try {
    const created = await botApi.create(scope, {
      slug,
      name: draft.value.name.trim() || slug,
      nick: draft.value.nick.trim(),
      bio: draft.value.bio.trim(),
    })
    notice.ok(t('portalBots.created', { name: created.slug }))
    creating.value = false
    await loadBots()
    await loadQuota()
  } catch (caught) {
    botsError.value = caught instanceof ExpressError ? caught.message : t('portal.error')
  } finally {
    busy.value = false
  }
}

async function saveBot() {
  const scope = props.scope
  const bot = editing.value
  if (!scope || !bot) return
  busy.value = true
  botsError.value = ''
  const slug = draft.value.slug.trim()
  const name = draft.value.name.trim()
  const nick = draft.value.nick.trim()
  const bio = draft.value.bio.trim()
  try {
    // An empty slug keeps the current one; an empty name, nick, or bio is
    // left out rather than sent as a blank, so an edit cannot clear a field
    // the reader did not mean to touch.
    await botApi.update(scope, bot.id, {
      ...(slug ? { slug } : {}),
      ...(name ? { name } : {}),
      ...(nick ? { nick } : {}),
      ...(bio ? { bio } : {}),
      is_active: draft.value.active,
    })
    notice.ok(t('portalBots.updated', { name: bot.slug }))
    editing.value = null
    await loadBots()
  } catch (caught) {
    botsError.value = caught instanceof ExpressError ? caught.message : t('portal.error')
  } finally {
    busy.value = false
  }
}

async function removeBot() {
  const scope = props.scope
  const bot = pendingDelete.value
  if (!scope || !bot) return
  busy.value = true
  try {
    await botApi.remove(scope, bot.id)
    notice.ok(t('portalBots.deleted'))
    pendingDelete.value = null
    if (selectedBotId.value === bot.id) selectedBotId.value = ''
    await loadBots()
    await loadQuota()
  } catch (caught) {
    notice.fail(caught instanceof ExpressError ? caught.message : t('portal.error'))
  } finally {
    busy.value = false
  }
}

// ── Wiring ──

function selectBot(botId: string) {
  selectedBotId.value = botId
}

watch(
  () => [props.scope?.dev, props.scope?.proj],
  () => {
    selectedBotId.value = ''
    issued.value = null
    void loadBots()
    void loadQuota()
  },
)

watch(selectedBotId, () => {
  issued.value = null
  void loadKeys()
  void loadChat()
})

onMounted(() => {
  void loadBots()
  void loadQuota()
})
</script>

<template>
  <ConsolePanel v-if="!scope" :title="t('portalBots.title')">
    <p class="voice text-[13px]">{{ t('portal.scopeMissing') }}</p>
  </ConsolePanel>

  <div v-else class="space-y-6">
    <!-- Quota first: it decides whether another bot can be created at all. -->
    <ConsolePanel
      :title="t('portal.quota.title')"
      :note="quotaLoading ? undefined : `${quota?.used ?? 0} / ${quota?.total ?? 0}`"
    >
      <p v-if="quotaError" class="mb-3 border-l border-error pl-3 text-xs text-error">
        {{ quotaError }}
      </p>

      <LoadingSkeleton v-if="quotaLoading" :rows="1" :columns="[2, 2, 2, 2]" />

      <dl v-else class="grid grid-cols-2 gap-3 sm:grid-cols-4">
        <div>
          <dt class="text-2xs uppercase tracking-[0.14em] text-faint">
            {{ t('portal.quota.used') }}
          </dt>
          <dd class="mt-1 font-mono text-sm text-base-content">{{ quota?.used ?? 0 }}</dd>
        </div>
        <div>
          <dt class="text-2xs uppercase tracking-[0.14em] text-faint">
            {{ t('portal.quota.total') }}
          </dt>
          <dd class="mt-1 font-mono text-sm text-base-content">{{ quota?.total ?? 0 }}</dd>
        </div>
        <div>
          <dt class="text-2xs uppercase tracking-[0.14em] text-faint">
            {{ t('portal.quota.remaining') }}
          </dt>
          <dd class="mt-1 font-mono text-sm text-base-content">{{ quota?.remaining ?? 0 }}</dd>
        </div>
        <div>
          <dt class="text-2xs uppercase tracking-[0.14em] text-faint">
            {{ t('portal.quota.level') }}
          </dt>
          <dd class="mt-1 font-mono text-sm text-base-content">{{ quota?.level ?? 0 }}</dd>
        </div>
      </dl>

      <p v-if="!quota?.records.length" class="voice mt-3 text-[12px] text-faint">
        {{ t('portal.quota.empty') }}
      </p>
    </ConsolePanel>

    <!-- Bots -->
    <ConsolePanel
      :title="t('portalBots.title')"
      :note="botsLoading ? undefined : String(bots.length)"
    >
      <template #header>
        <div class="flex flex-wrap items-center gap-1 sm:gap-2">
          <IconButton :label="t('common.refresh')" :disabled="botsLoading" @click="loadBots">
            <Renew16 aria-hidden="true" />
          </IconButton>
          <button v-if="canCreate" type="button" class="btn btn-ghost btn-xs" @click="openCreate">
            <Add16 class="mr-1" aria-hidden="true" />
            {{ t('portalBots.create') }}
          </button>
        </div>
      </template>

      <p class="voice mb-3 text-[12px]">{{ t('portalBots.description') }}</p>
      <p v-if="project" class="mb-3 text-2xs text-faint">{{ project.name }}</p>
      <p v-if="botsError" class="mb-3 border-l border-error pl-3 text-xs text-error">
        {{ botsError }}
      </p>

      <LoadingSkeleton v-if="botsLoading" :rows="4" :columns="[2, 3, 1, 2]" header />

      <div v-else-if="!bots.length" class="py-6">
        <p class="voice text-[13px]">{{ t('portalBots.empty') }}</p>
      </div>

      <div v-else class="table-bleed overflow-x-auto">
        <table class="ledger ledger-stack">
          <thead>
            <tr>
              <th scope="col">{{ t('portalBots.columns.slug') }}</th>
              <th scope="col">{{ t('portalBots.columns.name') }}</th>
              <th scope="col">{{ t('portalBots.columns.active') }}</th>
              <th scope="col">{{ t('portalBots.columns.updated') }}</th>
              <th scope="col"><span class="sr-only">{{ t('common.actions') }}</span></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="bot in bots" :key="bot.id">
              <td class="whitespace-nowrap" :data-label="t('portalBots.columns.slug')">
                <span class="font-mono text-xs text-base-content">{{ bot.slug }}</span>
              </td>
              <td class="text-xs text-dim" :data-label="t('portalBots.columns.name')">
                {{ botName(bot) }}
              </td>
              <td class="whitespace-nowrap" :data-label="t('portalBots.columns.active')">
                <span class="chip" :class="bot.is_active ? 'text-info' : 'text-faint'">
                  {{ bot.is_active ? t('common.enabled') : t('common.disabled') }}
                </span>
              </td>
              <td
                class="whitespace-nowrap text-2xs text-faint"
                :data-label="t('portalBots.columns.updated')"
              >
                {{ relative(bot.updated_at) }}
              </td>
              <td class="whitespace-nowrap">
                <div class="flex items-center justify-end gap-1">
                  <button
                    v-if="canUpdate"
                    type="button"
                    class="btn btn-ghost btn-xs text-dim"
                    @click="openEdit(bot)"
                  >
                    {{ t('common.edit') }}
                  </button>
                  <button
                    v-if="canDelete"
                    type="button"
                    class="btn btn-ghost btn-xs text-dim hover:text-error"
                    @click="pendingDelete = bot"
                  >
                    {{ t('common.delete') }}
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <!-- Which bot the API keys and the chat behaviour below act on. -->
      <div v-if="bots.length" class="mt-4 border-t border-line-soft pt-3">
        <p class="text-2xs uppercase tracking-[0.14em] text-faint">
          {{ t('portalBots.title') }}
        </p>
        <div class="mt-2 flex flex-wrap gap-2">
          <button
            v-for="bot in bots"
            :key="bot.id"
            type="button"
            class="chip"
            :class="bot.id === selectedBotId ? 'text-info' : 'text-faint'"
            :aria-pressed="bot.id === selectedBotId"
            @click="selectBot(bot.id)"
          >
            {{ bot.slug }}
          </button>
        </div>
      </div>
    </ConsolePanel>

    <!-- API keys -->
    <ConsolePanel
      :title="t('portalBots.keys.title')"
      :note="selectedBot ? selectedBot.slug : undefined"
    >
      <template #header>
        <div class="flex flex-wrap items-center gap-1 sm:gap-2">
          <IconButton
            :label="t('common.refresh')"
            :disabled="keysLoading || !selectedBot"
            @click="loadKeys"
          >
            <Renew16 aria-hidden="true" />
          </IconButton>
          <button
            v-if="canManageKeys && selectedBot"
            type="button"
            class="btn btn-ghost btn-xs"
            @click="openCreateKey"
          >
            <Add16 class="mr-1" aria-hidden="true" />
            {{ t('portalBots.keys.create') }}
          </button>
        </div>
      </template>

      <p class="voice mb-3 text-[12px]">{{ t('portalBots.keys.description') }}</p>
      <p v-if="keysError" class="mb-3 border-l border-error pl-3 text-xs text-error">
        {{ keysError }}
      </p>

      <!-- The plaintext key is the only thing here that cannot be recovered,
           so it sits above the list in a warning-toned block. -->
      <section
        v-if="issued"
        class="mb-3 border border-warning/50 bg-warning/5 px-4 py-3"
        role="status"
      >
        <p class="text-2xs uppercase tracking-[0.14em] text-warning">
          {{ t('portalBots.keys.issued') }}
        </p>
        <div class="mt-2 flex flex-wrap items-center gap-2">
          <code class="min-w-0 flex-1 truncate font-mono text-xs text-base-content">
            {{ issued.value }}
          </code>
          <CopyButton :value="issued.value" :label="t('portalBots.keys.title')" />
          <button type="button" class="btn btn-ghost btn-xs" @click="issued = null">
            {{ t('common.dismiss') }}
          </button>
        </div>
      </section>

      <div v-if="!selectedBot" class="py-6">
        <p class="voice text-[13px]">{{ t('portalBots.empty') }}</p>
      </div>

      <LoadingSkeleton v-else-if="keysLoading" :rows="3" :columns="[3, 2, 2]" header />

      <div v-else-if="!keys.length" class="py-6">
        <p class="voice text-[13px]">{{ t('portalBots.keys.empty') }}</p>
      </div>

      <div v-else class="table-bleed overflow-x-auto">
        <table class="ledger ledger-stack">
          <thead>
            <tr>
              <th scope="col">{{ t('portalBots.keys.columns.label') }}</th>
              <th scope="col">{{ t('portalBots.keys.columns.created') }}</th>
              <th scope="col"><span class="sr-only">{{ t('common.actions') }}</span></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="key in keys" :key="key.id">
              <td class="text-xs text-base-content" :data-label="t('portalBots.keys.columns.label')">
                {{ key.label }}
              </td>
              <td
                class="whitespace-nowrap text-2xs text-faint"
                :data-label="t('portalBots.keys.columns.created')"
              >
                {{ relative(keyTime(key.created_at)) }}
              </td>
              <td class="whitespace-nowrap">
                <div class="flex items-center justify-end gap-1">
                  <template v-if="canManageKeys">
                    <button
                      type="button"
                      class="btn btn-ghost btn-xs text-dim"
                      @click="pendingRotate = key"
                    >
                      {{ t('portalBots.keys.rotate') }}
                    </button>
                    <button
                      type="button"
                      class="btn btn-ghost btn-xs text-dim hover:text-error"
                      @click="pendingRevoke = key"
                    >
                      {{ t('common.delete') }}
                    </button>
                  </template>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </ConsolePanel>

    <!-- Chat behaviour -->
    <ConsolePanel
      :title="t('portalBots.chat.title')"
      :note="selectedBot ? selectedBot.slug : undefined"
    >
      <template #header>
        <div class="flex flex-wrap items-center gap-1 sm:gap-2">
          <IconButton
            :label="t('common.refresh')"
            :disabled="chatLoading || !selectedBot"
            @click="loadChat"
          >
            <Renew16 aria-hidden="true" />
          </IconButton>
        </div>
      </template>

      <p class="voice mb-3 text-[12px]">{{ t('portalBots.chat.description') }}</p>
      <p v-if="chatError" class="mb-3 border-l border-error pl-3 text-xs text-error">
        {{ chatError }}
      </p>

      <div v-if="!selectedBot" class="py-6">
        <p class="voice text-[13px]">{{ t('portalBots.empty') }}</p>
      </div>

      <LoadingSkeleton v-else-if="chatLoading" :rows="3" :columns="[3, 2, 2]" header />

      <div v-else class="space-y-4">
        <div class="space-y-2">
          <label class="flex items-center gap-2 text-2xs text-dim">
            <input
              v-model="chatDraft.autoApproveDm"
              type="checkbox"
              class="checkbox checkbox-xs"
              :disabled="!canManageChat"
            />
            {{ t('portalBots.chat.autoApproveDm') }}
          </label>
          <label class="flex items-center gap-2 text-2xs text-dim">
            <input
              v-model="chatDraft.autoApproveGroupChat"
              type="checkbox"
              class="checkbox checkbox-xs"
              :disabled="!canManageChat"
            />
            {{ t('portalBots.chat.autoApproveGroupChat') }}
          </label>
          <label class="flex items-center gap-2 text-2xs text-dim">
            <input
              v-model="chatDraft.supportChat"
              type="checkbox"
              class="checkbox checkbox-xs"
              :disabled="!canManageChat"
            />
            {{ t('portalBots.chat.supportChat') }}
          </label>
        </div>

        <div>
          <label class="field-label" for="bot-chat-events">{{ t('portalBots.chat.events') }}</label>
          <input
            id="bot-chat-events"
            v-model="chatDraft.events"
            class="input input-sm"
            autocomplete="off"
            spellcheck="false"
            placeholder="messages.new"
            :disabled="!canManageChat"
          />
          <p class="mt-1 text-2xs text-faint">{{ t('portalBots.chat.eventsHint') }}</p>
        </div>

        <div v-if="canManageChat">
          <button
            type="button"
            class="btn btn-primary btn-xs"
            :title="t('portalBots.chat.saveTitle')"
            :disabled="busy"
            @click="saveChat"
          >
            {{ busy ? t('common.saving') : t('common.save') }}
          </button>
        </div>

        <div>
          <p class="text-2xs uppercase tracking-[0.14em] text-faint">
            {{ t('portalBots.chat.commands') }}
          </p>
          <p v-if="!chat?.commands.length" class="voice mt-2 text-[12px] text-faint">
            {{ t('portalBots.chat.commandsEmpty') }}
          </p>
          <div v-else class="table-bleed mt-2 overflow-x-auto">
            <table class="ledger ledger-stack">
              <thead>
                <tr>
                  <th scope="col">{{ t('portalBots.chat.columnName') }}</th>
                  <th scope="col">{{ t('portalBots.chat.columnUsage') }}</th>
                  <th scope="col">{{ t('portalBots.chat.columnRequired') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="command in chat.commands" :key="command.name">
                  <td class="text-xs text-base-content" :data-label="t('portalBots.chat.columnName')">
                    {{ command.name }}
                  </td>
                  <td class="font-mono text-2xs text-dim" :data-label="t('portalBots.chat.columnUsage')">
                    {{ command.usage ?? '—' }}
                  </td>
                  <td class="text-2xs text-dim" :data-label="t('portalBots.chat.columnRequired')">
                    {{
                      command.parameters
                        ?.filter((parameter) => parameter.required)
                        .map((parameter) => parameter.name)
                        .join(', ') || '—'
                    }}
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </div>
    </ConsolePanel>
  </div>

  <!-- Create / edit bot -->
  <dialog
    v-if="creating || editing"
    class="modal modal-open"
    aria-modal="true"
    :aria-label="creating ? t('portalBots.createTitle') : t('portalBots.editTitle')"
  >
    <div class="modal-box max-w-md rounded-[2px] border border-line bg-base-100 p-0 shadow-none">
      <form @submit.prevent="creating ? createBot() : saveBot()">
        <div class="px-5 py-4">
          <h2 class="title text-base text-base-content">
            {{ creating ? t('portalBots.createTitle') : t('portalBots.editTitle') }}
          </h2>
          <p v-if="creating" class="voice mt-2 text-[12px]">{{ t('portalBots.createBody') }}</p>
          <div class="mt-4 space-y-3">
            <div>
              <label class="field-label" for="bot-slug">{{ t('portalBots.fieldSlug') }}</label>
              <input
                id="bot-slug"
                v-model="draft.slug"
                class="input input-sm"
                :required="creating"
                autocomplete="off"
                spellcheck="false"
                placeholder="my-bot"
              />
              <p class="mt-1 text-2xs text-faint">{{ t('portalBots.fieldSlugHint') }}</p>
            </div>
            <div>
              <label class="field-label" for="bot-name">{{ t('portalBots.fieldName') }}</label>
              <input id="bot-name" v-model="draft.name" class="input input-sm" autocomplete="off" />
            </div>
            <div>
              <label class="field-label" for="bot-nick">{{ t('portalBots.fieldNick') }}</label>
              <input id="bot-nick" v-model="draft.nick" class="input input-sm" autocomplete="off" />
            </div>
            <div>
              <label class="field-label" for="bot-bio">{{ t('portalBots.fieldBio') }}</label>
              <textarea id="bot-bio" v-model="draft.bio" class="textarea textarea-sm" rows="3" />
            </div>
            <label v-if="!creating" class="flex items-center gap-2 text-2xs text-dim">
              <input v-model="draft.active" type="checkbox" class="checkbox checkbox-xs" />
              {{ t('portalBots.fieldActive') }}
            </label>
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

  <!-- Create API key -->
  <dialog
    v-if="creatingKey"
    class="modal modal-open"
    aria-modal="true"
    :aria-label="t('portalBots.keys.createTitle')"
  >
    <div class="modal-box max-w-md rounded-[2px] border border-line bg-base-100 p-0 shadow-none">
      <form @submit.prevent="createKey">
        <div class="px-5 py-4">
          <h2 class="title text-base text-base-content">{{ t('portalBots.keys.createTitle') }}</h2>
          <p class="voice mt-2 text-[12px]">{{ t('portalBots.keys.createBody') }}</p>
          <div class="mt-4">
            <label class="field-label" for="bot-key-label">
              {{ t('portalBots.keys.fieldLabel') }}
            </label>
            <input
              id="bot-key-label"
              v-model="keyLabel"
              class="input input-sm"
              required
              autocomplete="off"
            />
            <p class="mt-1 text-2xs text-faint">{{ t('portalBots.keys.fieldLabelHint') }}</p>
          </div>
        </div>
        <footer class="flex items-center justify-end gap-2 border-t border-line px-5 py-3">
          <button type="button" class="btn btn-ghost btn-xs" @click="creatingKey = false">
            {{ t('common.cancel') }}
          </button>
          <button type="submit" class="btn btn-primary btn-xs" :disabled="busy || !keyLabel.trim()">
            {{ busy ? t('common.saving') : t('common.create') }}
          </button>
        </footer>
      </form>
    </div>
    <button
      type="button"
      class="modal-backdrop bg-black/60"
      :aria-label="t('common.cancel')"
      @click="creatingKey = false"
    />
  </dialog>

  <ConfirmAction
    v-if="pendingDelete"
    :title="t('portalBots.deleteTitle')"
    :body="t('portalBots.deleteBody')"
    :action="t('portalBots.deleteAction')"
    tone="error"
    :busy="busy"
    @confirm="removeBot"
    @cancel="pendingDelete = null"
  />

  <ConfirmAction
    v-if="pendingRotate"
    :title="t('portalBots.keys.rotateTitle')"
    :body="t('portalBots.keys.rotateBody')"
    :action="t('portalBots.keys.rotateAction')"
    :busy="busy"
    @confirm="rotateKey"
    @cancel="pendingRotate = null"
  />

  <ConfirmAction
    v-if="pendingRevoke"
    :title="t('portalBots.keys.deleteTitle')"
    :body="t('portalBots.keys.deleteBody')"
    :action="t('portalBots.keys.deleteAction')"
    tone="error"
    :busy="busy"
    @confirm="revokeKey"
    @cancel="pendingRevoke = null"
  />
</template>
