<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ArrowLeft16, ChevronRight16, Download16, Launch16 } from '@carbon/icons-vue'
import { RouterLink, useRoute } from 'vue-router'

import { catalogApi } from '@/lib/api'
import { ExpressError } from '@/lib/api/client'
import type { Channel, ProductDetail, Release } from '@/lib/api/types'
import { channelApi } from '@/lib/api'
import { count, day, localized, target } from '@/lib/format'
import PageHeader from '@/components/PageHeader.vue'
import ConsolePanel from '@/components/ConsolePanel.vue'
import LoadingSkeleton from '@/components/LoadingSkeleton.vue'
import AppMark from '@/components/AppMark.vue'

const { t, locale } = useI18n()
const route = useRoute()

const detail = ref<ProductDetail | null>(null)
const channels = ref<Channel[]>([])
const loading = ref(true)
const error = ref('')
const missing = ref(false)

const productId = computed(() => String(route.params.productId ?? ''))

const productName = computed(() =>
  detail.value ? localized(detail.value.product.names, detail.value.product.name, locale.value) : '',
)
const description = computed(() =>
  detail.value
    ? localized(
        detail.value.product.descriptions,
        detail.value.product.description,
        locale.value,
      )
    : '',
)
const publisherName = computed(
  () => detail.value?.publisher?.nick || detail.value?.publisher?.name || '—',
)

/** Channels that carry a published release, newest stream first. */
const publishedChannels = computed(() => channels.value.filter((channel) => channel.latest))

function channelLabel(channel: Channel): string {
  return localized(channel.display_names, channel.display_name || channel.name, locale.value)
}

function releaseTitle(release: Release): string {
  return localized(release.titles, release.title || release.version, locale.value)
}

function releaseNotes(release: Release): string {
  return localized(release.descriptions, release.release_notes, locale.value)
}

async function load() {
  loading.value = true
  error.value = ''
  missing.value = false
  try {
    const [result, channelList] = await Promise.all([
      catalogApi.product(productId.value),
      channelApi.list(productId.value).catch(() => ({ data: [] as Channel[] })),
    ])
    detail.value = result
    channels.value = channelList.data ?? []
  } catch (caught) {
    if (caught instanceof ExpressError && caught.isNotFound) {
      missing.value = true
    } else {
      error.value = caught instanceof ExpressError ? caught.message : t('product.error')
    }
  } finally {
    loading.value = false
  }
}

watch(productId, () => void load())
onMounted(load)
</script>

