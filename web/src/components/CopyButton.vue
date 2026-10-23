<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Checkmark16, Copy16 } from '@carbon/icons-vue'

const props = defineProps<{
  value: string
  label?: string
}>()

const { t } = useI18n()
const copied = ref(false)

async function copy() {
  try {
    await navigator.clipboard.writeText(props.value)
    copied.value = true
    window.setTimeout(() => {
      copied.value = false
    }, 2000)
  } catch {
    copied.value = false
  }
}
</script>

<template>
  <button
    type="button"
    class="chip justify-center"
    :title="t('copy.title', { label: label ?? t('copy.value') })"
    :aria-label="t('copy.title', { label: label ?? t('copy.value') })"
    @click="copy"
  >
    <Checkmark16 v-if="copied" aria-hidden="true" />
    <Copy16 v-else aria-hidden="true" />
  </button>
</template>
