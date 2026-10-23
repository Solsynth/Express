<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'

import PageHeader from '@/components/PageHeader.vue'

const { t } = useI18n()
</script>

<template>
  <div>
    <PageHeader :title="t('notFound.title')" :description="t('notFound.body')">
      <template #actions>
        <RouterLink to="/marketplace" class="btn btn-primary btn-xs">
          {{ t('notFound.action') }}
        </RouterLink>
      </template>
    </PageHeader>
  </div>
</template>
