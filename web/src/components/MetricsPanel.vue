<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Renew16 } from '@carbon/icons-vue'

import { metricsApi } from '@/lib/api'
import { ExpressError } from '@/lib/api/client'
import type { UsageMetrics } from '@/lib/api/types'
import { compactNumber, count, utcStamp } from '@/lib/format'
import ConsolePanel from '@/components/ConsolePanel.vue'
import LoadingSkeleton from '@/components/LoadingSkeleton.vue'
import IconButton from '@/components/IconButton.vue'

const props = defineProps<{
  productId: string
  canRead: boolean
}>()

const { t } = useI18n()

const metrics = ref<UsageMetrics | null>(null)
const loading = ref(true)
const error = ref('')

interface Breakdown {
  key: string
  label: string
  rows: { name: string; value: number }[]
}

/** One ranked distribution, sorted by count with the tail kept legible. */
function ranked(source?: Record<string, number>): { name: string; value: number }[] {
  return Object.entries(source ?? {})
    .map(([name, value]) => ({ name: name || '—', value }))
    .sort((left, right) => right.value - left.value || left.name.localeCompare(right.name))
}

const breakdowns = computed<Breakdown[]>(() => {
  const source = metrics.value
  if (!source) return []
  return [
    { key: 'byVersion', label: t('metrics.byVersion'), rows: ranked(source.by_version) },
    { key: 'byChannel', label: t('metrics.byChannel'), rows: ranked(source.by_channel) },
    { key: 'byPlatform', label: t('metrics.byPlatform'), rows: ranked(source.by_platform) },
    {
      key: 'byArchitecture',
      label: t('metrics.byArchitecture'),
      rows: ranked(source.by_architecture),
    },
    { key: 'byLocale', label: t('metrics.byLocale'), rows: ranked(source.by_locale) },
    { key: 'byOSVersion', label: t('metrics.byOSVersion'), rows: ranked(source.by_os_version) },
    {
      key: 'byClientVersion',
      label: t('metrics.byClientVersion'),
      rows: ranked(source.by_client_version),
    },
  ].filter((item) => item.rows.length > 0)
})

const empty = computed(() => !loading.value && metrics.value !== null && !metrics.value.checks)

function share(value: number, rows: { value: number }[]): number {
  const total = rows.reduce((sum, row) => sum + row.value, 0)
  if (!total) return 0
  return Math.max(2, Math.round((value / total) * 100))
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    metrics.value = await metricsApi.usage(props.productId)
  } catch (caught) {
    metrics.value = null
    error.value = caught instanceof ExpressError ? caught.message : t('metrics.error')
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="space-y-6">
    <div v-if="!canRead" class="border border-line bg-base-200/30 px-4 py-6">
      <p class="voice text-[12px]">{{ t('console.forbidden') }}</p>
    </div>

    <template v-else>
      <p v-if="error" class="border-l border-error pl-3 text-xs text-error">{{ error }}</p>

      <LoadingSkeleton v-if="loading" :rows="3" :columns="[2, 2, 2]" />

      <template v-else-if="metrics">
        <div class="grid gap-4 sm:grid-cols-3">
          <ConsolePanel :title="t('metrics.checks')">
            <p class="font-mono text-lg tabular-nums text-base-content">
              {{ count(metrics.checks) }}
            </p>
          </ConsolePanel>
          <ConsolePanel :title="t('metrics.dau')">
            <p class="font-mono text-lg tabular-nums text-base-content">{{ count(metrics.dau) }}</p>
          </ConsolePanel>
          <ConsolePanel :title="t('metrics.mau')">
            <p class="font-mono text-lg tabular-nums text-base-content">{{ count(metrics.mau) }}</p>
          </ConsolePanel>
        </div>

        <ConsolePanel
          :title="t('metrics.range')"
          :note="`${utcStamp(metrics.from)} → ${utcStamp(metrics.to)}`"
        >
          <p class="voice text-[12px]">{{ t('metrics.description') }}</p>
        </ConsolePanel>

        <div v-if="empty" class="border border-line bg-base-200/30 px-4 py-6">
          <p class="voice text-[12px]">{{ t('metrics.empty') }}</p>
        </div>

        <div v-else class="grid gap-4 lg:grid-cols-2">
          <ConsolePanel
            v-for="breakdown in breakdowns"
            :key="breakdown.key"
            :title="breakdown.label"
            :note="compactNumber(breakdown.rows.reduce((sum, row) => sum + row.value, 0))"
          >
            <ul class="space-y-2">
              <li v-for="row in breakdown.rows.slice(0, 8)" :key="row.name">
                <div class="flex items-baseline justify-between gap-3 text-2xs">
                  <span class="min-w-0 truncate font-mono text-dim" :title="row.name">
                    {{ row.name }}
                  </span>
                  <span class="shrink-0 tabular-nums text-faint">{{ count(row.value) }}</span>
                </div>
                <div class="mt-1 h-[3px] w-full bg-base-300/60" aria-hidden="true">
                  <div
                    class="h-full bg-primary/70"
                    :style="{ width: `${share(row.value, breakdown.rows)}%` }"
                  />
                </div>
              </li>
            </ul>
          </ConsolePanel>
        </div>
      </template>
    </template>

    <div v-if="canRead" class="flex justify-end">
      <IconButton :label="t('common.refresh')" :disabled="loading" @click="load">
        <Renew16 aria-hidden="true" />
      </IconButton>
    </div>
  </div>
</template>
