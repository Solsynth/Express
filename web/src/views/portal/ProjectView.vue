<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Add16, Renew16, TrashCan16 } from '@carbon/icons-vue'
import { useRoute } from 'vue-router'

import { projectApi } from '@/lib/api/portal'
import type { ProjectWrite } from '@/lib/api/portal'
import { ExpressError } from '@/lib/api/client'
import { relative } from '@/lib/format'
import { useNoticeStore } from '@/stores/notice'
import { usePortalStore } from '@/stores/portal'
import { useSessionStore } from '@/stores/session'
import PageHeader from '@/components/PageHeader.vue'
import ConsolePanel from '@/components/ConsolePanel.vue'
import LoadingSkeleton from '@/components/LoadingSkeleton.vue'
import IconButton from '@/components/IconButton.vue'
import ConfirmAction from '@/components/ConfirmAction.vue'
import SessionGate from '@/components/SessionGate.vue'
import TabBox from '@/components/TabBox.vue'
import type { Tab } from '@/components/tab'
import ProjectsPanel from '@/components/portal/ProjectsPanel.vue'
import AppsPanel from '@/components/portal/AppsPanel.vue'
import BotsPanel from '@/components/portal/BotsPanel.vue'
import MiniAppsPanel from '@/components/portal/MiniAppsPanel.vue'

const { t } = useI18n()
const route = useRoute()
const session = useSessionStore()
const portal = usePortalStore()
const notice = useNoticeStore()

const developerName = computed(() => String(route.params.name ?? ''))
const tab = ref('projects')

const creating = ref(false)
const busy = ref(false)
const error = ref('')
const draft = ref({ slug: '', name: '', description: '' })

const tabs = computed<Tab[]>(() => {
  const list: Tab[] = [{ key: 'projects', label: t('portal.tabs.projects') }]
  if (portal.developer) list.push({ key: 'apps', label: t('portal.tabs.apps') })
  if (portal.developer) list.push({ key: 'bots', label: t('portal.tabs.bots') })
  if (portal.developer) list.push({ key: 'miniapps', label: t('portal.tabs.miniapps') })
  return list
})

const canCreate = computed(() => session.can('dev.projects.create'))

function openCreate() {
  draft.value = { slug: '', name: '', description: '' }
  creating.value = true
}

async function create() {
  const slug = draft.value.slug.trim()
  if (!slug || !portal.scoped) return
  busy.value = true
  error.value = ''
  const body: ProjectWrite = {
    slug,
    name: draft.value.name.trim() || slug,
    description: draft.value.description.trim(),
  }
  try {
    const created = await projectApi.create(portal.scoped, body)
    notice.ok(t('portal.projectCreated', { name: created.name }))
    creating.value = false
    await portal.open(developerName.value)
  } catch (caught) {
    error.value = caught instanceof ExpressError ? caught.message : t('portal.error')
  } finally {
    busy.value = false
  }
}

async function load() {
  error.value = ''
  try {
    await portal.load()
    await portal.open(developerName.value)
  } catch (caught) {
    error.value = caught instanceof ExpressError ? caught.message : t('portal.error')
  }
}

watch(developerName, () => void load())

onMounted(() => void load())
</script>

<template>
  <div>
    <PageHeader
      :title="portal.developer?.publisher?.nick || portal.developer?.publisher?.name || developerName"
      :description="t('portal.projectDescription')"
    >
      <template #actions>
        <IconButton :label="t('common.refresh')" :disabled="portal.loading" @click="load">
          <Renew16 aria-hidden="true" />
        </IconButton>
      </template>
    </PageHeader>

    <SessionGate>
      <p v-if="portal.error" class="mb-4 border-l border-error pl-3 text-xs text-error">
        {{ portal.error }}
      </p>
      <p v-if="error" class="mb-4 border-l border-error pl-3 text-xs text-error">{{ error }}</p>

      <TabBox :tabs="tabs" :active="tab" :label="t('portal.tabsLabel')" @select="tab = $event" />

      <ProjectsPanel
        v-if="tab === 'projects'"
        :projects="portal.projects"
        :loading="portal.loading"
        :can-create="canCreate"
        :can-manage="session.can('dev.projects.update')"
        @create="openCreate"
        @select="portal.select($event)"
        @refresh="load"
      />

      <AppsPanel
        v-else-if="tab === 'apps'"
        :scope="portal.scoped"
        :project="portal.project"
      />

      <BotsPanel
        v-else-if="tab === 'bots'"
        :scope="portal.scoped"
        :project="portal.project"
      />

      <MiniAppsPanel
        v-else-if="tab === 'miniapps'"
        :scope="portal.scoped"
        :project="portal.project"
      />
    </SessionGate>

    <dialog
      v-if="creating"
      class="modal modal-open"
      aria-modal="true"
      :aria-label="t('portal.createProjectTitle')"
    >
      <div class="modal-box max-w-md rounded-[2px] border border-line bg-base-100 p-0 shadow-none">
        <form @submit.prevent="create">
          <div class="px-5 py-4">
            <h2 class="title text-base text-base-content">{{ t('portal.createProjectTitle') }}</h2>
            <p class="voice mt-2 text-[12px]">{{ t('portal.createProjectBody') }}</p>

            <div class="mt-4 space-y-3">
              <div>
                <label class="field-label" for="project-slug">{{ t('portal.fieldSlug') }}</label>
                <input
                  id="project-slug"
                  v-model="draft.slug"
                  class="input input-sm"
                  required
                  autocomplete="off"
                  spellcheck="false"
                  placeholder="my-app"
                />
                <p class="mt-1 text-2xs text-faint">{{ t('portal.fieldSlugHint') }}</p>
              </div>
              <div>
                <label class="field-label" for="project-name">{{ t('portal.fieldName') }}</label>
                <input
                  id="project-name"
                  v-model="draft.name"
                  class="input input-sm"
                  autocomplete="off"
                  placeholder="My App"
                />
              </div>
              <div>
                <label class="field-label" for="project-description">
                  {{ t('portal.fieldDescription') }}
                </label>
                <textarea
                  id="project-description"
                  v-model="draft.description"
                  class="textarea textarea-sm"
                  rows="3"
                />
              </div>
            </div>
          </div>
          <footer class="flex items-center justify-end gap-2 border-t border-line px-5 py-3">
            <button type="button" class="btn btn-ghost btn-xs" :disabled="busy" @click="creating = false">
              {{ t('common.cancel') }}
            </button>
            <button type="submit" class="btn btn-primary btn-xs" :disabled="busy || !draft.slug.trim()">
              {{ busy ? t('common.saving') : t('common.create') }}
            </button>
          </footer>
        </form>
      </div>
      <button
        type="button"
        class="modal-backdrop bg-black/60"
        :aria-label="t('common.cancel')"
        @click="creating = false"
      />
    </dialog>
  </div>
</template>