<template>
  <div>
    <RouterLink to="/marketplace" class="mb-5 inline-flex items-center gap-1.5 text-2xs text-faint transition-colors hover:text-dim">
      <ArrowLeft16 aria-hidden="true" />
      {{ t('product.backToCatalog') }}
    </RouterLink>

    <p v-if="error" class="mb-4 border-l border-error pl-3 text-xs text-error">{{ error }}</p>

    <div v-if="missing" class="py-10">
      <p class="voice">{{ t('product.notFound') }}</p>
      <RouterLink to="/marketplace" class="btn btn-ghost btn-sm mt-4">
        {{ t('product.backToCatalog') }}
      </RouterLink>
    </div>

    <LoadingSkeleton v-else-if="loading" :rows="5" variant="rows" />

    <template v-else-if="detail">
      <!-- The download block drops below the identity on a phone: side by
           side, both halves shrink and the product name breaks into a
           three-word column. -->
      <header class="mb-7 flex flex-col gap-4 sm:flex-row sm:flex-wrap sm:items-start">
        <AppMark :picture="detail.product.icon" :name="productName" size="lg" class="hidden sm:block" />
        <div class="min-w-0 flex-1">
          <h1 class="title text-xl text-base-content sm:text-2xl">{{ productName }}</h1>
          <p class="mt-1 text-2xs text-faint">
            {{ t('product.by', { publisher: publisherName }) }}
            <span class="mx-1 text-line">·</span>
            <span class="font-mono">{{ detail.product.slug }}</span>
          </p>
          <p class="voice mt-3 max-w-2xl">
            {{ description || t('product.noDescription') }}
          </p>
        </div>
        <div v-if="detail.latest" class="shrink-0 text-left sm:text-right">
          <p class="eyebrow">{{ t('catalog.latest') }}</p>
          <p class="mt-1 font-mono text-sm text-base-content">{{ detail.latest.version }}</p>
          <a
            v-if="detail.latest.artifacts.length"
            :href="detail.latest.artifacts.find((artifact) => artifact.download_url)?.download_url"
            class="btn btn-primary btn-xs mt-2"
            :class="
              detail.latest.artifacts.some((artifact) => artifact.download_url) ? '' : 'btn-disabled'
            "
          >
            <Download16 class="mr-1" aria-hidden="true" />
            {{ t('product.download') }}
          </a>
        </div>
      </header>

      <div v-if="detail.product.previews?.length" class="mb-6 flex gap-3 overflow-x-auto pb-1">
        <img
          v-for="preview in detail.product.previews"
          :key="preview.id || preview.url"
          :src="preview.url"
          alt=""
          class="h-40 w-auto shrink-0 border border-line object-cover"
        />
      </div>

      <div class="space-y-6">
        <ConsolePanel :title="t('product.channels')" :note="t('product.targets', { n: channels.length })">
          <p v-if="!publishedChannels.length" class="voice py-4 text-[12px]">
            {{ t('product.noReleases') }}
          </p>
          <ul v-else class="-my-1">
            <li
              v-for="channel in publishedChannels"
              :key="channel.id"
              class="border-b border-line-soft py-3 last:border-0"
            >
              <div class="flex items-baseline justify-between gap-3">
                <p class="min-w-0 truncate text-[13px] text-base-content">
                  {{ channelLabel(channel) }}
                  <!-- The wire name repeats the label whenever no display name
                       is set, so it is only printed when it says something new. -->
                  <span
                    v-if="channel.name !== channelLabel(channel)"
                    class="ml-1.5 font-mono text-2xs text-faint"
                  >
                    {{ channel.name }}
                  </span>
                </p>
                <p v-if="channel.latest" class="shrink-0 font-mono text-xs text-dim">
                  {{ channel.latest.version }}
                </p>
              </div>
              <p v-if="channel.latest" class="text-2xs text-faint">
                {{ releaseTitle(channel.latest) }}
                <span class="mx-1 text-line">·</span>
                {{ day(channel.latest.published_at) }}
              </p>
              <div v-if="channel.latest" class="mt-2 flex flex-wrap items-center gap-1.5">
                <span
                  v-for="artifact in channel.latest.artifacts"
                  :key="artifact.id"
                  class="chip"
                  :class="artifact.expired ? 'text-faint line-through' : ''"
                  :title="artifact.file_name"
                >
                  {{ target(artifact.platform, artifact.architecture) }}
                </span>
              </div>
            </li>
          </ul>
        </ConsolePanel>

        <ConsolePanel
          v-if="detail.latest"
          :title="t('product.notes')"
          :note="releaseTitle(detail.latest)"
        >
          <p class="whitespace-pre-line font-sans text-[13px] leading-relaxed text-dim">
            {{ releaseNotes(detail.latest) || t('product.noReleases') }}
          </p>
          <ul v-if="detail.latest.artifacts.length" class="mt-4 space-y-1.5">
            <li
              v-for="artifact in detail.latest.artifacts"
              :key="artifact.id"
              class="flex flex-wrap items-baseline gap-x-3 gap-y-1 text-2xs"
            >
              <span class="w-28 shrink-0 font-mono text-dim">
                {{ target(artifact.platform, artifact.architecture) }}
              </span>
              <!-- A file name is the one value worth wrapping: on a phone it
                   is longer than the line and clipping it hides the suffix
                   that says which archive this is. -->
              <span class="min-w-0 flex-1 break-all text-faint" :title="artifact.file_name">
                {{ artifact.file_name }}
              </span>
              <a
                v-if="artifact.download_url"
                :href="artifact.download_url"
                class="link-quiet"
                :title="artifact.file_name"
              >
                <Download16 aria-hidden="true" />
              </a>
              <span v-else class="text-faint">{{ t('product.expired') }}</span>
            </li>
          </ul>
          <p class="mt-3 text-2xs text-faint">
            {{ t('common.downloads') }}: {{ count(detail.latest.download_count) }}
          </p>
        </ConsolePanel>

        <div>
          <RouterLink :to="`/workspace/${detail.product.id}`" class="btn btn-outline btn-xs">
            {{ t('workspace.openConsole') }}
            <ChevronRight16 class="ml-1" aria-hidden="true" />
          </RouterLink>
          <a
            v-if="detail.publisher?.name"
            :href="`https://solsynth.dev/@${detail.publisher.name}`"
            class="btn btn-ghost btn-xs ml-2"
            target="_blank"
            rel="noreferrer"
          >
            {{ publisherName }}
            <Launch16 class="ml-1" aria-hidden="true" />
          </a>
        </div>
      </div>
    </template>
  </div>
</template>
