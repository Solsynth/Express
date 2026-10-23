<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Package16 } from '@carbon/icons-vue'

import type { CloudFile } from '@/lib/api/types'

/**
 * A product's mark. A product may carry a Drive picture; when it does not — or
 * when the stored file has gone and the image fails — the mark is a monogram
 * of the product name, and failing that a neutral glyph. A stale reference
 * therefore never leaves a hole in a row.
 */
const props = withDefaults(
  defineProps<{
    picture?: CloudFile | null
    name?: string
    /** Square the corner for list rows; leave rounded for a page title. */
    flat?: boolean
    size?: 'sm' | 'md' | 'lg'
  }>(),
  { flat: false, size: 'md' },
)

const failed = ref(false)

watch(
  () => props.picture?.id ?? props.picture?.url,
  () => {
    failed.value = false
  },
)

const source = computed(() => props.picture?.url ?? '')
const monogram = computed(() => (props.name ?? '').trim().slice(0, 1).toUpperCase() || '?')
const box = computed(() => {
  switch (props.size) {
    case 'sm':
      return 'h-7 w-7 text-[11px]'
    case 'lg':
      return 'h-14 w-14 text-lg'
    default:
      return 'h-9 w-9 text-sm'
  }
})
</script>

<template>
  <span
    class="flex shrink-0 items-center justify-center overflow-hidden border border-line bg-base-300/60 text-dim"
    :class="[box, flat ? 'rounded-[2px]' : 'rounded-[3px]']"
  >
    <img
      v-if="source && !failed"
      :src="source"
      alt=""
      class="h-full w-full object-cover"
      @error="failed = true"
    />
    <span v-else-if="name" class="font-sans font-medium" aria-hidden="true">{{ monogram }}</span>
    <Package16 v-else aria-hidden="true" />
  </span>
</template>
