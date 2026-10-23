<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  Catalog16,
  Close16,
  Code16,
  Menu16,
  Moon16,
  Sun16,
  UserAvatar16,
  Workspace16,
} from '@carbon/icons-vue'
import { RouterLink, RouterView, useRoute } from 'vue-router'

import { useNoticeStore } from '@/stores/notice'
import { useSessionStore } from '@/stores/session'
import { useUiStore } from '@/stores/ui'
import { useWorkspaceStore } from '@/stores/workspace'
import { useModulesStore, MODULES } from '@/stores/modules'
import { applyRouteTitle } from '@/router'
import { signInUrl } from '@/lib/auth'
import { localized } from '@/lib/format'
import type { Locale } from '@/lib/prefs'
import AppMark from '@/components/AppMark.vue'

const session = useSessionStore()
const notice = useNoticeStore()
const ui = useUiStore()
const workspace = useWorkspaceStore()
const modules = useModulesStore()
const { t, locale } = useI18n()
const route = useRoute()

/**
 * The shell's destinations. The developer portal is a module: a deployment
 * with every `app.solian.develop.*` module disabled serves no portal route,
 * so the entry point is dropped rather than left to 404. The lookup is
 * fail-open — before `/api/modules` answers, every surface stays offered, and
 * the portal's own pages refuse what they cannot serve.
 */
const primaryNav = computed(() => {
  const items = [
    { to: '/marketplace', labelKey: 'app.nav.catalog', icon: Catalog16 },
    { to: '/workspace', labelKey: 'app.nav.workspace', icon: Workspace16 },
  ]
  if (modules.has(MODULES.developers)) {
    items.push({ to: '/developer', labelKey: 'app.nav.developer', icon: Code16 })
  }
  return items
})

/** The product whose console is open, so the sidebar can mark it. */
const activeProductId = computed(() =>
  route.path.startsWith('/workspace/') && route.params.productId
    ? String(route.params.productId)
    : '',
)

/**
 * The sidebar belongs to the console, not the shell. The marketplace is a
 * public catalogue that reads as one wide page, and only a publisher
 * workspace has a tree worth putting beside it — so the rail appears when a
 * product console is open and the page otherwise uses the full width.
 */
const showSidebar = computed(() => session.signedIn && route.meta.sidebar === true)

const drawerOpen = ref(false)
const accountOpen = ref(false)
const accountRoot = ref<HTMLElement | null>(null)
const pictureFailed = ref(false)

const displayName = computed(
  () => session.account?.nick || session.account?.name || t('app.signedOut'),
)
const picture = computed(() => session.account?.picture)

const latest = computed(() => notice.items[notice.items.length - 1])
const year = new Date().getFullYear()

function closeMenus(event: MouseEvent) {
  if (!accountRoot.value?.contains(event.target as Node)) accountOpen.value = false
}

function signIn() {
  window.location.href = signInUrl()
}

/** Ends this console's session; the issuer's own session is not ours to end. */
async function signOut() {
  accountOpen.value = false
  await session.signOut()
}

function productName(product: { names?: Record<string, string>; name: string }): string {
  return localized(product.names, product.name, locale.value)
}

// The sidebar lists what the account can open; a signed-out visitor sees only
// the two entry points in the top bar.
watch(
  () => session.signedIn,
  (signedIn) => {
    if (signedIn) void workspace.load()
  },
)

watch(
  () => route.fullPath,
  () => {
    drawerOpen.value = false
    accountOpen.value = false
    void workspace.load(true)
  },
)

watch(locale, () => applyRouteTitle(route.meta))

onMounted(() => {
  document.addEventListener('click', closeMenus)
  // The module set gates the shell's own navigation, so it is resolved with
  // the session rather than in the view that first needs it. `GET /api/modules`
  // is anonymous, so the two do not have to wait on each other.
  void modules.load()
  void session.boot().then(() => void workspace.load())
})
onBeforeUnmount(() => document.removeEventListener('click', closeMenus))

function changeLocale(event: Event) {
  ui.setLocale((event.target as HTMLSelectElement).value as Locale)
}
</script>

