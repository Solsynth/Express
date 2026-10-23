<script setup lang="ts">
defineProps<{
  title: string
  note?: string
}>()
</script>

<template>
  <section class="border border-line bg-base-200/30">
    <!-- The actions slot drops below the title on a narrow card instead of
         pushing it off the edge: a panel header has no room for a title, a
         count, and two buttons on one 340 px line. -->
    <header
      class="flex flex-wrap items-baseline justify-between gap-x-2 gap-y-1 px-4 pb-1 pt-3"
    >
      <div class="flex min-w-0 flex-wrap items-baseline gap-x-2">
        <h2 class="text-2xs uppercase tracking-[0.16em] text-dim">{{ title }}</h2>
        <span v-if="note" class="text-2xs text-faint">{{ note }}</span>
      </div>
      <!-- Actions take a full line of their own on a narrow card; on a wider
           one they sit against the right edge. -->
      <div class="ml-auto flex w-full flex-wrap items-center justify-end gap-1 sm:w-auto sm:gap-2">
        <slot name="header" />
      </div>
    </header>
    <div class="px-4 py-4">
      <slot />
    </div>
    <footer v-if="$slots.footer" class="px-4 pb-3 pt-1">
      <slot name="footer" />
    </footer>
  </section>
</template>
