<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { Package16 } from '@carbon/icons-vue'

import type { CloudFile } from '@/lib/api/types'
import { installMediaElements } from '@/lib/media'

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

installMediaElements()

const failed = ref(false)
const mediaEl = ref<HTMLElement | null>(null)
let observer: MutationObserver | null = null

/**
 * `sk-media` reports a failed load by setting `data-error` on itself — it has
 * no error event — so the monogram fallback watches the attribute. Without
 * this a stale reference would leave an empty box instead of a letter.
 */
function observeMedia(el: HTMLElement | null) {
  observer?.disconnect()
  observer = null
  if (!el) return
  failed.value = el.hasAttribute('data-error')
  observer = new MutationObserver(() => {
    failed.value = el.hasAttribute('data-error')
  })
  observer.observe(el, { attributes: true, attributeFilter: ['data-error'] })
}

watch(mediaEl, (el) => observeMedia(el), { flush: 'post' })
onBeforeUnmount(() => observer?.disconnect())

watch(
  () => props.picture?.id ?? props.picture?.url,
  () => {
    failed.value = false
  },
)

// A stored reference may carry only an id; sk-media resolves that id against
// the drive, so a legacy row renders without a URL of its own.
const fileId = computed(() => props.picture?.id ?? '')
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
    <sk-media
      v-if="fileId && !failed"
      ref="mediaEl"
      :file="fileId"
      class="block h-full w-full"
    ></sk-media>
    <span v-else-if="name" class="font-sans font-medium" aria-hidden="true">{{ monogram }}</span>
    <Package16 v-else aria-hidden="true" />
  </span>
</template>