<template>
  <div class="flex min-h-dvh flex-col bg-base-100">
    <a
      href="#main"
      class="sr-only focus:not-sr-only focus:absolute focus:left-4 focus:top-4 focus:z-50 focus:bg-base-300 focus:px-3 focus:py-2 focus:text-xs"
    >
      {{ t('app.skipToContent') }}
    </a>

    <!-- Top bar: identity and the two workspace-level destinations. The
         per-product tree lives in the console's sidebar, so this row stays
         short even with several publishers. -->
    <header class="sticky top-0 z-30 border-b border-line bg-base-100/95 backdrop-blur">
      <div class="flex h-12 items-center gap-3 px-3 sm:px-5">
        <button
          v-if="showSidebar"
          type="button"
          class="btn btn-ghost btn-xs px-1.5 lg:hidden"
          :aria-label="t('app.openNavigation')"
          :title="t('app.openNavigation')"
          :aria-expanded="drawerOpen"
          @click="drawerOpen = !drawerOpen"
        >
          <Menu16 aria-hidden="true" />
        </button>

        <RouterLink to="/marketplace" class="flex shrink-0 items-center gap-2.5" :aria-label="t('app.home')">
          <span class="brand-mark h-[20px] w-[20px] shrink-0" aria-hidden="true" />
          <span class="text-[13px] font-semibold tracking-[0.14em] text-base-content">EXPRESS</span>
        </RouterLink>

        <span class="hidden text-2xs tracking-[0.04em] text-faint sm:inline">
          {{ t('app.network') }}
        </span>

        <nav class="ml-2 hidden items-center gap-1 md:flex" :aria-label="t('app.console')">
          <RouterLink
            v-for="item in primaryNav"
            :key="item.to"
            :to="item.to"
            view-transition
            class="flex items-center gap-1.5 rounded-[2px] px-2 py-1 text-2xs tracking-[0.04em] transition-colors"
            :class="
              route.path.startsWith(item.to)
                ? 'bg-base-300 text-base-content'
                : 'text-faint hover:bg-base-200 hover:text-dim'
            "
          >
            <component :is="item.icon" class="shrink-0" aria-hidden="true" />
            {{ t(item.labelKey) }}
          </RouterLink>
        </nav>

        <div class="ml-auto flex items-center gap-1.5">
          <button
            type="button"
            class="btn btn-ghost btn-xs px-1.5"
            :title="ui.isDark ? t('app.switchToLight') : t('app.switchToDark')"
            :aria-label="ui.isDark ? t('app.switchToLight') : t('app.switchToDark')"
            @click="ui.toggleTheme()"
          >
            <component :is="ui.isDark ? Sun16 : Moon16" aria-hidden="true" />
          </button>
          <select
            class="select select-xs control-select hidden sm:block"
            :value="ui.locale"
            :aria-label="t('app.language')"
            @change="changeLocale"
          >
            <option value="en">{{ t('app.languageEnglish') }}</option>
            <option value="zh-CN">{{ t('app.languageChinese') }}</option>
          </select>

          <button v-if="!session.signedIn" type="button" class="btn btn-primary btn-xs" @click="signIn">
            {{ t('app.signIn') }}
          </button>

          <div v-else ref="accountRoot" class="dropdown dropdown-end">
            <button
              type="button"
              class="flex items-center gap-2"
              :title="displayName"
              :aria-label="displayName"
              aria-haspopup="menu"
              :aria-expanded="accountOpen"
              @click="accountOpen = !accountOpen"
            >
              <img
                v-if="picture && !pictureFailed"
                :src="picture"
                alt=""
                class="h-6 w-6 rounded-[2px] object-cover"
                @error="pictureFailed = true"
              />
              <span
                v-else
                class="flex h-6 w-6 items-center justify-center rounded-[2px] border border-line text-faint"
              >
                <UserAvatar16 aria-hidden="true" />
              </span>
            </button>
            <ul
              v-if="accountOpen"
              class="menu dropdown-content z-50 mt-2 w-52 border border-line bg-base-200 p-1 shadow-none"
            >
              <li class="px-3 py-2">
                <span class="block truncate text-2xs text-base-content">{{ displayName }}</span>
                <span class="block truncate text-2xs text-faint">
                  {{ session.publishers.map((publisher) => publisher.name).join(', ') || '—' }}
                </span>
              </li>
              <li v-for="item in primaryNav" :key="item.to" class="md:hidden">
                <RouterLink :to="item.to">{{ t(item.labelKey) }}</RouterLink>
              </li>
              <li>
                <button type="button" @click="signOut">{{ t('app.signOut') }}</button>
              </li>
            </ul>
          </div>
        </div>
      </div>
    </header>

    <div class="flex flex-1">
      <!-- Console sidebar: the account's publishers and their products. Only a
           product console has a tree worth putting beside the page, so the
           marketplace and the workspace itself use the full width. -->
      <aside
        v-if="showSidebar"
        class="fixed inset-y-0 left-0 z-40 w-64 shrink-0 overflow-y-auto border-r border-line bg-base-100 transition-transform lg:sticky lg:top-12 lg:z-0 lg:h-[calc(100dvh-3rem)] lg:translate-x-0 lg:bg-base-100/40"
        :class="drawerOpen ? 'translate-x-0' : '-translate-x-full'"
        :aria-label="t('app.publishers')"
      >
        <div class="flex items-center justify-between px-4 py-3 lg:hidden">
          <span class="text-2xs uppercase tracking-[0.14em] text-faint">{{ t('app.publishers') }}</span>
          <button
            type="button"
            class="btn btn-ghost btn-xs px-1"
            :aria-label="t('common.close')"
            @click="drawerOpen = false"
          >
            <Close16 aria-hidden="true" />
          </button>
        </div>

        <nav class="px-2 pb-6 pt-2 lg:pt-4" :aria-label="t('app.publishers')">
          <div v-if="workspace.loading && !workspace.loaded" class="space-y-2 px-2">
            <span class="skeleton h-3 w-32" aria-hidden="true" />
            <span class="skeleton h-3 w-44" aria-hidden="true" />
            <span class="skeleton h-3 w-36" aria-hidden="true" />
          </div>

          <p v-else-if="!session.publishers.length" class="voice px-2 text-[12px]">
            {{ t('app.noPublishers') }}
          </p>

          <div v-for="group in workspace.grouped" :key="group.publisher.id" class="mb-4">
            <p class="flex items-baseline gap-1.5 px-2 pb-1.5 text-2xs uppercase tracking-[0.14em] text-faint">
              <span class="truncate">{{ group.publisher.nick || group.publisher.name }}</span>
              <span class="shrink-0 text-line">{{ group.products.length }}</span>
            </p>
            <ul>
              <li v-for="product in group.products" :key="product.id">
                <RouterLink
                  :to="`/workspace/${product.id}`"
                  view-transition
                  class="flex items-center gap-2 rounded-[2px] px-2 py-1.5 transition-colors"
                  :class="
                    activeProductId === product.id
                      ? 'bg-base-300 text-base-content'
                      : 'text-dim hover:bg-base-200 hover:text-base-content'
                  "
                >
                  <AppMark :picture="product.icon" :name="productName(product)" flat size="sm" />
                  <span class="min-w-0 flex-1 truncate text-xs">{{ productName(product) }}</span>
                </RouterLink>
              </li>
            </ul>
          </div>

          <p
            v-if="session.publishers.length && !workspace.products.length && !workspace.loading"
            class="voice px-2 text-[12px]"
          >
            {{ t('workspace.empty') }}
          </p>
        </nav>
      </aside>

      <button
        v-if="drawerOpen && showSidebar"
        type="button"
        class="fixed inset-0 z-30 bg-black/60 lg:hidden"
        :aria-label="t('common.close')"
        @click="drawerOpen = false"
      />

      <main id="main" class="min-w-0 flex-1">
        <div
          class="mx-auto w-full px-4 py-6 sm:px-7 sm:py-8"
          :class="route.meta.narrow ? 'max-w-[560px]' : 'max-w-[1440px]'"
        >
          <RouterView v-slot="{ Component }">
            <component :is="Component" />
          </RouterView>
        </div>

        <!-- The status rail closes the page the way a terminal closes a
             command: the outcome is printed where the work happened. -->
        <Transition name="notice">
          <div
            v-if="latest"
            class="sticky bottom-0 flex items-baseline gap-2 border-t border-line bg-base-100/95 px-4 py-2 text-2xs backdrop-blur sm:px-7"
            role="status"
            aria-live="polite"
          >
            <span :class="latest.tone === 'error' ? 'text-error' : 'text-success'">
              {{ latest.tone === 'error' ? 'ERROR' : 'OK' }}
            </span>
            <span class="truncate text-dim">{{ latest.message }}</span>
          </div>
        </Transition>
      </main>
    </div>

    <footer class="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 border-t border-line px-4 py-3 text-2xs text-faint sm:px-7">
      <span>{{ t('app.copyright', { year }) }}</span>
      <span class="hidden sm:inline">{{ t('app.footerHint') }}</span>
    </footer>
  </div>
</template>
