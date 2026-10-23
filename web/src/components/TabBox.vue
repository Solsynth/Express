<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'

import type { Tab } from '@/components/tab'

/**
 * The console's boxed tab strip: 1 px border, hairline dividers, the active tab
 * filled with `base-300`. A tab with a `to` is a route link; one without is a
 * local view switch and renders a toggle button, so both read as the same
 * control without a second set of classes.
 *
 * A strip with six tabs does not fit a 390 px card in one row, and scrolling
 * it would hide the tab that names the current section. Below the `sm`
 * breakpoint the same box becomes a three-column grid; the hairlines are the
 * container's own color showing through 1 px gaps, so any tab count divides
 * evenly and the wrapping needs no per-tab border rules.
 */
const props = defineProps<{
  tabs: Tab[]
  active: string
  label: string
}>()

const emit = defineEmits<{ select: [key: string] }>()

const root = ref<HTMLElement | null>(null)

const tabClass = (current: boolean) =>
  current
    ? 'bg-base-300 text-base-content'
    : 'bg-base-200 text-faint hover:bg-base-300/50 hover:text-dim sm:bg-transparent'

/**
 * On a single-row strip, reveal the active tab when the strip is wider than
 * the card (a long label set on a medium viewport). The grid layout never
 * needs it, but the measurement is harmless there.
 */
function revealActive() {
  const nav = root.value
  if (!nav) return
  const current = nav.querySelector<HTMLElement>('[aria-current="page"], [aria-pressed="true"]')
  if (!current) return
  const strip = nav.getBoundingClientRect()
  const tab = current.getBoundingClientRect()
  if (tab.left < strip.left) nav.scrollLeft -= strip.left - tab.left
  else if (tab.right > strip.right) nav.scrollLeft += tab.right - strip.right
}

onMounted(revealActive)
watch(
  () => props.active,
  () => nextTick(revealActive),
)
const resize = new ResizeObserver(revealActive)
onMounted(() => {
  if (root.value) resize.observe(root.value)
})
onBeforeUnmount(() => resize.disconnect())
</script>

<template>
  <nav
    ref="root"
    class="mb-6 grid w-full grid-cols-3 gap-px overflow-hidden border border-line bg-line sm:flex sm:w-fit sm:max-w-full sm:gap-0 sm:overflow-x-auto sm:bg-base-200/30"
    :aria-label="label"
  >
    <template v-for="tab in tabs" :key="tab.key">
      <RouterLink
        v-if="tab.to"
        :to="tab.to"
        class="flex items-center justify-center whitespace-nowrap px-3 py-2 text-center text-2xs tracking-[0.02em] transition-colors sm:flex-none sm:justify-start sm:border-r sm:border-line sm:py-1.5 sm:last:border-r-0"
        :class="tabClass(active === tab.key)"
        :aria-current="active === tab.key ? 'page' : undefined"
      >
        {{ tab.label }}
      </RouterLink>
      <button
        v-else
        type="button"
        class="flex items-center justify-center whitespace-nowrap px-3 py-2 text-center text-2xs tracking-[0.02em] transition-colors sm:flex-none sm:justify-start sm:border-r sm:border-line sm:py-1.5 sm:last:border-r-0"
        :class="tabClass(active === tab.key)"
        :aria-pressed="active === tab.key"
        @click="emit('select', tab.key)"
      >
        {{ tab.label }}
      </button>
    </template>
  </nav>
</template>
