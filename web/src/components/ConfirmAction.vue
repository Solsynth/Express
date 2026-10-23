<script setup lang="ts">
import { WarningAlt16 } from '@carbon/icons-vue'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

defineProps<{
  title: string
  body: string
  action: string
  tone?: 'error' | 'neutral'
  busy?: boolean
}>()

const emit = defineEmits<{ confirm: []; cancel: [] }>()
</script>

<template>
  <dialog class="modal modal-open" aria-modal="true" :aria-label="title">
    <div class="modal-box max-w-sm rounded-[2px] border border-line bg-base-100 p-0 shadow-none">
      <div class="px-5 py-4">
        <h2 class="flex items-center gap-1.5 text-sm text-base-content">
          <WarningAlt16 v-if="tone === 'error'" class="shrink-0 text-error" aria-hidden="true" />
          {{ title }}
        </h2>
        <p class="voice mt-2">{{ body }}</p>
      </div>
      <footer class="flex items-center justify-end gap-2 border-t border-line px-5 py-3">
        <button type="button" class="btn btn-ghost btn-xs" :disabled="busy" @click="emit('cancel')">
          {{ t('common.cancel') }}
        </button>
        <button
          type="button"
          class="btn btn-xs"
          :class="tone === 'error' ? 'btn-error' : 'btn-primary'"
          :disabled="busy"
          @click="emit('confirm')"
        >
          {{ busy ? t('confirm.working') : action }}
        </button>
      </footer>
    </div>
    <button
      type="button"
      class="modal-backdrop bg-black/60"
      :aria-label="t('common.cancel')"
      @click="emit('cancel')"
    />
  </dialog>
</template>
