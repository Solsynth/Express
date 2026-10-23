<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { Add16 } from '@carbon/icons-vue'

import type { DevProject } from '@/lib/api/types'
import { relative } from '@/lib/format'
import ConsolePanel from '@/components/ConsolePanel.vue'
import LoadingSkeleton from '@/components/LoadingSkeleton.vue'

defineProps<{
  projects: DevProject[]
  loading: boolean
  canCreate: boolean
  canManage: boolean
}>()

const emit = defineEmits<{
  create: []
  select: [id: string]
  refresh: []
}>()

const { t } = useI18n()
</script>

<template>
  <ConsolePanel :title="t('portal.projects')" :note="loading ? undefined : String(projects.length)">
    <template #header>
      <button v-if="canCreate" type="button" class="btn btn-ghost btn-xs" @click="emit('create')">
        <Add16 class="mr-1" aria-hidden="true" />
        {{ t('portal.createProject') }}
      </button>
    </template>

    <LoadingSkeleton v-if="loading" :rows="3" :columns="[3, 2, 2]" header />

    <p v-else-if="!projects.length" class="voice py-4 text-[12px]">
      {{ t('portal.noProjects') }}
    </p>

    <ul v-else class="-my-1">
      <li
        v-for="project in projects"
        :key="project.id"
        class="flex flex-wrap items-center gap-x-3 gap-y-2 border-b border-line-soft py-3 last:border-0"
      >
        <button type="button" class="min-w-0 flex-1 text-left" @click="emit('select', project.id)">
          <p class="truncate text-[13px] text-base-content">{{ project.name }}</p>
          <p class="truncate font-mono text-2xs text-faint">
            <span>{{ project.slug }}</span>
            <span class="mx-1 text-line">·</span>
            {{ relative(project.updated_at) }}
          </p>
        </button>
        <p v-if="project.description" class="voice w-full text-[12px] text-dim sm:w-auto sm:flex-1">
          {{ project.description }}
        </p>
      </li>
    </ul>
  </ConsolePanel>
</template>
