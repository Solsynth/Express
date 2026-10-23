<script setup lang="ts">
import { useI18n } from 'vue-i18n'

import { useSessionStore } from '@/stores/session'
import { signInUrl } from '@/lib/auth'
import LoadingSkeleton from '@/components/LoadingSkeleton.vue'

/**
 * The signed-out and offline states of an account surface. A gated page is not
 * a dead end: it names what the page needs and offers the one action that
 * resolves it, instead of rendering an empty shell.
 */
const props = defineProps<{
  /** Shown instead of the generic signed-out copy when the account has no publisher. */
  empty?: 'publishers' | null
}>()

const session = useSessionStore()
const { t } = useI18n()

function signIn() {
  window.location.href = signInUrl()
}
</script>

<template>
  <LoadingSkeleton v-if="session.state === 'loading'" :rows="4" variant="rows" />

  <div v-else-if="session.state === 'offline'" class="py-8">
    <h2 class="title text-lg text-base-content">{{ t('app.offline') }}</h2>
    <p class="voice mt-2 max-w-xl">{{ t('app.offlineBody') }}</p>
    <p v-if="session.error" class="mt-2 font-mono text-2xs text-error">{{ session.error }}</p>
    <button type="button" class="btn btn-primary btn-sm mt-4" @click="session.retry()">
      {{ t('common.retry') }}
    </button>
  </div>

  <div v-else-if="!session.signedIn" class="py-8">
    <h2 class="title text-lg text-base-content">{{ t('app.signedOutTitle') }}</h2>
    <p class="voice mt-2 max-w-xl">{{ t('app.signedOutBody') }}</p>
    <button type="button" class="btn btn-primary btn-sm mt-4" @click="signIn">
      {{ t('app.signIn') }}
    </button>
  </div>

  <div v-else-if="props.empty === 'publishers'" class="py-8">
    <h2 class="title text-lg text-base-content">{{ t('app.noPublishers') }}</h2>
    <p class="voice mt-2 max-w-xl">{{ t('app.noPublishersBody') }}</p>
  </div>

  <slot v-else />
</template>
