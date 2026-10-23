<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Add16, Renew16 } from '@carbon/icons-vue'
import { RouterLink } from 'vue-router'

import { developerApi } from '@/lib/api/portal'
import { ExpressError } from '@/lib/api/client'
import type { Developer } from '@/lib/api/types'
import { useNoticeStore } from '@/stores/notice'
import { usePortalStore } from '@/stores/portal'
import { useSessionStore } from '@/stores/session'
import PageHeader from '@/components/PageHeader.vue'
import ConsolePanel from '@/components/ConsolePanel.vue'
import LoadingSkeleton from '@/components/LoadingSkeleton.vue'
import IconButton from '@/components/IconButton.vue'
import SessionGate from '@/components/SessionGate.vue'

const { t } = useI18n()
const session = useSessionStore()
const portal = usePortalStore()
const notice = useNoticeStore()

const busy = ref(false)
const error = ref('')

const loading = computed(() => portal.loading && !portal.loaded)

/**
 * The publishers the account belongs to but that are not in the developer
 * program. The portal only knows about publishers that already enrolled, so
 * the candidates come from the session's memberships.
 */
const candidates = computed(() =>
  session.publishers.filter(
    (publisher) =>
      !portal.developers.some(
        (developer) =>
          developer.publisher?.id === publisher.id ||
          developer.publisher?.name === publisher.name,
      ),
  ),
)

/**
 * Enrollment is the publisher owner's action, and it also needs the
 * `developers.create` node. The button appears only where both hold.
 */
function canEnrollAs(publisher: { role: string }): boolean {
  return session.can('developers.create') && publisher.role === 'owner'
}

function publisherName(developer: Developer): string {
  return developer.publisher?.nick || developer.publisher?.name || developer.id
}

function developerPath(developer: Developer): string {
  return `/developer/${encodeURIComponent(developer.publisher?.name ?? developer.id)}`
}

async function enroll(name: string) {
  busy.value = true
  error.value = ''
  try {
    await developerApi.enroll(name)
    notice.ok(t('portal.enrolled', { name }))
    await portal.load(true)
  } catch (caught) {
    error.value = caught instanceof ExpressError ? caught.message : t('portal.error')
  } finally {
    busy.value = false
  }
}

onMounted(() => void portal.load())
</script>

<template>
  <div>
    <PageHeader :title="t('portal.title')" :description="t('portal.description')">
      <template #actions>
        <IconButton
          :label="t('common.refresh')"
          :disabled="loading || !session.signedIn"
          @click="portal.load(true)"
        >
          <Renew16 aria-hidden="true" />
        </IconButton>
      </template>
    </PageHeader>

    <SessionGate>
      <p v-if="portal.error" class="mb-4 border-l border-error pl-3 text-xs text-error">
        {{ portal.error }}
      </p>
      <p v-if="error" class="mb-4 border-l border-error pl-3 text-xs text-error">{{ error }}</p>

      <LoadingSkeleton v-if="loading" :rows="3" variant="rows" />

      <div v-else class="space-y-6">
        <ConsolePanel :title="t('portal.developers')" :note="String(portal.developers.length)">
          <p v-if="!portal.developers.length" class="voice py-4 text-[12px]">
            {{ t('portal.noDevelopers') }}
          </p>

          <ul v-else class="-my-1">
            <li
              v-for="developer in portal.developers"
              :key="developer.id"
              class="flex flex-wrap items-center gap-x-3 gap-y-2 border-b border-line-soft py-3 last:border-0"
            >
              <RouterLink :to="developerPath(developer)" view-transition class="min-w-0 flex-1">
                <p class="truncate text-[13px] text-base-content">{{ publisherName(developer) }}</p>
                <p class="truncate font-mono text-2xs text-faint">
                  {{ developer.publisher?.name ?? '—' }}
                </p>
              </RouterLink>
              <RouterLink :to="developerPath(developer)" view-transition class="btn btn-ghost btn-xs">
                {{ t('portal.open') }}
              </RouterLink>
            </li>
          </ul>
        </ConsolePanel>

        <ConsolePanel
          v-if="candidates.length"
          :title="t('portal.enrollTitle')"
          :note="t('portal.enrollNote')"
        >
          <ul class="-my-1">
            <li
              v-for="publisher in candidates"
              :key="publisher.id"
              class="flex flex-wrap items-center gap-x-3 gap-y-2 border-b border-line-soft py-3 last:border-0"
            >
              <div class="min-w-0 flex-1">
                <p class="truncate text-[13px] text-base-content">
                  {{ publisher.nick || publisher.name }}
                </p>
                <p class="truncate font-mono text-2xs text-faint">{{ publisher.name }}</p>
              </div>
              <span class="chip">{{ publisher.role }}</span>
              <button
                v-if="canEnrollAs(publisher)"
                type="button"
                class="btn btn-primary btn-xs"
                :disabled="busy"
                @click="enroll(publisher.name)"
              >
                <Add16 class="mr-1" aria-hidden="true" />
                {{ t('portal.enroll') }}
              </button>
              <span v-else class="text-2xs text-faint">{{ t('portal.ownerOnly') }}</span>
            </li>
          </ul>
        </ConsolePanel>
      </div>
    </SessionGate>
  </div>
</template>
